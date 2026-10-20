package upgrade

import (
	"context"
	"testing"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewExecutionRequiresApproval(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	target := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-next"}, Spec: csm.SystemTemplateSpec{Version: "next", Source: "official", Image: "ubuntu:next", Hash: "next-hash"}}
	version := &csm.UpgradeVersion{ObjectMeta: metav1.ObjectMeta{Name: "next"}, Spec: csm.UpgradeVersionSpec{TemplateName: target.Name, Version: "1.0.0"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(target, version).Build()
	host := &csm.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "users"}, Spec: csm.CsmSpec{TemplateName: "ubuntu-old"}, Status: csm.CsmStatus{TemplateHash: "old-hash"}}
	if _, err := NewExecution(context.Background(), c, host, "next", ""); err == nil {
		t.Fatal("unapproved upgrade accepted")
	}
	execution, err := NewExecution(context.Background(), c, host, "next", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Spec.SourceTemplateHash != "old-hash" || execution.Spec.TargetTemplateHash != target.Spec.Hash {
		t.Fatalf("unexpected upgrade snapshot: %+v", execution.Spec)
	}
}
