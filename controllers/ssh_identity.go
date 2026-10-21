package controllers

import (
	"bytes"
	"context"
	"fmt"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *CsmReconciler) ensureSSHIdentity(ctx context.Context, host *api.Csm) error {
	if host.UID == "" || len(r.SSHCAPublicKey) == 0 {
		return fmt.Errorf("CSM SSH CA or host UID is missing")
	}
	key := types.NamespacedName{Namespace: host.Namespace, Name: sshidentity.SecretName(host)}
	secret := &corev1.Secret{}
	err := r.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		signer, private, err := sshidentity.NewKey()
		if err != nil {
			return err
		}
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}, Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"host-key": private, "host-key.pub": ssh.MarshalAuthorizedKey(signer.PublicKey()), "user-ca.pub": r.SSHCAPublicKey, "principals": []byte(sshidentity.Principal(host) + "\n")}}
		if err := r.setOwner(host, secret); err != nil {
			return err
		}
		return r.Create(ctx, secret)
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(secret, host) {
		return fmt.Errorf("CSM SSH Secret is not owned by host")
	}
	signer, err := ssh.ParsePrivateKey(secret.Data["host-key"])
	if err != nil {
		return fmt.Errorf("invalid CSM SSH host key: %w", err)
	}
	if !bytes.Equal(secret.Data["host-key.pub"], ssh.MarshalAuthorizedKey(signer.PublicKey())) {
		return fmt.Errorf("CSM SSH host public key mismatch")
	}
	principal := []byte(sshidentity.Principal(host) + "\n")
	if bytes.Equal(secret.Data["user-ca.pub"], r.SSHCAPublicKey) && bytes.Equal(secret.Data["principals"], principal) {
		return nil
	}
	secret.Data["user-ca.pub"] = r.SSHCAPublicKey
	secret.Data["principals"] = principal
	return r.Update(ctx, secret)
}
