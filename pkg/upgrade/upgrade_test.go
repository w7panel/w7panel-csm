package upgrade

import (
	"context"
	"testing"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewExecutionRequiresApprovalAndSnapshotsPolicies(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	target := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-next"}, Spec: csm.SystemTemplateSpec{Version: "next", Source: "official", Image: "ubuntu:next", Hash: "next-hash"}}
	version := &csm.UpgradeVersion{ObjectMeta: metav1.ObjectMeta{Name: "next"}, Spec: csm.UpgradeVersionSpec{TemplateName: target.Name, Version: "1.0.0", Policies: []string{"late", "early"}}}
	late := &csm.UpgradePolicy{ObjectMeta: metav1.ObjectMeta{Name: "late"}, Spec: csm.UpgradePolicySpec{VersionName: "next", Enabled: true, Order: 2, Shell: "echo late"}}
	early := &csm.UpgradePolicy{ObjectMeta: metav1.ObjectMeta{Name: "early"}, Spec: csm.UpgradePolicySpec{VersionName: "next", Enabled: true, Order: 1, Shell: "echo early"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(target, version, late, early).Build()
	host := &csm.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "users"}, Spec: csm.CsmSpec{TemplateName: "ubuntu-old"}, Status: csm.CsmStatus{TemplateHash: "old-hash"}}
	if _, err := NewExecution(context.Background(), c, host, "next", ""); err == nil {
		t.Fatal("unapproved upgrade accepted")
	}
	execution, err := NewExecution(context.Background(), c, host, "next", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Spec.SourceTemplateHash != "old-hash" || execution.Spec.TargetTemplateHash != target.Spec.Hash || execution.Spec.Steps[0].Name != "early" {
		t.Fatalf("unexpected upgrade snapshot: %+v", execution.Spec)
	}
}
