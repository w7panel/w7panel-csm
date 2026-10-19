package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type DataDiskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DataDiskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&api.DataDisk{}).Owns(&corev1.PersistentVolumeClaim{}).Complete(r)
}

func (r *DataDiskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	d := &api.DataDisk{}
	if err := r.Get(ctx, req.NamespacedName, d); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	name := d.Status.PVCName
	if name == "" {
		name = storage.ClaimName("data-", d.Name)
	}
	if !d.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(d, api.DiskFinalizer) {
			return ctrl.Result{}, nil
		}
		free, err := r.detached(ctx, d)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !free {
			return r.diskStatus(ctx, d, "Detaching", "waiting for host rollout", name)
		}
		pvc := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: name}, pvc); err == nil {
			if !metav1.IsControlledBy(pvc, d) {
				return ctrl.Result{}, fmt.Errorf("data PVC %s is not owned by DataDisk", name)
			}
			if err := r.Delete(ctx, pvc); err != nil {
				return ctrl.Result{}, err
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: name}, pvc); err == nil {
			return r.diskStatus(ctx, d, "Deleting", "waiting for PVC deletion", name)
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(d, api.DiskFinalizer)
		return ctrl.Result{}, r.Update(ctx, d)
	}
	if !controllerutil.ContainsFinalizer(d, api.DiskFinalizer) {
		controllerutil.AddFinalizer(d, api.DiskFinalizer)
		return ctrl.Result{}, r.Update(ctx, d)
	}
	if !d.Spec.Paid {
		return r.diskStatus(ctx, d, "PendingPayment", "", "")
	}
	expired, err := past(d.Spec.ExpireTime)
	if err != nil {
		return r.diskStatus(ctx, d, "Failed", err.Error(), name)
	}
	if expired {
		free, err := r.detached(ctx, d)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !free {
			return r.diskStatus(ctx, d, "Unmounting", "waiting for host rollout", name)
		}
		return r.diskStatus(ctx, d, "Expired", "", name)
	}
	if d.Spec.StorageGi < 1 {
		return r.diskStatus(ctx, d, "Failed", "storageGi must be positive", "")
	}
	if d.Spec.Attached && (d.Spec.CsmName == "" || d.Spec.MountPath == "") {
		return r.diskStatus(ctx, d, "Failed", "attached disk requires csmName and mountPath", name)
	}
	pvc, err := storage.EnsurePVC(ctx, r.Client, r.Scheme, d, name, d.Spec.StorageClassName, d.Spec.StorageGi)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !d.Spec.Attached {
		free, err := r.detached(ctx, d)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !free {
			return r.diskStatus(ctx, d, "Unmounting", "waiting for host rollout", name)
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			return r.diskStatus(ctx, d, "Provisioning", "waiting for PVC bind", name)
		}
		return r.diskStatus(ctx, d, "Available", "", name)
	}
	host := &api.Csm{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: d.Spec.CsmName}, host); err != nil {
		return r.diskStatus(ctx, d, "Failed", fmt.Sprintf("host: %v", err), name)
	}
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: host.Name}, dep); err != nil {
		return r.diskStatus(ctx, d, "Mounting", "waiting for host deployment", name)
	}
	if pvc.Status.Phase != corev1.ClaimBound || !deploymentMounts(dep, name) || dep.Status.ReadyReplicas != 1 || dep.Status.ObservedGeneration < dep.Generation {
		return r.diskStatus(ctx, d, "Mounting", "waiting for PVC and host rollout", name)
	}
	return r.diskStatus(ctx, d, "Mounted", "", name)
}

func deploymentMounts(dep *appsv1.Deployment, claim string) bool {
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == claim {
			return true
		}
	}
	return false
}

func (r *DataDiskReconciler) detached(ctx context.Context, d *api.DataDisk) (bool, error) {
	name := d.Status.PVCName
	if name == "" {
		name = storage.ClaimName("data-", d.Name)
	}
	list := &appsv1.DeploymentList{}
	if err := r.List(ctx, list, client.InNamespace(d.Namespace)); err != nil {
		return false, err
	}
	for i := range list.Items {
		dep := &list.Items[i]
		if deploymentMounts(dep, name) {
			return false, nil
		}
		if dep.Name == d.Spec.CsmName && (dep.Status.ObservedGeneration < dep.Generation || dep.Status.ReadyReplicas != 1) {
			return false, nil
		}
	}
	return true, nil
}

func (r *DataDiskReconciler) diskStatus(ctx context.Context, d *api.DataDisk, phase, msg, name string) (ctrl.Result, error) {
	next := d.Status
	next.Phase = phase
	next.Message = msg
	if name != "" {
		next.PVCName = name
	}
	if !reflect.DeepEqual(next, d.Status) {
		d.Status = next
		if err := r.Status().Update(ctx, d); err != nil {
			return ctrl.Result{}, err
		}
	}
	if phase == "Mounted" {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	if phase == "Mounting" || phase == "Unmounting" || phase == "Detaching" || phase == "Provisioning" || phase == "Deleting" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}
