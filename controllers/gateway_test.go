package controllers

import (
	"context"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGatewayPortPreservesListeners(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	s.AddKnownTypeWithName(gatewayGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(routeGVK, &unstructured.Unstructured{})
	gw := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway", "metadata": map[string]any{"name": "shared", "namespace": "network"}, "spec": map[string]any{"listeners": []any{map[string]any{"name": "https", "port": int64(443), "protocol": "HTTPS"}}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(gw).Build()
	r := &CsmReconciler{Client: c, Scheme: s, GatewayName: "shared", GatewayNamespace: "network", PortStart: 30000, PortEnd: 30001}
	a := &api.Csm{}
	a.Name = "a"
	a.Namespace = "tenant"
	a.UID = "uid-a"
	b := &api.Csm{}
	b.Name = "b"
	b.Namespace = "tenant"
	ctx := context.Background()
	p, err := r.ensurePort(ctx, a)
	if err != nil || p != 30000 {
		t.Fatalf("first port %d: %v", p, err)
	}
	if err := r.ensureRoute(ctx, a); err != nil {
		t.Fatal(err)
	}
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "tenant", Name: "a"}, route); err != nil {
		t.Fatal(err)
	}
	if route.GetAPIVersion() != "gateway.networking.k8s.io/v1alpha2" {
		t.Fatalf("TCPRoute version %s", route.GetAPIVersion())
	}
	p, err = r.ensurePort(ctx, b)
	if err != nil || p != 30001 {
		t.Fatalf("second port %d: %v", p, err)
	}
	if _, err = r.ensurePort(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ensurePort(ctx, &api.Csm{}); err == nil {
		t.Fatal("pool should be exhausted")
	}
	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(gatewayGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "network", Name: "shared"}, stored); err != nil {
		t.Fatal(err)
	}
	listeners, _, _ := unstructured.NestedSlice(stored.Object, "spec", "listeners")
	if len(listeners) != 3 || listeners[0].(map[string]any)["name"] != "https" {
		t.Fatalf("listeners changed: %#v", listeners)
	}
}

func TestGatewayReleaseRemovesOnlyHostListener(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	s.AddKnownTypeWithName(gatewayGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(routeGVK, &unstructured.Unstructured{})
	gw := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway", "metadata": map[string]any{"name": "shared", "namespace": "network"}, "spec": map[string]any{"listeners": []any{map[string]any{"name": "https", "port": int64(443), "protocol": "HTTPS"}}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(gw).Build()
	r := &CsmReconciler{Client: c, Scheme: s, GatewayName: "shared", GatewayNamespace: "network", PortStart: 30000, PortEnd: 30001}
	h := &api.Csm{}
	h.Name, h.Namespace, h.UID = "host", "tenant", "uid"
	ctx := context.Background()
	if _, err := r.ensurePort(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureRoute(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := r.releasePort(ctx, h); err != nil {
		t.Fatal(err)
	}
	stored := &unstructured.Unstructured{}
	stored.SetGroupVersionKind(gatewayGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "network", Name: "shared"}, stored); err != nil {
		t.Fatal(err)
	}
	listeners, _, _ := unstructured.NestedSlice(stored.Object, "spec", "listeners")
	if len(listeners) != 1 || listeners[0].(map[string]any)["name"] != "https" {
		t.Fatalf("listeners after release: %#v", listeners)
	}
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "tenant", Name: "host"}, route); !apierrors.IsNotFound(err) {
		t.Fatalf("TCPRoute not removed: %v", err)
	}
}
