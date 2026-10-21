package controllers

import (
	"bytes"
	"context"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSSHIdentityPersistsAndRejectsForeignSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	host := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "uid"}}
	ca, _, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build()
	r := &CsmReconciler{Client: c, Scheme: scheme, SSHCAPublicKey: ssh.MarshalAuthorizedKey(ca.PublicKey())}
	ctx := context.Background()
	if err := r.ensureSSHIdentity(ctx, host); err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Namespace: host.Namespace, Name: sshidentity.SecretName(host)}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), secret.Data["host-key"]...)
	if _, ok := secret.Data["private-key"]; ok {
		t.Fatal("CA private key exposed to host")
	}
	if err := r.ensureSSHIdentity(ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, secret.Data["host-key"]) {
		t.Fatal("host identity changed")
	}
	secret.OwnerReferences = nil
	if err := c.Update(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureSSHIdentity(ctx, host); err == nil {
		t.Fatal("foreign identity accepted")
	}
}
