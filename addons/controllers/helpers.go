package controllers

// Check to see what we have in cluster api
import (
	"context"
	"fmt"
	runtanzuv1alpha3 "github.com/vmware-tanzu/tanzu-framework/apis/run/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func GetClusterBoostrap(ctx context.Context, cluster *clusterv1.Cluster, ctrlClient client.Client) (*runtanzuv1alpha3.ClusterBootstrap, error) {
	clusterBootstrap := &runtanzuv1alpha3.ClusterBootstrap{}
	err := ctrlClient.Get(ctx, client.ObjectKeyFromObject(cluster), clusterBootstrap)
	if err != nil {
		return nil, err
	}

	return clusterBootstrap, nil
}

func GetMachineClusterBoostrap(ctx context.Context, machine *clusterv1.Machine, ctrlClient client.Client) (*runtanzuv1alpha3.ClusterBootstrap, error) {
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

	//TODO (adduarte): need to think through the case of clusterbootstrap not being present
	return GetClusterBoostrap(ctx, cluster, ctrlClient)
}
func hasAdditionalPackageInstalls(cluster *clusterapiv1beta1.Cluster) bool {
	//checks  if cluster has additionalPackage installs.
	return true
}

func timeoutOccured(cluster *clusterapiv1beta1.Cluster) bool {
	// figures out if timeout has occured: too much time has elapsed since cluster delete first was issued.
	// checked delete time vs current time
	return false
}

//Notes:
//delete clsuter boostrap. Check reconcile noraml for clusterboostrap reconcierl to undo
