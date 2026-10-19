package storage

import (
	"context"
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func ClaimName(prefix, name string) string {
	value := prefix + name
	if len(value) <= 63 {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s-%x", value[:50], sum[:6])
}

// EnsurePVC creates a claim or expands it. Shrinking and adopting another owner's claim are rejected.
func EnsurePVC(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, name, class string, sizeGi int64) (*corev1.PersistentVolumeClaim, error) {
	if sizeGi < 1 {
		return nil, fmt.Errorf("PVC %s size must be positive", name)
	}
	qty := resource.MustParse(fmt.Sprintf("%dGi", sizeGi))
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Namespace: owner.GetNamespace(), Name: name}
	err := c.Get(ctx, key, pvc)
	if apierrors.IsNotFound(err) {
		pvc = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: owner.GetNamespace()}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: qty}}}}
		if class != "" {
			pvc.Spec.StorageClassName = &class
		}
		if err := controllerutil.SetControllerReference(owner, pvc, scheme); err != nil {
			return nil, err
		}
		return pvc, c.Create(ctx, pvc)
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(pvc, owner) {
		return nil, fmt.Errorf("PVC %s is not owned by %s", name, owner.GetName())
	}
	if class != "" && (pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != class) {
		return nil, fmt.Errorf("PVC %s storage class cannot change", name)
	}
	current := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if qty.Cmp(current) < 0 {
		return nil, fmt.Errorf("PVC %s cannot shrink", name)
	}
	if qty.Cmp(current) > 0 {
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = qty
		if err := c.Update(ctx, pvc); err != nil {
			return nil, err
		}
	}
	return pvc, nil
}
