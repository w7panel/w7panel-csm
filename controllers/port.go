package controllers

import (
	"context"
	"fmt"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *CsmReconciler) ensurePort(ctx context.Context, host *api.Csm) (int32, error) {
	if r.PortStart < 1 || r.PortEnd < r.PortStart || r.PortEnd > 65535 {
		return 0, fmt.Errorf("invalid SSH port pool configuration")
	}
	service := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: host.Name}, service)
	if err == nil {
		if !metav1.IsControlledBy(service, host) {
			return 0, fmt.Errorf("Service %s is not owned by Csm", host.Name)
		}
		if len(service.Spec.Ports) == 1 && service.Spec.Ports[0].NodePort != 0 {
			return service.Spec.Ports[0].NodePort, nil
		}
	} else if !apierrors.IsNotFound(err) {
		return 0, err
	}
	services := &corev1.ServiceList{}
	if err := r.List(ctx, services); err != nil {
		return 0, err
	}
	used := map[int32]bool{}
	for _, service := range services.Items {
		for _, port := range service.Spec.Ports {
			used[port.NodePort] = true
		}
	}
	for port := r.PortStart; port <= r.PortEnd; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("SSH port pool exhausted")
}
