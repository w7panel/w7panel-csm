package controllers

import (
	"context"
	"testing"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFirstHostCreationSnapshotsPreinstall(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	host := &csm.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "users", UID: types.UID("host-uid")}, Spec: csm.CsmSpec{TemplateName: "ubuntu"}}
	template := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu"}, Spec: csm.SystemTemplateSpec{Version: "24.04", Source: "official", Image: "ubuntu:24.04", Hash: "hash", PreinstallApplications: []string{"shell"}}}
	app := &csm.PreinstallApplication{ObjectMeta: metav1.ObjectMeta{Name: "shell"}, Spec: csm.PreinstallApplicationSpec{Enabled: true, Order: 1, Shell: "echo original"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(host).WithObjects(host, template, app).Build()
	r := &CsmTemplateReconciler{Client: c}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: host.Namespace, Name: host.Name}}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	execution := &csm.PreinstallExecution{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "preinstall-host-uid"}, execution); err != nil {
		t.Fatal(err)
	}
	if len(execution.Spec.Steps) != 1 || execution.Spec.Steps[0].Shell != "echo original" {
		t.Fatalf("wrong snapshot: %+v", execution.Spec)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(host), host); err != nil {
		t.Fatal(err)
	}
	if host.Status.TemplateHash != "" {
		t.Fatal("template hash was set before host became Ready")
	}
	app.Spec.Shell = "echo changed"
	if err := c.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(execution), execution); err != nil {
		t.Fatal(err)
	}
	if execution.Spec.Steps[0].Shell != "echo original" {
		t.Fatal("existing execution changed after application edit")
	}
}
