package sshjump

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const hostKeySecret = "csm-ssh-jump-hostkey"
const maxConnectionsPerIP = 32
const maxConnections = 512
const maxChannels = 32

type Server struct {
	Client      client.Reader
	Suffix      string
	Signer      ssh.Signer
	CA          ssh.Signer
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func LoadHostKey(ctx context.Context, kube kubernetes.Interface, namespace string) (ssh.Signer, error) {
	secrets := kube.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, hostKeySecret, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, key, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, genErr
		}
		secret, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: hostKeySecret}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"ed25519": key}}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, hostKeySecret, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	key := secret.Data["ed25519"]
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid SSH jump host key")
	}
	return ssh.NewSignerFromKey(ed25519.PrivateKey(key))
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	if s.Client == nil || s.Signer == nil || s.CA == nil || s.Suffix == "" {
		return fmt.Errorf("CSM SSH gateway is not configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	config := &ssh.ServerConfig{PublicKeyCallback: s.authenticate, MaxAuthTries: 3}
	config.AddHostKey(s.Signer)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var mu sync.Mutex
	counts := map[string]int{}
	total := 0
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		ip, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		if err != nil {
			_ = conn.Close()
			continue
		}
		mu.Lock()
		if counts[ip] >= maxConnectionsPerIP || total >= maxConnections {
			mu.Unlock()
			_ = conn.Close()
			slog.Warn("CSM SSH connection limit", "source", ip)
			continue
		}
		counts[ip]++
		total++
		mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				mu.Lock()
				counts[ip]--
				total--
				if counts[ip] == 0 {
					delete(counts, ip)
				}
				mu.Unlock()
			}()
			s.handle(ctx, conn, config)
		}()
	}
}

func matchesKey(keys []string, fingerprint string) bool {
	for _, line := range keys {
		key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
		if err == nil && len(bytes.TrimSpace(rest)) == 0 && ssh.FingerprintSHA256(key) == fingerprint {
			return true
		}
	}
	return false
}

func (s *Server) destination(ctx context.Context, username, fingerprint string) (*api.Csm, error) {
	if !strings.HasPrefix(username, "root+") || len(username) > 258 {
		return nil, errors.New("CSM SSH access denied")
	}
	hosts := &api.CsmList{}
	if err := s.Client.List(ctx, hosts); err != nil {
		return nil, err
	}
	for i := range hosts.Items {
		host := &hosts.Items[i]
		if !strings.EqualFold(strings.TrimPrefix(username, "root+"), api.SSHDomain(host.Namespace, host.Name, s.Suffix)) {
			continue
		}
		if host.UID == "" || host.Status.Phase != "Ready" || !host.Spec.Paid || !host.DeletionTimestamp.IsZero() || !matchesKey(host.Spec.JumpPublicKeys, fingerprint) {
			break
		}
		if host.Spec.ExpireTime != "" {
			expiry, err := time.Parse(time.RFC3339, host.Spec.ExpireTime)
			if err != nil {
				expiry, err = time.ParseInLocation(time.DateTime, host.Spec.ExpireTime, time.Local)
			}
			if err != nil || !time.Now().Before(expiry) {
				break
			}
		}
		return host, nil
	}
	return nil, errors.New("CSM SSH access denied")
}

func (s *Server) authenticate(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fingerprint := ssh.FingerprintSHA256(key)
	host, err := s.destination(ctx, meta.User(), fingerprint)
	if err != nil {
		slog.Warn("CSM SSH authentication denied", "source", meta.RemoteAddr().String(), "fingerprint", fingerprint)
		return nil, errors.New("CSM SSH access denied")
	}
	return &ssh.Permissions{Extensions: map[string]string{"key": fingerprint, "uid": string(host.UID)}}, nil
}

type target struct {
	Host       string
	Port       uint32
	Origin     string
	OriginPort uint32
}

func forwardingPayload(payload []byte) ([]byte, bool) {
	var request target
	if ssh.Unmarshal(payload, &request) != nil || request.Port == 0 || request.Port > 65535 {
		return nil, false
	}
	switch request.Host {
	case "localhost", "127.0.0.1":
		request.Host = "127.0.0.1"
	case "::1":
	default:
		return nil, false
	}
	return ssh.Marshal(request), true
}

func (s *Server) backend(ctx context.Context, host *api.Csm, fingerprint string) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	secret := &corev1.Secret{}
	if err := s.Client.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: sshidentity.SecretName(host)}, secret); err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(secret, host) {
		return nil, errors.New("CSM SSH identity owner mismatch")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(secret.Data["host-key.pub"])
	if err != nil {
		return nil, err
	}
	signer, err := sshidentity.Certificate(s.CA, host, fingerprint, time.Now())
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort(host.Name+"."+host.Namespace+".svc.cluster.local", "22")
	dial := s.dialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	upstream, channels, requests, err := ssh.NewClientConn(conn, address, &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.FixedHostKey(key)})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(upstream, channels, requests), nil
}

