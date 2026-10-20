package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	hostruntime "github.com/w7panel/w7panel-csm/pkg/runtime"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

type CsmReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	GatewayNamespace   string
	GatewayName        string
	GatewayServiceName string
	PortStart          int32
	PortEnd            int32
	JumpPort           int32
	SSHDomainSuffix    string
}

func (r *CsmReconciler) setOwner(owner *api.Csm, obj client.Object) error {
	return controllerutil.SetControllerReference(owner, obj, r.Scheme)
}

func (r *CsmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&api.Csm{}).Owns(&appsv1.Deployment{}).Owns(&corev1.PersistentVolumeClaim{}).Watches(&api.DataDisk{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
		d := obj.(*api.DataDisk)
		if d.Spec.CsmName == "" {
			return nil
		}
		return []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: d.Namespace, Name: d.Spec.CsmName}}}
	})).Watches(&api.SystemTemplate{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
		list := &api.CsmList{}
		if err := r.List(ctx, list); err != nil {
			return nil
		}
		requests := make([]ctrl.Request, 0)
		for _, h := range list.Items {
			if h.Spec.TemplateName == obj.GetName() {
				requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: h.Namespace, Name: h.Name}})
			}
		}
		return requests
	})).Complete(r)
}

func (r *CsmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	host := &api.Csm{}
	if err := r.Get(ctx, req.NamespacedName, host); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !host.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(host, api.CsmFinalizer) {
			if err := r.releasePort(ctx, host); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(host, api.CsmFinalizer)
			return ctrl.Result{}, r.Update(ctx, host)
		}
		return ctrl.Result{}, nil
	}
	if !controllerutil.ContainsFinalizer(host, api.CsmFinalizer) {
		controllerutil.AddFinalizer(host, api.CsmFinalizer)
		return ctrl.Result{}, r.Update(ctx, host)
	}
	if !host.Spec.Paid {
		if err := r.stopHost(ctx, host, false); err != nil {
			return ctrl.Result{}, err
		}
		return r.hostStatus(ctx, host, "PendingPayment", "", 0)
	}
	expired, err := past(host.Spec.ExpireTime)
	if err != nil {
		return r.hostStatus(ctx, host, "Failed", err.Error(), 0)
	}
	if expired {
		recycle, err := past(host.Spec.RecycleTime)
		if err != nil {
			return r.hostStatus(ctx, host, "Failed", err.Error(), 0)
		}
		if err := r.stopHost(ctx, host, recycle); err != nil {
			return ctrl.Result{}, err
		}
		phase := "Expired"
		if recycle {
			phase = "Recycled"
		}
		return r.hostStatus(ctx, host, phase, "", 0)
	}
	if host.Spec.TemplateName == "" || host.Spec.SystemDiskGi < 1 {
		return r.hostStatus(ctx, host, "Failed", "templateName and systemDiskGi are required", 0)
	}
	template := &api.SystemTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Name: host.Spec.TemplateName}, template); err != nil {
		return r.hostStatus(ctx, host, "Failed", fmt.Sprintf("template: %v", err), 0)
	}
	image := template.Spec.Image
	if image == "" {
		return r.hostStatus(ctx, host, "Failed", "template image is empty", 0)
	}
	claim := host.Status.SystemPVCName
	if claim == "" {
		claim = storage.ClaimName("system-", host.Name)
	}
	if _, err := storage.EnsurePVC(ctx, r.Client, r.Scheme, host, claim, host.Spec.StorageClassName, host.Spec.SystemDiskGi); err != nil {
		return ctrl.Result{}, err
	}
	if host.Status.SystemPVCName != claim {
		if _, err := r.hostStatus(ctx, host, "Provisioning", "", 0); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	var port int32
	if r.JumpPort != 0 {
		if err := r.releasePort(ctx, host); err != nil {
			return ctrl.Result{}, err
		}
		port = r.JumpPort
	} else {
		var err error
		port, err = r.ensurePort(ctx, host)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	disks := &api.DataDiskList{}
	if err := r.List(ctx, disks, client.InNamespace(host.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	mounts := make([]hostruntime.DiskMount, 0)
	for _, d := range disks.Items {
		if d.Spec.CsmName != host.Name {
			continue
		}
		diskExpired, err := past(d.Spec.ExpireTime)
		if err != nil {
			return ctrl.Result{}, err
		}
		if d.Spec.Paid && !diskExpired && d.Spec.Attached && d.Status.PVCName != "" && d.DeletionTimestamp.IsZero() {
			mounts = append(mounts, hostruntime.DiskMount{Name: d.Name, PVCName: d.Status.PVCName, Path: d.Spec.MountPath})
		}
	}
	dep, err := hostruntime.Deployment(host, image, mounts)
	if err != nil {
		return r.hostStatus(ctx, host, "Failed", err.Error(), port)
	}
	if err := r.ensureDeployment(ctx, host, dep); err != nil {
		return ctrl.Result{}, err
	}
	if r.JumpPort == 0 {
		if err := r.ensureRoute(ctx, host); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.ensureService(ctx, host, hostruntime.Service(host)); err != nil {
		return ctrl.Result{}, err
	}
	actual := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: host.Name}, actual); err != nil {
		return ctrl.Result{}, err
	}
	phase := "Provisioning"
	if actual.Status.ReadyReplicas == 1 && actual.Status.ObservedGeneration >= actual.Generation {
		phase = "Ready"
		if host.Status.TemplateHash != template.Spec.Hash {
			host.Status.TemplateHash = template.Spec.Hash
			if err := r.Status().Update(ctx, host); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}
	return r.hostStatus(ctx, host, phase, "", port)
}

func (r *CsmReconciler) hostStatus(ctx context.Context, h *api.Csm, phase, msg string, port int32) (ctrl.Result, error) {
	next := h.Status
	next.Phase = phase
	next.Message = msg
	if r.JumpPort != 0 {
		next.SSHHost = api.SSHDomain(h.Namespace, h.Name, r.SSHDomainSuffix)
	}
	if port != 0 {
		next.SSHPort = port
	} else if phase == "Expired" || phase == "Recycled" || phase == "PendingPayment" {
		next.SSHPort = 0
	}
	if next.SystemPVCName == "" && phase == "Provisioning" {
		next.SystemPVCName = storage.ClaimName("system-", h.Name)
	}
	if !reflect.DeepEqual(next, h.Status) {
		h.Status = next
		if err := r.Status().Update(ctx, h); err != nil {
			return ctrl.Result{}, err
		}
	}
	if phase == "Provisioning" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if phase == "Ready" || phase == "Expired" {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	return ctrl.Result{}, nil
}

func (r *CsmReconciler) stopHost(ctx context.Context, h *api.Csm, recycle bool) error {
	if err := r.releasePort(ctx, h); err != nil {
		return err
	}
	key := types.NamespacedName{Namespace: h.Namespace, Name: h.Name}
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, key, dep); err == nil {
		if !metav1.IsControlledBy(dep, h) {
			return fmt.Errorf("Deployment %s is not owned by Csm", h.Name)
		}
		if err := r.Delete(ctx, dep); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	svc := &corev1.Service{}
	if err := r.Get(ctx, key, svc); err == nil {
		if !metav1.IsControlledBy(svc, h) {
			return fmt.Errorf("Service %s is not owned by Csm", h.Name)
		}
		if err := r.Delete(ctx, svc); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if recycle {
		if err := r.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, dep); err == nil {
			return fmt.Errorf("waiting for host Deployment deletion before system PVC recycle")
		} else if !apierrors.IsNotFound(err) {
			return err
		}
		name := h.Status.SystemPVCName
		if name == "" {
			name = storage.ClaimName("system-", h.Name)
		}
		pvc := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: name}, pvc); err == nil {
			if !metav1.IsControlledBy(pvc, h) {
				return fmt.Errorf("system PVC %s is not owned by Csm", name)
			}
			if err := r.Delete(ctx, pvc); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *CsmReconciler) ensureDeployment(ctx context.Context, h *api.Csm, desired *appsv1.Deployment) error {
	current := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, current)
	if apierrors.IsNotFound(err) {
		if err := r.setOwner(h, desired); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, h) {
		return fmt.Errorf("Deployment %s is not owned by Csm", h.Name)
	}
	if reflect.DeepEqual(current.Spec, desired.Spec) {
		return nil
	}
	current.Spec = desired.Spec
	return r.Update(ctx, current)
}

func (r *CsmReconciler) ensureService(ctx context.Context, h *api.Csm, desired *corev1.Service) error {
	current := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, current)
	if apierrors.IsNotFound(err) {
		if err := r.setOwner(h, desired); err != nil {
			return err
		}
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, h) {
		return fmt.Errorf("Service %s is not owned by Csm", h.Name)
	}
	if current.Spec.Type == desired.Spec.Type && reflect.DeepEqual(current.Spec.Selector, desired.Spec.Selector) && reflect.DeepEqual(current.Spec.Ports, desired.Spec.Ports) {
		return nil
	}
	current.Spec.Type = desired.Spec.Type
	current.Spec.Selector = desired.Spec.Selector
	current.Spec.Ports = desired.Spec.Ports
	return r.Update(ctx, current)
}
