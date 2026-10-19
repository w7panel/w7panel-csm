package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/execshell"
	"github.com/w7panel/w7panel-csm/pkg/upgrade"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PreinstallExecutionReconciler struct {
	client.Client
	Runner execshell.Runner
}
type UpgradeExecutionReconciler struct {
	client.Client
	Runner execshell.Runner
}

func SetupPreinstallExecutionController(mgr ctrl.Manager, runner execshell.Runner) error {
	return ctrl.NewControllerManagedBy(mgr).For(&csm.PreinstallExecution{}).Complete(&PreinstallExecutionReconciler{Client: mgr.GetClient(), Runner: runner})
}
func SetupUpgradeExecutionController(mgr ctrl.Manager, runner execshell.Runner) error {
	return ctrl.NewControllerManagedBy(mgr).For(&csm.UpgradeExecution{}).Complete(&UpgradeExecutionReconciler{Client: mgr.GetClient(), Runner: runner})
}

func (r *PreinstallExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	execution := &csm.PreinstallExecution{}
	if err := r.Get(ctx, req.NamespacedName, execution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if execution.Status.Phase == "failed" || execution.Status.Phase == "succeeded" {
		return ctrl.Result{}, nil
	}
	host := &csm.Csm{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: execution.Spec.CsmNamespace, Name: execution.Spec.CsmName}, host); err != nil {
		return ctrl.Result{}, err
	}
	hash := host.Status.TemplateHash
	name := host.Spec.TemplateName
	if hash == "" && name == execution.Spec.TemplateName {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if hash != execution.Spec.TemplateHash || name != execution.Spec.TemplateName {
		return ctrl.Result{}, failPreinstall(ctx, r.Client, execution, "host template changed")
	}
	status := &execution.Status
	if err := runSteps(ctx, r.Runner, execution.Spec.CsmNamespace, execution.Spec.CsmName, execution.Spec.Steps, status, func() error { return r.Status().Update(ctx, execution) }); err != nil {
		return ctrl.Result{}, err
	}
	if status.Phase == "running" {
		status.Phase = "succeeded"
		now := metav1.Now()
		status.CompletedAt = &now
		return ctrl.Result{}, r.Status().Update(ctx, execution)
	}
	return ctrl.Result{}, nil
}

func failPreinstall(ctx context.Context, c client.Client, execution *csm.PreinstallExecution, message string) error {
	execution.Status.Phase, execution.Status.Message = "failed", message
	now := metav1.Now()
	execution.Status.CompletedAt = &now
	return c.Status().Update(ctx, execution)
}

func (r *UpgradeExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	execution := &csm.UpgradeExecution{}
	if err := r.Get(ctx, req.NamespacedName, execution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if execution.Status.Phase == "failed" || execution.Status.Phase == "succeeded" {
		return ctrl.Result{}, nil
	}
	if err := upgrade.ValidateApproval(execution.Spec); err != nil {
		return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, err.Error())
	}
	host := &csm.Csm{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: execution.Spec.CsmNamespace, Name: execution.Spec.CsmName}, host); err != nil {
		return ctrl.Result{}, err
	}
	name, hash, phase := host.Spec.TemplateName, host.Status.TemplateHash, host.Status.Phase
	if name == execution.Spec.TargetTemplateName && len(execution.Status.Completed) == len(execution.Spec.Steps) {
		if phase == "Ready" && hash == execution.Spec.TargetTemplateHash {
			execution.Status.Phase = "succeeded"
			now := metav1.Now()
			execution.Status.CompletedAt = &now
			return ctrl.Result{}, r.Status().Update(ctx, execution)
		}
		if execution.Status.AppliedAt == nil {
			now := metav1.Now()
			execution.Status.AppliedAt = &now
			execution.Status.Phase = "applying"
			if err := r.Status().Update(ctx, execution); err != nil {
				return ctrl.Result{}, err
			}
		}
		if phase == "Failed" || time.Since(execution.Status.AppliedAt.Time) > 10*time.Minute {
			message := "target template did not become Ready within 10 minutes"
			if phase == "Failed" {
				message = host.Status.Message
				if message == "" {
					message = "target template failed"
				}
			}
			base := host.DeepCopyObject().(*csm.Csm)
			host.Spec.TemplateName = execution.Spec.SourceTemplateName
			if err := r.Patch(ctx, host, client.MergeFrom(base)); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, message+"; restored original template selection")
		}
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}
	if name != execution.Spec.SourceTemplateName || hash != execution.Spec.SourceTemplateHash {
		return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, "source template changed")
	}
	if execution.Status.Phase == "" {
		if phase != "Ready" {
			return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
		}
		target := &csm.SystemTemplate{}
		if err := r.Get(ctx, client.ObjectKey{Name: execution.Spec.TargetTemplateName}, target); err != nil {
			return ctrl.Result{}, err
		}
		if target.Spec.Hash != execution.Spec.TargetTemplateHash {
			return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, "target template changed")
		}
		version := &csm.UpgradeVersion{}
		if err := r.Get(ctx, client.ObjectKey{Name: execution.Spec.VersionName}, version); err != nil {
			return ctrl.Result{}, err
		}
		if version.Spec.TemplateName != target.Name {
			return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, "version targets another template")
		}
	}
	if err := runSteps(ctx, r.Runner, execution.Spec.CsmNamespace, execution.Spec.CsmName, execution.Spec.Steps, &execution.Status, func() error { return r.Status().Update(ctx, execution) }); err != nil {
		return ctrl.Result{}, err
	}
	if execution.Status.Phase == "failed" {
		return ctrl.Result{}, nil
	}
	target := &csm.SystemTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Name: execution.Spec.TargetTemplateName}, target); err != nil {
		return ctrl.Result{}, err
	}
	if target.Spec.Hash != execution.Spec.TargetTemplateHash {
		return ctrl.Result{}, failUpgrade(ctx, r.Client, execution, "target template changed before apply")
	}
	base := host.DeepCopyObject().(*csm.Csm)
	host.Spec.TemplateName = execution.Spec.TargetTemplateName
	if err := r.Patch(ctx, host, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	now := metav1.Now()
	execution.Status.AppliedAt = &now
	execution.Status.Phase = "applying"
	if err := r.Status().Update(ctx, execution); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
}

