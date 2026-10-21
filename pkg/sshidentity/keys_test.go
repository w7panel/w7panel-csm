package sshidentity

import (
	"bytes"
	"context"
	"testing"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCertificateScopeAndLifetime(t *testing.T) {
	ca, _, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	host := &api.Csm{ObjectMeta: metav1.ObjectMeta{UID: "host-a"}}
	now := time.Now()
	signer, err := Certificate(ca, host, "SHA256:user", now)
	if err != nil {
		t.Fatal(err)
	}
	cert := signer.PublicKey().(*ssh.Certificate)
	checker := &ssh.CertChecker{Clock: func() time.Time { return now }}
	if err := checker.CheckCert(Principal(host), cert); err != nil {
		t.Fatal(err)
	}
	if err := checker.CheckCert("csm:host-b", cert); err == nil {
		t.Fatal("certificate accepted for another host")
	}
	checker.Clock = func() time.Time { return now.Add(6 * time.Minute) }
	if err := checker.CheckCert(Principal(host), cert); err == nil {
		t.Fatal("expired certificate accepted")
	}
	if _, ok := cert.Extensions["permit-agent-forwarding"]; ok {
		t.Fatal("agent forwarding permitted")
	}
	if cert.ValidBefore-cert.ValidAfter != 330 {
		t.Fatal("unexpected certificate lifetime")
	}
}

func TestCAPersists(t *testing.T) {
	kube := fake.NewClientset()
	first, err := LoadCA(context.Background(), kube, "csm")
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadCA(context.Background(), kube, "csm")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PublicKey().Marshal(), second.PublicKey().Marshal()) {
		t.Fatal("CA changed")
	}
}
