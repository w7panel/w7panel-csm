package controllers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/execshell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testRunner struct {
	calls []string
	fail  string
	wait  bool
}

func (r *testRunner) Run(_ context.Context, _, _, shell string) (string, error) {
	r.calls = append(r.calls, shell)
	if r.wait {
		return "", execshell.ErrPodUnavailable
	}
	if shell == r.fail {
		return "partial output", errors.New("exit 1")
	}
	return "ok", nil
}

func TestRunStepsWaitsForReadyPod(t *testing.T) {
	runner := &testRunner{wait: true}
	status := &csm.ExecutionStatus{}
	err := runSteps(context.Background(), runner, "ns", "host", []csm.ShellStep{{Name: "install", Shell: "echo ready"}}, status, func() error { return nil })
	if !errors.Is(err, execshell.ErrPodUnavailable) || status.Phase != "running" || len(status.Completed) != 0 {
		t.Fatalf("pod wait became a failed execution: %+v, %v", status, err)
	}
}

func TestRunStepsStopsAndRecordsFailure(t *testing.T) {
	runner := &testRunner{fail: "bad"}
	status := &csm.ExecutionStatus{}
	steps := []csm.ShellStep{{Name: "first", Shell: "good"}, {Name: "second", Shell: "bad"}, {Name: "third", Shell: "never"}}
	if err := runSteps(context.Background(), runner, "ns", "host", steps, status, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if status.Phase != "failed" || len(status.Completed) != 1 || len(runner.calls) != 2 || !strings.Contains(status.Log, "partial output") || !strings.Contains(status.Log, "exit 1") {
		t.Fatalf("failure not preserved: %+v, calls: %v", status, runner.calls)
	}
}

func TestUpgradeFailureKeepsHostTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	host := &csm.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "users"}, Spec: csm.CsmSpec{TemplateName: "old"}, Status: csm.CsmStatus{TemplateHash: "old-hash", Phase: "Ready"}}
	target := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "new"}, Spec: csm.SystemTemplateSpec{Version: "2", Source: "official", Image: "ubuntu:2", Hash: "new-hash"}}
	version := &csm.UpgradeVersion{ObjectMeta: metav1.ObjectMeta{Name: "v2"}, Spec: csm.UpgradeVersionSpec{TemplateName: "new", Version: "2"}}
	execution := &csm.UpgradeExecution{ObjectMeta: metav1.ObjectMeta{Name: "upgrade-host"}, Spec: csm.UpgradeExecutionSpec{CsmNamespace: "users", CsmName: "host", ApprovedBy: "alice", SourceTemplateName: "old", SourceTemplateHash: "old-hash", TargetTemplateName: "new", TargetTemplateHash: "new-hash", VersionName: "v2", Steps: []csm.ShellStep{{Name: "step", Shell: "bad"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(host, execution).WithObjects(host, target, version, execution).Build()
	r := &UpgradeExecutionReconciler{Client: c, Runner: &testRunner{fail: "bad"}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: execution.Name}}); err != nil {
		t.Fatal(err)
	}
	got := &csm.Csm{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(host), got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.TemplateName != "old" || got.Status.TemplateHash != "old-hash" {
		t.Fatalf("failed upgrade changed host template: %+v", got)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(execution), execution); err != nil {
		t.Fatal(err)
	}
	if execution.Status.Phase != "failed" || !strings.Contains(execution.Status.Log, "partial output") {
		t.Fatalf("failure log missing: %+v", execution.Status)
	}
}

func TestUpgradeWaitsForReadyAndRollsBackFailure(t *testing.T) {
	for _, outcome := range []string{"Ready", "Failed", "TimedOut"} {
		t.Run(outcome, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := csm.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			host := &csm.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "users"}, Spec: csm.CsmSpec{TemplateName: "old"}, Status: csm.CsmStatus{TemplateHash: "old-hash", Phase: "Ready"}}
			target := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "new"}, Spec: csm.SystemTemplateSpec{Version: "2", Source: "official", Image: "ubuntu:2", Hash: "new-hash"}}
			version := &csm.UpgradeVersion{ObjectMeta: metav1.ObjectMeta{Name: "v2"}, Spec: csm.UpgradeVersionSpec{TemplateName: "new", Version: "2"}}
			execution := &csm.UpgradeExecution{ObjectMeta: metav1.ObjectMeta{Name: "upgrade-host"}, Spec: csm.UpgradeExecutionSpec{CsmNamespace: "users", CsmName: "host", ApprovedBy: "alice", SourceTemplateName: "old", SourceTemplateHash: "old-hash", TargetTemplateName: "new", TargetTemplateHash: "new-hash", VersionName: "v2", Steps: []csm.ShellStep{{Name: "step", Shell: "good"}}}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(host, execution).WithObjects(host, target, version, execution).Build()
			r := &UpgradeExecutionReconciler{Client: c, Runner: &testRunner{}}
			request := ctrl.Request{NamespacedName: types.NamespacedName{Name: execution.Name}}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(host), host); err != nil {
				t.Fatal(err)
			}
			if host.Spec.TemplateName != "new" || host.Status.TemplateHash != "old-hash" {
				t.Fatalf("upgrade reported target too early: %+v", host)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(execution), execution); err != nil {
				t.Fatal(err)
			}
			if execution.Status.Phase != "applying" {
				t.Fatalf("upgrade should wait: %+v", execution.Status)
			}
			host.Status.Phase = outcome
			if outcome == "TimedOut" {
				host.Status.Phase = "Provisioning"
				old := metav1.NewTime(time.Now().Add(-11 * time.Minute))
				execution.Status.AppliedAt = &old
				if err := c.Status().Update(context.Background(), execution); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "Ready" {
				host.Status.TemplateHash = "new-hash"
			}
			if err := c.Status().Update(context.Background(), host); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(host), host); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(execution), execution); err != nil {
				t.Fatal(err)
			}
			if outcome == "Ready" && (host.Spec.TemplateName != "new" || execution.Status.Phase != "succeeded") {
				t.Fatalf("ready upgrade not completed: %+v %+v", host.Spec, execution.Status)
			}
			if (outcome == "Failed" || outcome == "TimedOut") && (host.Spec.TemplateName != "old" || execution.Status.Phase != "failed") {
				t.Fatalf("failed upgrade not rolled back: %+v %+v", host.Spec, execution.Status)
			}
		})
	}
}
