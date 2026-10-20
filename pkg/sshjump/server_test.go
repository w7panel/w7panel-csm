package sshjump

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestJumpDestinationRequiresMatchingHostAndKey(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	host := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "csm-admin"}, Spec: api.CsmSpec{JumpPublicKeys: []string{key}}, Status: api.CsmStatus{Phase: "Ready"}}
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	s := &Server{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(host).WithObjects(host).Build(), Suffix: "fan.b2.sz.w7.com"}
	domain := api.SSHDomain(host.Namespace, host.Name, s.Suffix)
	if !strings.HasPrefix(domain, "test-csm-admin-") {
		t.Fatalf("domain: %s", domain)
	}
	address, err := s.destination(domain, ssh.FingerprintSHA256(signer.PublicKey()))
	if err != nil || address != "test.csm-admin.svc.cluster.local:22" {
		t.Fatalf("destination: %s, %v", address, err)
	}
	if _, err := s.destination(domain, "wrong key"); err == nil {
		t.Fatal("wrong key reached host")
	}
	if _, err := s.destination("other.fan.b2.sz.w7.com", ssh.FingerprintSHA256(signer.PublicKey())); err == nil {
		t.Fatal("unknown host reached service")
	}
	if err := s.Client.Get(context.Background(), types.NamespacedName{Namespace: host.Namespace, Name: host.Name}, host); err != nil {
		t.Fatal(err)
	}
	host.Status.Phase = "Expired"
	if err := s.Client.Status().Update(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if _, err := s.destination(domain, ssh.FingerprintSHA256(signer.PublicKey())); err == nil {
		t.Fatal("expired host remained reachable")
	}
}

func TestJumpRejectsShellAndUnknownDestination(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	host := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "tenant"}, Spec: api.CsmSpec{JumpPublicKeys: []string{key}}, Status: api.CsmStatus{Phase: "Ready"}}
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	s := &Server{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build(), Suffix: "fan.b2.sz.w7.com", Signer: signer}
	config := &ssh.ServerConfig{PublicKeyCallback: s.authenticate}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		serverConn, err := listener.Accept()
		if err == nil {
			s.handle(serverConn, config)
		}
	}()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, channels, requests, err := ssh.NewClientConn(clientConn, "jump", &ssh.ClientConfig{User: "jump", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	client := ssh.NewClient(conn, channels, requests)
	defer client.Close()
	if _, err := client.NewSession(); err == nil {
		t.Fatal("jump accepted a shell")
	}
	if _, err := client.Dial("tcp", "unknown.fan.b2.sz.w7.com:22"); err == nil {
		t.Fatal("jump forwarded unknown host")
	}
}
