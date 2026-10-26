package controllers

import (
	"context"
	"testing"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUnpaidHostCreatesNothing(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	h := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "uid"}, Spec: api.CsmSpec{TemplateName: "ubuntu", SystemDiskGi: 10}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&api.Csm{}).WithObjects(h).Build()
	r := &CsmReconciler{Client: c, Scheme: s}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "tenant", Name: "host"}}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "tenant", Name: "system-host"}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unpaid PVC: %v", err)
	}
	if err := c.Get(context.Background(), req.NamespacedName, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unpaid Deployment: %v", err)
	}
}

func TestExpiredHostPreservesDisks(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	h := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "uid", Finalizers: []string{api.CsmFinalizer}}, Spec: api.CsmSpec{Paid: true, TemplateName: "ubuntu", SystemDiskGi: 10, ExpireTime: time.Now().Add(-time.Hour).Format(time.RFC3339), RecycleTime: time.Now().Add(time.Hour).Format(time.RFC3339)}, Status: api.CsmStatus{SystemPVCName: "system-host"}}
	owner := metav1.OwnerReference{APIVersion: "csm.w7.cc/v1alpha1", Kind: "Csm", Name: h.Name, UID: h.UID, Controller: boolPtr(true)}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: h.Name, Namespace: h.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}}
	system := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "system-host", Namespace: h.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}}
	data := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data-disk", Namespace: h.Namespace}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&api.Csm{}).WithObjects(h, dep, system, data).Build()
	r := &CsmReconciler{Client: c, Scheme: s}
	key := types.NamespacedName{Namespace: h.Namespace, Name: h.Name}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), key, &appsv1.Deployment{}); !apierrors.IsNotFound(err) {
		t.Fatalf("host still running: %v", err)
	}
	for _, name := range []string{"system-host", "data-disk"} {
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: name}, &corev1.PersistentVolumeClaim{}); err != nil {
			t.Fatalf("disk %s lost: %v", name, err)
		}
	}
	if err := c.Get(context.Background(), key, h); err != nil {
		t.Fatal(err)
	}
	h.Spec.RecycleTime = time.Now().Add(-time.Minute).Format(time.RFC3339)
	if err := c.Update(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: "system-host"}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("system disk not recycled: %v", err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: "data-disk"}, &corev1.PersistentVolumeClaim{}); err != nil {
		t.Fatalf("data disk lost on recycle: %v", err)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestPaidHostTemplateAndReadyHash(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	s.AddKnownTypeWithName(gatewayGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(routeGVK, &unstructured.Unstructured{})
	h := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "uid"}, Spec: api.CsmSpec{Paid: true, TemplateName: "ubuntu", SystemDiskGi: 10, SSHPublicKeys: []string{"ssh-ed25519 YWJj test"}}}
	tpl := &api.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu"}, Spec: api.SystemTemplateSpec{Image: "ubuntu:24.04", Hash: "template-hash"}}
	gw := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway", "metadata": map[string]any{"name": "shared", "namespace": "network"}, "spec": map[string]any{"listeners": []any{}}}}
	owner := metav1.OwnerReference{APIVersion: "csm.w7.cc/v1alpha1", Kind: "Csm", Name: h.Name, UID: h.UID, Controller: boolPtr(true)}
	oldService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: h.Name, Namespace: h.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Selector: map[string]string{"csm.w7.cc/host": h.Name}, Ports: []corev1.ServicePort{{Name: "ssh", Port: 22, NodePort: 30000}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&api.Csm{}, &appsv1.Deployment{}).WithObjects(h, tpl, gw, oldService).Build()
	r := &CsmReconciler{Client: c, Scheme: s, GatewayNamespace: "network", GatewayName: "shared", PortStart: 30000, PortEnd: 30001}
	ctx := context.Background()
	key := types.NamespacedName{Namespace: "tenant", Name: "host"}
	for i := 0; i < 3; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, key, dep); err != nil {
		t.Fatal(err)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "ubuntu:24.04" {
		t.Fatal("template image ignored")
	}
	if err := c.Get(ctx, key, h); err != nil {
		t.Fatal(err)
	}
	if h.Status.SSHPort != 30000 {
		t.Fatalf("port %d", h.Status.SSHPort)
	}
	service := &corev1.Service{}
	if err := c.Get(ctx, key, service); err != nil {
		t.Fatal(err)
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.Ports[0].NodePort != 0 || service.Spec.Ports[0].Port != 22 || service.Spec.Ports[0].TargetPort.IntVal != 2222 {
		t.Fatalf("host SSH Service was not migrated: %#v", service.Spec)
	}
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	if err := c.Get(ctx, key, route); err != nil {
		t.Fatalf("host TCPRoute missing: %v", err)
	}
	dep.Status.ReadyReplicas = 1
	dep.Status.ObservedGeneration = dep.Generation
	if err := c.Status().Update(ctx, dep); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, h); err != nil {
		t.Fatal(err)
	}
	if h.Status.TemplateHash != tpl.Spec.Hash {
		t.Fatalf("hash %q", h.Status.TemplateHash)
	}
}