func failUpgrade(ctx context.Context, c client.Client, execution *csm.UpgradeExecution, message string) error {
	execution.Status.Phase, execution.Status.Message = "failed", message
	now := metav1.Now()
	execution.Status.CompletedAt = &now
	return c.Status().Update(ctx, execution)
}

func runSteps(ctx context.Context, runner execshell.Runner, namespace, host string, steps []csm.ShellStep, status *csm.ExecutionStatus, save func() error) error {
	if status.Phase == "" {
		now := metav1.Now()
		status.StartedAt = &now
		status.Phase = "running"
		if err := save(); err != nil {
			return err
		}
	}
	completed := make(map[string]bool, len(status.Completed))
	for _, name := range status.Completed {
		completed[name] = true
	}
	for _, step := range steps {
		if completed[step.Name] {
			continue
		}
		if step.Name == "" || strings.TrimSpace(step.Shell) == "" {
			status.Phase, status.Message = "failed", "invalid shell step"
			now := metav1.Now()
			status.CompletedAt = &now
			return save()
		}
		status.Current = step.Name
		if err := save(); err != nil {
			return err
		}
		output, err := runner.Run(ctx, namespace, host, step.Shell)
		if errors.Is(err, execshell.ErrPodUnavailable) {
			status.Current, status.Message = "", err.Error()
			if saveErr := save(); saveErr != nil {
				return saveErr
			}
			return err
		}
		if err != nil {
			output += "\n" + err.Error()
			status.Phase, status.Message = "failed", err.Error()
			now := metav1.Now()
			status.CompletedAt = &now
		}
		status.Log += fmt.Sprintf("[%s]\n%s\n", step.Name, output)
		if len(status.Log) > 65536 {
			status.Log = status.Log[len(status.Log)-65536:]
		}
		status.Current = ""
		if err == nil {
			status.Completed = append(status.Completed, step.Name)
		}
		if saveErr := save(); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return nil
		}
	}
	// Upgrade completion is recorded after the host's template is updated.
	return nil
}
