package controllers

import (
	"context"
	"fmt"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/preinstall"
	hostruntime "github.com/w7panel/w7panel-csm/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureCloudInit freezes user-data before the host Deployment can boot.
func (r *CsmReconciler) ensureCloudInit(ctx context.Context, host *api.Csm, template *api.SystemTemplate) (*api.PreinstallExecution, error) {
	if host.UID == "" {
		return nil, fmt.Errorf("CSM host UID is missing")
	}
	name := hostruntime.CloudInitSecretName(host)
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		userData, err := preinstall.CloudConfigSnapshot(ctx, r.Client, template.Spec.PreinstallApplications)
		if err != nil {
			return nil, err
		}
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: host.Namespace}, Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"user-data": []byte(userData), "meta-data": []byte(fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", host.UID, host.Name))}}
		if err := r.setOwner(host, secret); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		err = r.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: name}, secret)
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(secret, host) || len(secret.Data["user-data"]) == 0 || len(secret.Data["meta-data"]) == 0 {
		return nil, fmt.Errorf("invalid CSM cloud-init seed %s/%s", host.Namespace, name)
	}
	execution := &api.PreinstallExecution{}
	execution.Name = "preinstall-" + string(host.UID)
	err = r.Get(ctx, client.ObjectKey{Name: execution.Name}, execution)
	if apierrors.IsNotFound(err) {
		execution = &api.PreinstallExecution{ObjectMeta: metav1.ObjectMeta{Name: execution.Name}, Spec: api.PreinstallExecutionSpec{
			CsmNamespace: host.Namespace, CsmName: host.Name, TemplateName: template.Name, TemplateHash: template.Spec.Hash, InitMode: "cloudInit"}}
		if err := r.Create(ctx, execution); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		err = r.Get(ctx, client.ObjectKey{Name: execution.Name}, execution)
	}
	if err != nil {
		return nil, err
	}
	if execution.Spec.CsmNamespace != host.Namespace || execution.Spec.CsmName != host.Name || execution.Spec.InitMode != "cloudInit" {
		return nil, fmt.Errorf("cloud-init execution collision for %s/%s", host.Namespace, host.Name)
	}
	return execution, nil
}
