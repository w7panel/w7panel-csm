package controllers

import (
	"context"
	"fmt"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/preinstall"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CsmTemplateReconciler freezes preinstall inputs on the host's first creation.
type CsmTemplateReconciler struct{ client.Client }

func SetupCsmTemplateController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("csm-template").For(&csm.Csm{}).Complete(&CsmTemplateReconciler{Client: mgr.GetClient()})
}

func (r *CsmTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	host := &csm.Csm{}
	if err := r.Get(ctx, req.NamespacedName, host); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	name := host.Spec.TemplateName
	if name == "" {
		return ctrl.Result{}, nil
	}
	hash := host.Status.TemplateHash
	execution := &csm.PreinstallExecution{}
	execution.Name = "preinstall-" + string(host.GetUID())
	if err := r.Get(ctx, types.NamespacedName{Name: execution.Name}, execution); err == nil {
		if execution.Spec.CsmNamespace != host.GetNamespace() || execution.Spec.CsmName != host.GetName() {
			return ctrl.Result{}, fmt.Errorf("preinstall execution name collision for %s/%s", host.GetNamespace(), host.GetName())
		}
		return ctrl.Result{}, nil
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	template := &csm.SystemTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Name: name}, template); err != nil {
		return ctrl.Result{}, err
	}
	if template.Spec.Image == "" || template.Spec.Hash == "" {
		return ctrl.Result{}, fmt.Errorf("template %q requires image and hash", name)
	}
	if hash != "" && hash != template.Spec.Hash {
		return ctrl.Result{}, nil
	}
	steps, err := preinstall.Snapshot(ctx, r.Client, template.Spec.PreinstallApplications)
	if err != nil {
		return ctrl.Result{}, err
	}
	hash = template.Spec.Hash
	execution.Spec = csm.PreinstallExecutionSpec{CsmNamespace: host.GetNamespace(), CsmName: host.GetName(), TemplateName: name, TemplateHash: hash, Steps: steps}
	if err := r.Create(ctx, execution); err != nil {
		if getErr := r.Get(ctx, types.NamespacedName{Name: execution.Name}, execution); getErr != nil {
			return ctrl.Result{}, err
		}
		if execution.Spec.CsmNamespace != host.GetNamespace() || execution.Spec.CsmName != host.GetName() {
			return ctrl.Result{}, fmt.Errorf("preinstall execution name collision for %s/%s", host.GetNamespace(), host.GetName())
		}
		if execution.Spec.TemplateHash != hash {
			return ctrl.Result{}, nil
		}
	}
	return ctrl.Result{}, nil
}
