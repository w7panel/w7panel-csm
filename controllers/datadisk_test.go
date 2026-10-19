package controllers

import (
	"context"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDataDiskPaidPVCAndDetach(t *testing.T) {
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	d := &api.DataDisk{ObjectMeta: metav1.ObjectMeta{Name: "disk", Namespace: "tenant", UID: "uid"}, Spec: api.DataDiskSpec{StorageGi: 10, CsmName: "host", MountPath: "/data"}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&api.DataDisk{}).WithObjects(d).Build()
	r := &DataDiskReconciler{Client: c, Scheme: s}
	ctx := context.Background()
	key := types.NamespacedName{Namespace: d.Namespace, Name: d.Name}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(ctx, key, d); err != nil {
		t.Fatal(err)
	}
	if d.Status.Phase != "PendingPayment" {
		t.Fatalf("phase %q", d.Status.Phase)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: "data-disk"}, &corev1.PersistentVolumeClaim{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unpaid PVC: %v", err)
	}
	d.Spec.Paid = true
	if err := c.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	p := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: "data-disk"}, p); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, d); err != nil {
		t.Fatal(err)
	}
	if d.Status.PVCName != "data-disk" {
		t.Fatal("PVC identity not recorded")
	}
	// A detached purchased disk remains available even if its host is removed.
	p.Status.Phase = corev1.ClaimBound
	if err := c.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, d); err != nil {
		t.Fatal(err)
	}
	if d.Status.Phase != "Available" {
		t.Fatalf("phase %q", d.Status.Phase)
	}
}