func (s *Server) handle(ctx context.Context, conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	_ = conn.SetDeadline(time.Time{})
	fingerprint := server.Permissions.Extensions["key"]
	uid := server.Permissions.Extensions["uid"]
	start := time.Now()
	slog.Info("CSM SSH connected", "source", conn.RemoteAddr().String(), "hostUID", uid, "fingerprint", fingerprint)
	defer func() {
		slog.Info("CSM SSH disconnected", "hostUID", uid, "fingerprint", fingerprint, "duration", time.Since(start).String())
	}()
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = server.Wait(); cancel() }()
	go func() {
		for request := range requests {
			// OpenSSH keepalives are safe; remote listeners and other global requests are not.
			_ = request.Reply(request.Type == "keepalive@openssh.com", nil)
		}
	}()
	authCtx, authCancel := context.WithTimeout(connectionCtx, 5*time.Second)
	host, err := s.destination(authCtx, server.User(), fingerprint)
	authCancel()
	if err != nil || string(host.UID) != uid {
		return
	}
	upstream, err := s.backend(connectionCtx, host, fingerprint)
	if err != nil {
		slog.Warn("CSM SSH backend unavailable", "hostUID", uid, "error", err)
		return
	}
	defer upstream.Close()
	backendStop := context.AfterFunc(connectionCtx, func() { _ = upstream.Close() })
	defer backendStop()
	go func() { _ = upstream.Wait(); _ = server.Close() }()
	slots := make(chan struct{}, maxChannels)
	for channel := range channels {
		payload := channel.ExtraData()
		switch channel.ChannelType() {
		case "session":
			if len(payload) != 0 {
				_ = channel.Reject(ssh.Prohibited, "invalid session")
				continue
			}
		case "direct-tcpip":
			var ok bool
			payload, ok = forwardingPayload(payload)
			if !ok {
				_ = channel.Reject(ssh.Prohibited, "only target loopback forwarding is allowed")
				continue
			}
		default:
			_ = channel.Reject(ssh.Prohibited, "unsupported channel")
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = channel.Reject(ssh.ResourceShortage, "CSM SSH channel limit")
			continue
		}
		go func() {
			defer func() { <-slots }()
			authCtx, cancel := context.WithTimeout(connectionCtx, 5*time.Second)
			current, err := s.destination(authCtx, server.User(), fingerprint)
			cancel()
			if err != nil || string(current.UID) != uid {
				_ = channel.Reject(ssh.Prohibited, "CSM SSH access denied")
				return
			}
			// OpenChannel has no context API; bound a stalled backend by closing its connection.
			timer := time.AfterFunc(10*time.Second, func() { _ = upstream.Close() })
			remote, remoteRequests, err := upstream.OpenChannel(channel.ChannelType(), payload)
			timer.Stop()
			if err != nil {
				_ = channel.Reject(ssh.ConnectionFailed, "CSM SSH channel unavailable")
				return
			}
			local, localRequests, err := channel.Accept()
			if err != nil {
				_ = remote.Close()
				return
			}
			relay(local, localRequests, remote, remoteRequests)
		}()
	}
}

func sessionRequestAllowed(kind string) bool {
	switch kind {
	case "pty-req", "window-change", "shell", "exec", "subsystem", "signal", "break":
		return true
	// Do not pass arbitrary environment variables into privileged backend processes.
	default:
		return false
	}
}

func relay(local ssh.Channel, localRequests <-chan *ssh.Request, remote ssh.Channel, remoteRequests <-chan *ssh.Request) {
	defer local.Close()
	defer remote.Close()
	outputDone := make(chan struct{})
	go func() {
		var output sync.WaitGroup
		output.Add(2)
		go func() { defer output.Done(); _, _ = io.Copy(local, remote) }()
		go func() { defer output.Done(); _, _ = io.Copy(local.Stderr(), remote.Stderr()) }()
		output.Wait()
		_ = local.CloseWrite()
		close(outputDone)
	}()
	go func() { _, _ = io.Copy(remote, local); _ = remote.CloseWrite() }()
	go func() {
		for request := range localRequests {
			ok := false
			if sessionRequestAllowed(request.Type) {
				ok, _ = remote.SendRequest(request.Type, request.WantReply, request.Payload)
			}
			if request.WantReply {
				_ = request.Reply(ok, nil)
			}
		}
		_ = remote.Close()
	}()
	for request := range remoteRequests {
		ok, _ := local.SendRequest(request.Type, request.WantReply, request.Payload)
		if request.WantReply {
			_ = request.Reply(ok, nil)
		}
	}
	<-outputDone
}
