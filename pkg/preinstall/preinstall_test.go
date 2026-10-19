package preinstall

import (
	"context"
	"testing"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSnapshotOrdersAndFreezesShell(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	one := &csm.PreinstallApplication{ObjectMeta: metav1.ObjectMeta{Name: "one"}, Spec: csm.PreinstallApplicationSpec{Enabled: true, Order: 20, Shell: "echo one"}}
	two := &csm.PreinstallApplication{ObjectMeta: metav1.ObjectMeta{Name: "two"}, Spec: csm.PreinstallApplicationSpec{Enabled: true, Order: 10, Shell: "echo two"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(one, two).Build()
	steps, err := Snapshot(context.Background(), c, []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Name != "two" || steps[1].Shell != "echo one" {
		t.Fatalf("unexpected snapshot: %+v", steps)
	}
	one.Spec.Shell = "changed"
	if steps[1].Shell != "echo one" {
		t.Fatal("snapshot changed with source")
	}
	if _, err := Snapshot(context.Background(), c, []string{"one", "one"}); err == nil {
		t.Fatal("duplicate application accepted")
	}
}
