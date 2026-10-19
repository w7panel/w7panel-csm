package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureOfficialSystemTemplate installs the first selectable Linux template.
func EnsureOfficialSystemTemplate(ctx context.Context, c client.Client) error {
	image := os.Getenv("CSM_SYSTEM_IMAGE")
	if image == "" {
		image = "w7panel-csm-system:ubuntu-24.04"
	}
	digest := sha256.Sum256([]byte(image))
	template := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-24.04"}, Spec: csm.SystemTemplateSpec{
		Version: "24.04", Source: "official", Image: image, Hash: hex.EncodeToString(digest[:]),
	}}
	if err := c.Create(ctx, template); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
