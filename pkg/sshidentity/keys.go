// Package sshidentity manages CSM SSH identities shared by the controller and gateway.
package sshidentity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const CASecret = "csm-ssh-user-ca"

func SecretName(host *api.Csm) string { return "csm-ssh-" + string(host.UID) }
func Principal(host *api.Csm) string  { return "csm:" + string(host.UID) }

func NewKey() (ssh.Signer, []byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, "CSM SSH")
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(private)
	return signer, pem.EncodeToMemory(block), err
}

func LoadCA(ctx context.Context, kube kubernetes.Interface, namespace string) (ssh.Signer, error) {
	secrets := kube.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, CASecret, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, private, genErr := NewKey()
		if genErr != nil {
			return nil, genErr
		}
		secret, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: CASecret}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"private-key": private}}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, CASecret, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(secret.Data["private-key"])
}

func Certificate(ca ssh.Signer, host *api.Csm, fingerprint string, now time.Time) (ssh.Signer, error) {
	if host.UID == "" || ca == nil {
		return nil, fmt.Errorf("CSM SSH identity is missing")
	}
	signer, _, err := NewKey()
	if err != nil {
		return nil, err
	}
	cert := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.UserCert, KeyId: string(host.UID) + ":" + fingerprint,
		ValidPrincipals: []string{Principal(host)}, ValidAfter: uint64(now.Add(-30 * time.Second).Unix()), ValidBefore: uint64(now.Add(5 * time.Minute).Unix()),
		Permissions: ssh.Permissions{Extensions: map[string]string{"permit-pty": "", "permit-port-forwarding": ""}},
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		return nil, err
	}
	return ssh.NewCertSigner(cert, signer)
}
