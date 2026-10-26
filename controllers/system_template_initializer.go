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
		image = "docker.cnb.cool/i0358/w7panel-csm:system-ubuntu-24.04-test-20261019"
	}
	digest := sha256.Sum256([]byte(image))
	template := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-24.04"}, Spec: csm.SystemTemplateSpec{
		Version: "24.04", Source: "official", Image: image, Hash: hex.EncodeToString(digest[:]),
	}}
	if err := c.Create(ctx, template); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	cloudImage := os.Getenv("CSM_CLOUD_INIT_SYSTEM_IMAGE")
	if cloudImage == "" {
		return nil
	}
	cloudDigest := sha256.Sum256([]byte(cloudImage))
	cloudTemplate := &csm.SystemTemplate{ObjectMeta: metav1.ObjectMeta{Name: "ubuntu-24.04-cloudinit"}, Spec: csm.SystemTemplateSpec{
		Version: "24.04", Source: "official", Image: cloudImage, Hash: hex.EncodeToString(cloudDigest[:]), InitMode: "cloudInit",
	}}
	if err := c.Create(ctx, cloudTemplate); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
