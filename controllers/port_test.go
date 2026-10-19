package controllers

import (
	"context"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodePortAllocation(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	h := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "host-uid"}}
	other := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "other"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{NodePort: 30000}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(other).Build()
	r := &CsmReconciler{Client: c, Scheme: s, PortStart: 30000, PortEnd: 30001}
	ctx := context.Background()
	port, err := r.ensurePort(ctx, h)
	if err != nil || port != 30001 {
		t.Fatalf("allocated port %d: %v", port, err)
	}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: h.Name, Namespace: h.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: "csm.w7.cc/v1alpha1", Kind: "Csm", Name: h.Name, UID: h.UID, Controller: ptr(true)}}}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{NodePort: port}}}}
	if err := c.Create(ctx, service); err != nil {
		t.Fatal(err)
	}
	if got, err := r.ensurePort(ctx, h); err != nil || got != port {
		t.Fatalf("existing host port %d: %v", got, err)
	}
	if got, err := r.ensurePort(ctx, &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "tenant"}}); err == nil || got != 0 {
		t.Fatalf("exhausted port pool returned %d: %v", got, err)
	}
}

func ptr(v bool) *bool { return &v }
