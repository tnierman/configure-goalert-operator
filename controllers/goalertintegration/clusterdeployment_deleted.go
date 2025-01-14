package goalertintegration

//goland:noinspection SpellCheckingInspection
import (
	"context"
	"github.com/openshift/configure-goalert-operator/pkg/localmetrics"

	goalertv1alpha1 "github.com/openshift/configure-goalert-operator/api/v1alpha1"
	"github.com/openshift/configure-goalert-operator/config"
	"github.com/openshift/configure-goalert-operator/pkg/goalert"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *GoalertIntegrationReconciler) handleDelete(ctx context.Context, gclient goalert.Client, gi *goalertv1alpha1.GoalertIntegration, cd *hivev1.ClusterDeployment) error {

	if cd == nil {
		return nil
	}

	// Evaluate edge-cases where Goalert service no longer needs to be deleted
	deleteSvcBool := true

	cmData := &v1.ConfigMap{Data: map[string]string{}}
	cmData.Name = config.Name(gi.Spec.ServicePrefix, cd.Name, config.ConfigMapSuffix)
	err := r.Get(ctx, types.NamespacedName{Name: cmData.Name, Namespace: cd.Namespace}, cmData)
	if err != nil {
		if !errors.IsNotFound(err) {
			// some error other than not found, requeue
			return err
		}
		deleteSvcBool = false
	}

	if deleteSvcBool {
		goalertHighServiceID := cmData.Data["HIGH_SERVICE_ID"]
		goalertLowServiceID := cmData.Data["LOW_SERVICE_ID"]

		if goalertHighServiceID != "" {
			r.reqLogger.Info("Deleting service", "goalert high service id", goalertHighServiceID)
			err = gclient.DeleteService(ctx, &goalert.Data{
				Id:      goalertHighServiceID,
				Timeout: 15,
			})
			if err != nil {
				r.reqLogger.Error(err, "unable to delete service", "goalert high service id", goalertHighServiceID)
				localmetrics.UpdateMetricCGAODeleteFailure(1, goalertHighServiceID)
				return err
			}
		}

		if goalertLowServiceID != "" {
			r.reqLogger.Info("Deleting service", "goalert low service id", goalertLowServiceID)
			err = gclient.DeleteService(ctx, &goalert.Data{
				Id:      goalertLowServiceID,
				Timeout: 15,
			})
			if err != nil {
				r.reqLogger.Error(err, "unable to delete service %s", "goalert low service id", goalertLowServiceID)
				localmetrics.UpdateMetricCGAODeleteFailure(1, goalertLowServiceID)
				return err
			}
		}

		r.reqLogger.Info("Deleting Goalert configmap for", "clusterdeployment:", cd.Name)
		cmData.Namespace = cd.Namespace
		err = r.Delete(ctx, cmData)
		if err != nil {
			r.reqLogger.Error(err, "unable to remove goalert configmap", "configmap", cmData.Name)
			return err
		}
	}

	deleteSecret := true
	secretToRemove := &v1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: config.SecretName, Namespace: cd.Namespace}, secretToRemove)
	if err != nil {
		if !errors.IsNotFound(err) {
			r.reqLogger.Error(err, "unable to reconcile secret for", "clusterdeployment", cd.Name)
			return err
		}
		r.reqLogger.Info("unable to locate goalert secret for cluster deployment, moving on", "clusterdeployment:", cd.Name)
		deleteSecret = false
	}

	deleteSyncset := true
	ssToRemove := &hivev1.SyncSet{}
	err = r.Get(ctx, types.NamespacedName{Name: config.SecretName, Namespace: cd.Namespace}, ssToRemove)
	if err != nil {
		if !errors.IsNotFound(err) {
			r.reqLogger.Error(err, "unable to reconcile syncset for", "clusterdeployment name", cd.Name)
			return err
		}
		r.reqLogger.Info("unable to locate goalert syncset for cluster deployment, moving on", "clusterdeployment", cd.Name)
		deleteSyncset = false
	}

	if deleteSecret {
		r.reqLogger.Info("Deleting Goalert secret for", "clusterdeployment: ", cd.Name)
		secretToRemove.Name = config.SecretName
		secretToRemove.Namespace = cd.Namespace
		err = r.Delete(ctx, secretToRemove)
		if err != nil {
			r.reqLogger.Error(err, "unable to delete secret for", "clusterdeployment", cd.Name)
			return err
		}
	}

	if deleteSyncset {
		r.reqLogger.Info("Deleting Goalert syncset for", "clusterdeployment:", cd.Name)
		ssToRemove.Name = config.SecretName
		ssToRemove.Namespace = cd.Namespace
		err = r.Delete(ctx, ssToRemove)
		if err != nil {
			r.reqLogger.Error(err, "unable to remove goalert syncset", "clusterdeployment", cd.Name)
			return err
		}
	}

	goalertFinalizer := config.GoalertFinalizerPrefix + gi.Name
	r.reqLogger.Info("removing Goalert finalizer from ClusterDeployment", "clusterdeployment", cd.Name)
	baseToPatch := client.MergeFrom(cd.DeepCopy())
	deleteFinalizer := controllerutil.RemoveFinalizer(cd, goalertFinalizer)
	if !deleteFinalizer {
		r.reqLogger.Error(err, "failed to update cd finalizer")
	}
	if err := r.Patch(ctx, cd, baseToPatch); err != nil {
		r.reqLogger.Error(err, "failed to remove finalizer from cd", "clusterdeployment:", cd.Name)
	}
	return nil
}