func TestCloudInitSeedPrecedesDeploymentAndGatesReady(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	s.AddKnownTypeWithName(gatewayGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(routeGVK, &unstructured.Unstructured{})
	h := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "uid"}, Spec: api.CsmSpec{Paid: true, TemplateName: "ubuntu-cloud", SystemDiskGi: 10}}
	tpl := &api.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-cloud"}, Spec: api.SystemTemplateSpec{Image: "ubuntu:cloud", Hash: "hash", InitMode: "cloudInit", PreinstallApplications: []string{"setup"}}}
	app := &api.PreinstallApplication{ObjectMeta: metav1.ObjectMeta{Name: "setup"}, Spec: api.PreinstallApplicationSpec{Enabled: true, CloudConfig: "#cloud-config\npackages: [curl]\n"}}
	gw := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway", "metadata": map[string]any{"name": "shared", "namespace": "network"}, "spec": map[string]any{"listeners": []any{}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&api.Csm{}, &appsv1.Deployment{}, &api.PreinstallExecution{}).WithObjects(h, tpl, app, gw).Build()
	r := &CsmReconciler{Client: c, Scheme: s, GatewayNamespace: "network", GatewayName: "shared", PortStart: 30000, PortEnd: 30001}
	ctx := context.Background()
	key := types.NamespacedName{Namespace: h.Namespace, Name: h.Name}
	for i := 0; i < 5; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	seed := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: "csm-cloudinit-uid"}, seed); err != nil {
		t.Fatal(err)
	}
	if string(seed.Data["user-data"]) == "" || string(seed.Data["meta-data"]) != "instance-id: uid\nlocal-hostname: host\n" {
		t.Fatalf("invalid seed: %v", seed.Data)
	}
	initialUserData := string(seed.Data["user-data"])
	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, key, dep); err != nil {
		t.Fatal(err)
	}
	if len(dep.Spec.Template.Spec.Containers[0].VolumeMounts) == 0 {
		t.Fatal("seed not mounted")
	}
	dep.Status.ReadyReplicas = 1
	dep.Status.ObservedGeneration = dep.Generation
	if err := c.Status().Update(ctx, dep); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(ctx, key, h); err != nil {
		t.Fatal(err)
	}
	if h.Status.Phase != "Provisioning" || h.Status.TemplateHash != "hash" {
		t.Fatalf("ready before cloud-init: %+v", h.Status)
	}
	execution := &api.PreinstallExecution{}
	if err := c.Get(ctx, types.NamespacedName{Name: "preinstall-uid"}, execution); err != nil {
		t.Fatal(err)
	}
	execution.Status.Phase = "succeeded"
	if err := c.Status().Update(ctx, execution); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, h); err != nil {
		t.Fatal(err)
	}
	if h.Status.Phase != "Ready" {
		t.Fatalf("cloud-init completion not reflected: %+v", h.Status)
	}
	app.Spec.CloudConfig = "#cloud-config\npackages: [changed]\n"
	if err := c.Update(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: seed.Name}, seed); err != nil {
		t.Fatal(err)
	}
	if string(seed.Data["user-data"]) != initialUserData {
		t.Fatal("seed changed after config edit")
	}
}
