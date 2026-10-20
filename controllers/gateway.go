package controllers

import (
	"context"
	"crypto/sha256"
	"fmt"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var gatewayGVK = schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway"}
var routeGVK = schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Kind: "TCPRoute"}

func listenerName(host *api.Csm) string {
	sum := sha256.Sum256([]byte(host.Namespace + "/" + host.Name))
	return fmt.Sprintf("csm-%x", sum[:6])
}

func (r *CsmReconciler) ensurePort(ctx context.Context, host *api.Csm) (int32, error) {
	if r.GatewayName == "" || r.GatewayNamespace == "" || r.PortStart < 1 || r.PortEnd < r.PortStart || r.PortEnd > 65535 {
		return 0, fmt.Errorf("invalid Gateway or SSH port pool configuration")
	}
	var port int32
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		gw := &unstructured.Unstructured{}
		gw.SetGroupVersionKind(gatewayGVK)
		if err := r.Get(ctx, types.NamespacedName{Namespace: r.GatewayNamespace, Name: r.GatewayName}, gw); err != nil {
			return err
		}
		listeners, _, err := unstructured.NestedSlice(gw.Object, "spec", "listeners")
		if err != nil {
			return err
		}
		used := map[int32]bool{}
		for _, raw := range listeners {
			item, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Gateway listener")
			}
			p, ok := item["port"].(int64)
			if !ok {
				return fmt.Errorf("invalid Gateway listener port")
			}
			used[int32(p)] = true
			if item["name"] == listenerName(host) {
				port = int32(p)
				return nil
			}
		}
		for p := r.PortStart; p <= r.PortEnd; p++ {
			if !used[p] {
				port = p
				break
			}
		}
		if port == 0 {
			return fmt.Errorf("SSH port pool exhausted")
		}
		listeners = append(listeners, map[string]any{"name": listenerName(host), "port": int64(port), "protocol": "TCP", "allowedRoutes": map[string]any{"kinds": []any{map[string]any{"kind": "TCPRoute"}}, "namespaces": map[string]any{"from": "Selector", "selector": map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": host.Namespace}}}}})
		if err := unstructured.SetNestedSlice(gw.Object, listeners, "spec", "listeners"); err != nil {
			return err
		}
		return r.Update(ctx, gw)
	})
	return port, err
}

func (r *CsmReconciler) ensureRoute(ctx context.Context, host *api.Csm) error {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	key := types.NamespacedName{Namespace: host.Namespace, Name: host.Name}
	err := r.Get(ctx, key, route)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if err == nil && !metav1.IsControlledBy(route, host) {
		return fmt.Errorf("TCPRoute %s is not owned by Csm", host.Name)
	}
	desired := map[string]any{"parentRefs": []any{map[string]any{"name": r.GatewayName, "namespace": r.GatewayNamespace, "sectionName": listenerName(host)}}, "rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": host.Name, "port": int64(22)}}}}}
	if err != nil {
		route = &unstructured.Unstructured{}
		route.SetGroupVersionKind(routeGVK)
		route.SetName(host.Name)
		route.SetNamespace(host.Namespace)
		route.Object["spec"] = desired
		if err := r.setOwner(host, route); err != nil {
			return err
		}
		return r.Create(ctx, route)
	}
	old, _, _ := unstructured.NestedMap(route.Object, "spec")
	if fmt.Sprint(old) == fmt.Sprint(desired) {
		return nil
	}
	route.Object["spec"] = desired
	return r.Update(ctx, route)
}

func (r *CsmReconciler) releasePort(ctx context.Context, host *api.Csm) error {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	route.SetName(host.Name)
	route.SetNamespace(host.Namespace)
	key := types.NamespacedName{Namespace: host.Namespace, Name: host.Name}
	if err := r.Get(ctx, key, route); client.IgnoreNotFound(err) != nil {
		return err
	} else if err == nil {
		if !metav1.IsControlledBy(route, host) {
			return fmt.Errorf("TCPRoute %s is not owned by Csm", host.Name)
		}
		if route.GetDeletionTimestamp() == nil {
			if err := r.Delete(ctx, route); err != nil {
				return err
			}
		}
		if err := r.Get(ctx, key, route); err == nil {
			return fmt.Errorf("waiting for TCPRoute %s deletion", host.Name)
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		gw := &unstructured.Unstructured{}
		gw.SetGroupVersionKind(gatewayGVK)
		if err := r.Get(ctx, types.NamespacedName{Namespace: r.GatewayNamespace, Name: r.GatewayName}, gw); err != nil {
			return client.IgnoreNotFound(err)
		}
		listeners, _, err := unstructured.NestedSlice(gw.Object, "spec", "listeners")
		if err != nil {
			return err
		}
		kept := listeners[:0]
		for _, raw := range listeners {
			item, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Gateway listener")
			}
			if item["name"] != listenerName(host) {
				kept = append(kept, raw)
			}
		}
		if len(kept) == len(listeners) {
			return nil
		}
		if err := unstructured.SetNestedSlice(gw.Object, kept, "spec", "listeners"); err != nil {
			return err
		}
		return r.Update(ctx, gw)
	})
}
