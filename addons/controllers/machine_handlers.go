package controllers

//TODO (adduarte) looks like most of this functions can be pulled from cluster api.
// Check to see what we have in cluster api. Rework
import (
	"context"
	"errors"
	"fmt"
	"github.com/vmware-tanzu/tanzu-framework/addons/pkg/constants"
	runtanzuv1alpha3 "github.com/vmware-tanzu/tanzu-framework/apis/run/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AddFinalizer accepts an Object and adds the provided finalizer if not present.
func AddFinalizer(o client.Object, finalizer string) {
	f := o.GetFinalizers()
	for _, e := range f {
		if e == finalizer {
			return
		}
	}
	o.SetFinalizers(append(f, finalizer))
}

// RemoveFinalizer accepts an Object and removes the provided finalizer if present.
func RemoveFinalizer(o client.Object, finalizer string) {
	f := o.GetFinalizers()
	for i := 0; i < len(f); i++ {
		if f[i] == finalizer {
			f = append(f[:i], f[i+1:]...)
			i--
		}
	}
	o.SetFinalizers(f)
}

// ContainsFinalizer checks an Object that the provided finalizer is present.
func ContainsFinalizer(o client.Object, finalizer string) bool {
	f := o.GetFinalizers()
	for _, e := range f {
		if e == finalizer {
			return true
		}
	}
	return false
}

func (r *MachineReconciler) MachinesFromClusterBoostrap(o client.Object) []ctrl.Request {
	clusterBootstrap, ok := o.(*runtanzuv1alpha3.ClusterBootstrap)
	if !ok {
		r.Log.Error(errors.New("invalid type"),
			"Expected to receive ClusterBootstrap resource",
			"actualType", fmt.Sprintf("%T", o))
		return nil
	}

	log := r.Log.WithValues(constants.ClusterBootstrapNameLogKey, clusterBootstrap.Name)

	log.V(4).Info("Mapping ClusterBootstrap to machines")

	// take advantage that cluster.Name = clusterBoostrap.Name to get list of machines
	var machines clusterv1.MachineList
	listOptions := []client.ListOption{
		client.InNamespace(clusterBootstrap.Namespace),
		client.MatchingLabels(map[string]string{clusterv1.ClusterLabelName: clusterBootstrap.Name}),
	}
	ctx := context.Background() //TODO (adduarte) can we get this from somewhere else?
	if err := r.Client.List(ctx, &machines, listOptions...); err != nil {
		return []reconcile.Request{}
	}
	// Create a reconcile request for each machine resource.
	var requests []ctrl.Request
	for _, machine := range machines.Items {
		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: machine.Namespace,
				Name:      machine.Name,
			},
		})
	}
	log.Info("Generating requests", "requests", requests) //TODO (adduarte): do we really need this?
	// Return list of reconcile requests for the Machine resources.
	return requests

}

func MachineToClusterBoostrap(ctx context.Context, machine *clusterv1.Machine, ctrlClient client.Client) (*runtanzuv1alpha3.ClusterBootstrap, error) {

	// Get the name of the cluster to which the current machine belongs
	clusterName := machine.Spec.ClusterName
	if clusterName == "" {
		return nil, fmt.Errorf("machine spec does not contain a cluster name")
	}

	cluster := &clusterv1.Cluster{}
	if err := ctrlClient.Get(ctx, client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      clusterName,
	}, cluster); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, nil
	}
	clusterBootstrap := &runtanzuv1alpha3.ClusterBootstrap{}
	err := ctrlClient.Get(ctx, client.ObjectKeyFromObject(cluster), clusterBootstrap)
	if err != nil {
		return nil, err
	}

	return clusterBootstrap, nil
	//TODO (adduarte): need to think through the case of clusterbootstrap not being present

}
