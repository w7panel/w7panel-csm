package sshjump

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	hostruntime "github.com/w7panel/w7panel-csm/pkg/runtime"
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Run with CSM_SSH_TEST_IMAGE set to a local CSM system image; no cluster is used.
func TestOpenSSHIntegration(t *testing.T) {
	image := os.Getenv("CSM_SSH_TEST_IMAGE")
	if image == "" {
		t.Skip("set CSM_SSH_TEST_IMAGE for isolated Docker/OpenSSH integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := func(t *testing.T, name string, args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", name, err, out)
		}
		return out
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ca, _, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	hostKey, hostPrivate, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	user, userPrivate, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	gateway, _, err := sshidentity.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	host := &api.Csm{ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "tenant", UID: "test-uid"}, Spec: api.CsmSpec{Paid: true, JumpPublicKeys: []string{string(ssh.MarshalAuthorizedKey(user.PublicKey()))}}, Status: api.CsmStatus{Phase: "Ready"}}
	write("host-key", hostPrivate)
	write("user-ca.pub", ssh.MarshalAuthorizedKey(ca.PublicKey()))
	write("principals", []byte(sshidentity.Principal(host)+"\n"))
	write("user", userPrivate)
	dep, err := hostruntime.Deployment(host, image, nil)
	if err != nil {
		t.Fatal(err)
	}
	hostruntime.ConfigureSSHGateway(dep, host)
	startup := strings.Replace(dep.Spec.Template.Spec.Containers[0].Args[0], "exec /sbin/init", "exec /usr/sbin/sshd -D -e -o MaxStartups=100", 1)
	// Secret volumes are root-owned and their mount directory may be 01777 (Sysbox).
	startup = "mkdir -p /etc/csm-ssh-owned; cp /etc/csm-ssh/* /etc/csm-ssh-owned/; chown root:root /etc/csm-ssh-owned/*; chmod 1777 /etc/csm-ssh-owned; " + strings.ReplaceAll(startup, "/etc/csm-ssh/", "/etc/csm-ssh-owned/")
	container := strings.TrimSpace(string(command(t, "docker", "run", "--pull=never", "--rm", "-d", "--network", "bridge", "-p", "127.0.0.1::2222", "-v", dir+":/etc/csm-ssh:ro", "--entrypoint", "/bin/sh", image, "-ec", startup)))
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command("docker", "logs", container).CombinedOutput()
			t.Logf("sshd logs: %s", logs)
		}
		out, err := exec.Command("docker", "rm", "-f", container).CombinedOutput()
		if err != nil {
			t.Logf("container cleanup: %v %s", err, out)
		}
	})
	address := strings.TrimSpace(string(command(t, "docker", "port", container, "2222/tcp")))
	for deadline := time.Now().Add(10 * time.Second); ; {
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			banner := make([]byte, 128)
			n, readErr := c.Read(banner)
			_ = c.Close()
			if readErr == nil && strings.HasPrefix(string(banner[:n]), "SSH-") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd not ready: %s", command(t, "docker", "logs", container))
		}
		time.Sleep(50 * time.Millisecond)
	}
	owner := true
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: sshidentity.SecretName(host), Namespace: host.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "Csm", Name: host.Name, UID: host.UID, Controller: &owner}}}, Data: map[string][]byte{"host-key.pub": ssh.MarshalAuthorizedKey(hostKey.PublicKey())}}
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	reader := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(host).WithObjects(host, secret).Build()
	server := &Server{Client: reader, Suffix: "example.test", Signer: gateway, CA: ca, dialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverConfig := &ssh.ServerConfig{PublicKeyCallback: server.authenticate}
	serverConfig.AddHostKey(gateway)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.handle(ctx, conn, serverConfig)
		}
	}()
	login := "root+" + api.SSHDomain(host.Namespace, host.Name, server.Suffix)
	dial := func() *ssh.Client {
		t.Helper()
		client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: login, Auth: []ssh.AuthMethod{ssh.PublicKeys(user)}, HostKeyCallback: ssh.FixedHostKey(gateway.PublicKey()), Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	client := dial()
	defer client.Close()
	t.Run("command stdout stderr and exit", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		var stdout, stderr bytes.Buffer
		session.Stdout = &stdout
		session.Stderr = &stderr
		err = session.Run("printf csm-output; printf csm-error >&2; exit 42")
		exit, ok := err.(*ssh.ExitError)
		if !ok || exit.ExitStatus() != 42 || stdout.String() != "csm-output" || stderr.String() != "csm-error" {
			t.Fatalf("exit=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}
	})
	t.Run("large output", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		out, err := session.Output("head -c 1048576 /dev/zero")
		if err != nil || len(out) != 1048576 {
			t.Fatalf("output=%d err=%v", len(out), err)
		}
	})
	t.Run("pty window change and shell", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		if err := session.WindowChange(40, 100); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		session.Stdout = &out
		session.Stdin = strings.NewReader("stty size\nexit\n")
		if err := session.Shell(); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "40 100") {
			t.Fatalf("window size missing: %s", out.String())
		}
	})
	t.Run("root signal rejection and terminal interrupt", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		output, err := session.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		input, err := session.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Start("echo ready; exec sleep 30"); err != nil {
			t.Fatal(err)
		}
		if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
			t.Fatalf("ready=%q error=%v", line, err)
		}
		// OpenSSH rejects protocol signals for root; preserve that response for clients.
		ok, err := session.SendRequest("signal", true, ssh.Marshal(struct{ Signal string }{"TERM"}))
		if err != nil || ok {
			t.Fatalf("root signal reply=%v error=%v", ok, err)
		}
		if _, err := input.Write([]byte{3}); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- session.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("Ctrl+C did not interrupt command")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Ctrl+C timed out")
		}
	})

	t.Run("loopback forwarding and remote listener denial", func(t *testing.T) {
		conn, err := client.Dial("tcp", "127.0.0.1:2222")
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		_ = conn.Close()
		if err != nil || !strings.HasPrefix(string(buf[:n]), "SSH-") {
			t.Fatalf("forwarded banner: %q %v", buf[:n], err)
		}
		if conn, err := client.Dial("tcp", "10.0.0.1:22"); err == nil {
			conn.Close()
			t.Fatal("external forward allowed")
		}
		if l, err := client.Listen("tcp", "127.0.0.1:12345"); err == nil {
			l.Close()
			t.Fatal("remote listener allowed")
		}
	})
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	write("known_hosts", []byte("[127.0.0.1]:"+port+" "+string(ssh.MarshalAuthorizedKey(gateway.PublicKey()))))
	options := []string{"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + filepath.Join(dir, "known_hosts"), "-o", "IdentitiesOnly=yes", "-i", filepath.Join(dir, "user")}
	write("upload", []byte("CSM file transfer\n"))
	t.Run("sftp upload and download", func(t *testing.T) {
		args := append(append([]string{}, options...), "-P", port, "-b", "-", login+"@127.0.0.1")
		cmd := exec.CommandContext(ctx, "sftp", args...)
		cmd.Stdin = strings.NewReader(fmt.Sprintf("put %s /tmp/csm-upload\nget /tmp/csm-upload %s\n", filepath.Join(dir, "upload"), filepath.Join(dir, "download")))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("sftp: %v %s", err, out)
		}
		got, err := os.ReadFile(filepath.Join(dir, "download"))
		if err != nil || string(got) != "CSM file transfer\n" {
			t.Fatalf("download %q %v", got, err)
		}
	})
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("scp legacy=%v", legacy), func(t *testing.T) {
			args := append(append([]string{}, options...), "-P", port)
			if legacy {
				args = append(args, "-O")
			}
			command(t, "scp", append(args, filepath.Join(dir, "upload"), login+"@127.0.0.1:/tmp/csm-scp")...)
			command(t, "scp", append(args, login+"@127.0.0.1:/tmp/csm-scp", filepath.Join(dir, "scp-download"))...)
			got, err := os.ReadFile(filepath.Join(dir, "scp-download"))
			if err != nil || string(got) != "CSM file transfer\n" {
				t.Fatalf("download %q %v", got, err)
			}
		})
	}
	t.Run("channel limit", func(t *testing.T) {
		var connections []net.Conn
		defer func() {
			for _, conn := range connections {
				_ = conn.Close()
			}
		}()
		for i := 0; i < maxChannels; i++ {
			conn, err := client.Dial("tcp", "127.0.0.1:2222")
			if err != nil {
				t.Fatalf("channel %d: %v", i, err)
			}
			connections = append(connections, conn)
		}
		if conn, err := client.Dial("tcp", "127.0.0.1:2222"); err == nil {
			conn.Close()
			t.Fatal("channel limit exceeded")
		}
	})
	t.Run("backend rejects expired or wrong principal certificates", func(t *testing.T) {
		for _, badHost := range []bool{false, true} {
			target := host.DeepCopyObject().(*api.Csm)
			now := time.Now()
			if badHost {
				target.UID = "another-host"
			} else {
				now = now.Add(-10 * time.Minute)
			}
			signer, err := sshidentity.Certificate(ca, target, "test", now)
			if err != nil {
				t.Fatal(err)
			}
			connection, err := ssh.Dial("tcp", address, &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()), Timeout: 5 * time.Second})
			if err == nil {
				connection.Close()
				t.Fatalf("bad certificate accepted: wrongPrincipal=%v", badHost)
			}
		}
	})

	t.Run("key revocation rejects new channels", func(t *testing.T) {
		key := types.NamespacedName{Namespace: host.Namespace, Name: host.Name}
		current := &api.Csm{}
		if err := reader.Get(ctx, key, current); err != nil {
			t.Fatal(err)
		}
		current.Spec.JumpPublicKeys = nil
		if err := reader.Update(ctx, current); err != nil {
			t.Fatal(err)
		}
		if sess, err := client.NewSession(); err == nil {
			sess.Close()
			t.Fatal("revoked key opened session")
		}
		current.Spec.JumpPublicKeys = host.Spec.JumpPublicKeys
		if err := reader.Update(ctx, current); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("host key mismatch", func(t *testing.T) {
		wrong, _, err := sshidentity.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		secret.Data["host-key.pub"] = ssh.MarshalAuthorizedKey(wrong.PublicKey())
		if err := reader.Update(ctx, secret); err != nil {
			t.Fatal(err)
		}
		if backend, err := server.backend(ctx, host, "fingerprint"); err == nil {
			backend.Close()
			t.Fatal("wrong host key accepted")
		}
	})
	t.Run("shutdown closes connection", func(t *testing.T) {
		cancel()
		done := make(chan error, 1)
		go func() { done <- client.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("connection leaked on shutdown")
		}
	})
}
