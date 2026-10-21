package sshjump

import (
	"context"
	"testing"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDestinationAuthorization(t *testing.T) {
	signer, _, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	key := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	base := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "tenant", UID: "uid"}, Spec: api.CsmSpec{Paid: true, JumpPublicKeys: []string{key}}, Status: api.CsmStatus{Phase: "Ready"}}
	user := "root+" + api.SSHDomain(base.Namespace, base.Name, "example.test")
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	for _, tc := range []struct {
		name, user, key string
		change          func(*api.Csm)
		allowed         bool
	}{
		{name: "valid", allowed: true},
		{name: "wrong key", key: "wrong"},
		{name: "legacy jump", user: "jump"},
		{name: "other host", user: "root+other.example.test"},
		{name: "other account", user: "admin+" + user[5:]},
		{name: "unpaid", change: func(h *api.Csm) { h.Spec.Paid = false }},
		{name: "not ready", change: func(h *api.Csm) { h.Status.Phase = "Provisioning" }},
		{name: "expired despite ready", change: func(h *api.Csm) { h.Spec.ExpireTime = time.Now().Add(-time.Minute).Format(time.RFC3339) }},
		{name: "invalid expiry", change: func(h *api.Csm) { h.Spec.ExpireTime = "invalid" }},
		{name: "legacy expiry", change: func(h *api.Csm) { h.Spec.ExpireTime = time.Now().Add(time.Hour).Format(time.DateTime) }, allowed: true},
		{name: "deleting", change: func(h *api.Csm) {
			now := metav1.Now()
			h.DeletionTimestamp = &now
			h.Finalizers = []string{api.CsmFinalizer}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := base.DeepCopyObject().(*api.Csm)
			if tc.change != nil {
				tc.change(host)
			}
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			server := &Server{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build(), Suffix: "example.test"}
			login, fp := user, fingerprint
			if tc.user != "" {
				login = tc.user
			}
			if tc.key != "" {
				fp = tc.key
			}
			_, err := server.destination(context.Background(), login, fp)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
		})
	}
}

func TestForwardingIsLimitedToTargetLoopback(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if _, ok := forwardingPayload(ssh.Marshal(target{Host: host, Port: 12345})); !ok {
			t.Fatalf("rejected %s", host)
		}
	}
	for _, host := range []string{"10.0.0.1", "example.com", "127.0.0.2", "localhost.evil", "::ffff:127.0.0.1"} {
		if _, ok := forwardingPayload(ssh.Marshal(target{Host: host, Port: 22})); ok {
			t.Fatalf("accepted %s", host)
		}
	}
	for _, port := range []uint32{0, 65536} {
		if _, ok := forwardingPayload(ssh.Marshal(target{Host: "localhost", Port: port})); ok {
			t.Fatal("invalid port")
		}
	}
	if _, ok := forwardingPayload([]byte("bad")); ok {
		t.Fatal("malformed request")
	}
	for _, kind := range []string{"auth-agent-req@openssh.com", "x11-req", "env"} {
		if sessionRequestAllowed(kind) {
			t.Fatalf("accepted %s", kind)
		}
	}
}
