package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	runtanzuv1alpha3 "github.com/vmware-tanzu/tanzu-framework/apis/run/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3" //TODO (adduarte) check: this should probably be v1beta1
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type MachineReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const PreTerminateAnnotation = clusterv1.PreTerminateDeleteHookAnnotationPrefix + "/addons"

// SetupWithManager adds this reconciler to a new controller then to the
// provided manager.
func (r *MachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// We need to watch for clusterboostrap
		For(&clusterv1.Machine{}).
		Watches(
			&source.Kind{Type: &runtanzuv1alpha3.ClusterBootstrap{}},
			handler.EnqueueRequestsFromMapFunc(machinesInClusterBootstrap(r.Client, r.Log)),
		).
		Complete(r)
}

func machinesInClusterBootstrap(c client.Client, log logr.Logger) handler.MapFunc {
	return func(o client.Object) []reconcile.Request {

		ctx := context.Background()
		clusterBoostrap, ok := o.(*runtanzuv1alpha3.ClusterBootstrap)
		if !ok {
			log.Error(errors.New("invalid type"),
				"Expected to receive ClusterBoostrap resource",
				"actualType", fmt.Sprintf("%T", o))
			return nil
		}

		listOptions := []client.ListOption{
			client.InNamespace(clusterBoostrap.Namespace),
			client.MatchingLabels(map[string]string{clusterv1.ClusterLabelName: clusterBoostrap.Name}), // we take advantage of the fact that clusterBoostrap name = couster name
		}

		var machines clusterv1.MachineList
		if err := c.List(ctx, &machines, listOptions...); err != nil {
			return []reconcile.Request{}
		}

		// Create a reconcile request for each machine resource.
		requests := []ctrl.Request{}
		for _, machine := range machines.Items {
			requests = append(requests, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: machine.Namespace,
					Name:      machine.Name,
				},
			})
		}
		log.Info("Generating requests", "requests", requests)
		// Return reconcile requests for the Machine resources.
		return requests
	}
}

func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := r.Log.WithValues("Machine", req.NamespacedName)

	res := ctrl.Result{}
	// Get the resource for this request.
	obj := &clusterv1.Machine{}
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Machine not found, will not reconcile")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Always Patch when exiting this function so changes to the resource are updated on the API server.
	patchHelper, err := patch.NewHelper(obj, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to init patch helper for %s %s",
			obj.GroupVersionKind(), req.NamespacedName)
	}
	defer func() {
		if err := patchHelper.Patch(ctx, obj); err != nil {
			if reterr == nil {
				reterr = err
			}
			log.Error(err, "patch failed")
		}
	}()

	// Get the name of the cluster to which the current machine belongs
	clusterName := obj.Spec.ClusterName
	if clusterName == "" {
		log.Info("machine doesn't have cluster name label, skip reconciling")
		return res, nil
	}

	cluster := &clusterv1.Cluster{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: obj.Namespace,
		Name:      clusterName,
	}, cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			return res, err
		}
		return res, nil
	}

	clusterBootstrap := &runtanzuv1alpha3.ClusterBootstrap{}
	err = r.Client.Get(ctx, client.ObjectKeyFromObject(cluster), clusterBootstrap)
	if err != nil {
		// need to think through the case of clusterbootstrap not being present
		log.Error(err, "failed to get cluster bootstrap from cluster")
		return ctrl.Result{}, err
	}

	// Removes the pre-terminate hook when machine is being deleted directly and it's parent cluster is not.
	if !obj.GetDeletionTimestamp().IsZero() && cluster.GetDeletionTimestamp().IsZero() {
		delete(obj.Annotations, PreTerminateAnnotation)
		log.Info("Machine is being deleted though its parent Cluster is not, removing pre-terminate hook")
		return res, nil
	}

	// Handle cluster delete.
	if !cluster.GetDeletionTimestamp().IsZero() {
		res, err := r.reconcileMachineDeletionHook(ctx, log, obj, cluster)
		if err != nil {
			log.Error(err, "failed to reconcile Machine deletion")
			return res, err
		}
		return res, nil
	}

	// Handle cluster create/update
	return r.reconcileNormal(ctx, log, obj, cluster)
}

// reconcileNormal adds the pre-terminate machine deletion phase hook to the Machine
func (r *MachineReconciler) reconcileNormal(ctx context.Context, log logr.Logger, obj *clusterv1.Machine, cluster *clusterv1.Cluster) (ctrl.Result, error) {
	log.Info("Start reconciling")

	clusterBootstrap := &runtanzuv1alpha3.ClusterBootstrap{}
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(cluster), clusterBootstrap)
	if err != nil {
		log.Error(err, "failed to get cluster bootstrap from cluster")
		return ctrl.Result{}, err
	}

	if ContainsFinalizer(clusterBootstrap, AdditionalPackageFinalizer) {
		// Add pre-terminate machine deletion phase hook if it doesn't exist

		if _, exist := obj.Annotations[clusterv1.PreTerminateDeleteHookAnnotationPrefix]; !exist {
			if obj.Annotations == nil {
				obj.Annotations = make(map[string]string)
			}

			obj.Annotations[PreTerminateAnnotation] = "additional-package-installs"
		}
	}

	return ctrl.Result{}, nil
}

// reconcileMachineDeletionHook removes the pre-terminate hook when the finalizer not present the Cluster
// is absent
func (r *MachineReconciler) reconcileMachineDeletionHook(ctx context.Context, log logr.Logger, obj *clusterv1.Machine, cluster *clusterv1.Cluster) (ctrl.Result, error) {
	log.Info("Start reconciling machine deletion pre-terminate hook")

	res := ctrl.Result{}

	if ContainsFinalizer(cluster, AdditionalPackageFinalizer) { // TODO (adduarte) ask: do we add finalizer to clusterboostrap or cluster object.
		// if clusterbootstrap need to delete all resources associate. clones, secrets, etc..
		// anything that was created when clusterboostrap was created.<-add to clsuterboostrap delete logic.
		log.Info("Cluster has finalizer set. Clean up has not finished. Will skip reconciling", "finalizer", ContainsFinalizer)
		return res, nil
	}

	if annotations.HasWithPrefix(clusterv1.PreTerminateDeleteHookAnnotationPrefix, obj.ObjectMeta.Annotations) {
		// Removes the pre-terminate hook as the cleanup has finished
		delete(obj.Annotations, PreTerminateAnnotation)
		log.Info("Removing pre-terminate hook")
	}

	return res, nil
}
