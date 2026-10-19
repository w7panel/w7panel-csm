package storage

import (
	"context"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsurePVCExpandAndRejectShrink(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	h := &api.Csm{}
	h.Name = "host"
	h.Namespace = "tenant"
	h.UID = "uid"
	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()
	if _, err := EnsurePVC(ctx, c, s, h, "system-host", "standard", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePVC(ctx, c, s, h, "system-host", "standard", 20); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePVC(ctx, c, s, h, "system-host", "standard", 5); err == nil {
		t.Fatal("PVC shrank")
	}
	p := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "tenant", Name: "system-host"}, p); err != nil {
		t.Fatal(err)
	}
	if got := p.Spec.Resources.Requests[corev1.ResourceStorage]; got.Value() != 20<<30 {
		t.Fatalf("size %s", got.String())
	}
}
