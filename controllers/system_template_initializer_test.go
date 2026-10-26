package controllers

import (
	"context"
	"testing"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOfficialTemplateUsesConfiguredSystemImage(t *testing.T) {
	t.Setenv("CSM_SYSTEM_IMAGE", "registry.example/csm-system:24.04")
	t.Setenv("CSM_CLOUD_INIT_SYSTEM_IMAGE", "registry.example/csm-system:24.04-cloudinit")
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	if err := EnsureOfficialSystemTemplate(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	template := &csm.SystemTemplate{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "ubuntu-24.04"}, template); err != nil {
		t.Fatal(err)
	}
	if template.Spec.Image != "registry.example/csm-system:24.04" || template.Spec.Hash == "" {
		t.Fatalf("wrong system image: %+v", template.Spec)
	}
	cloud := &csm.SystemTemplate{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "ubuntu-24.04-cloudinit"}, cloud); err != nil {
		t.Fatal(err)
	}
	if cloud.Spec.InitMode != "cloudInit" || cloud.Spec.Image != "registry.example/csm-system:24.04-cloudinit" || cloud.Spec.Hash == "" {
		t.Fatalf("wrong cloud-init template: %+v", cloud.Spec)
	}
	if err := EnsureOfficialSystemTemplate(context.Background(), c); err != nil {
		t.Fatalf("template initialization must be repeatable: %v", err)
	}
}

func TestOfficialCloudInitTemplateWaitsForConfiguredImage(t *testing.T) {
	t.Setenv("CSM_CLOUD_INIT_SYSTEM_IMAGE", "")
	scheme := runtime.NewScheme()
	if err := csm.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	if err := EnsureOfficialSystemTemplate(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	cloud := &csm.SystemTemplate{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "ubuntu-24.04-cloudinit"}, cloud); !apierrors.IsNotFound(err) {
		t.Fatalf("template appeared without image: %v", err)
	}
}
