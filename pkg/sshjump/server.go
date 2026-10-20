package sshjump

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const hostKeySecret = "csm-ssh-jump-hostkey"

type Server struct {
	Client client.Client
	Suffix string
	Signer ssh.Signer
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
	if s.Client == nil || s.Signer == nil || s.Suffix == "" {
		return fmt.Errorf("SSH jump is not configured")
	}
	config := &ssh.ServerConfig{PublicKeyCallback: s.authenticate}
	config.AddHostKey(s.Signer)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() { <-ctx.Done(); _ = listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handle(conn, config)
	}
}

func (s *Server) hosts(ctx context.Context) ([]api.Csm, error) {
	list := &api.CsmList{}
	if err := s.Client.List(ctx, list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func matchesKey(keys []string, fingerprint string) bool {
	for _, line := range keys {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err == nil && ssh.FingerprintSHA256(key) == fingerprint {
			return true
		}
	}
	return false
}

func (s *Server) authenticate(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if meta.User() != "jump" {
		return nil, errors.New("invalid jump user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hosts, err := s.hosts(ctx)
	if err != nil {
		return nil, err
	}
	fingerprint := ssh.FingerprintSHA256(key)
	for _, host := range hosts {
		if host.Status.Phase == "Ready" && matchesKey(host.Spec.JumpPublicKeys, fingerprint) {
			return &ssh.Permissions{Extensions: map[string]string{"key": fingerprint}}, nil
		}
	}
	return nil, errors.New("jump key is not authorized")
}

type target struct {
	Host       string
	Port       uint32
	Origin     string
	OriginPort uint32
}

func (s *Server) destination(hostname, fingerprint string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hosts, err := s.hosts(ctx)
	if err != nil {
		return "", err
	}
	for _, host := range hosts {
		if host.Status.Phase == "Ready" && host.DeletionTimestamp.IsZero() &&
			strings.EqualFold(hostname, api.SSHDomain(host.Namespace, host.Name, s.Suffix)) &&
			matchesKey(host.Spec.JumpPublicKeys, fingerprint) {
			return net.JoinHostPort(host.Name+"."+host.Namespace+".svc.cluster.local", "22"), nil
		}
	}
	return "", errors.New("SSH destination not authorized")
}

func (s *Server) handle(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	server, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer server.Close()
	_ = conn.SetDeadline(time.Time{})
	go ssh.DiscardRequests(requests)
	for channel := range channels {
		if channel.ChannelType() != "direct-tcpip" {
			_ = channel.Reject(ssh.Prohibited, "only SSH forwarding is allowed")
			continue
		}
		var request target
		if err := ssh.Unmarshal(channel.ExtraData(), &request); err != nil || request.Port != 22 || len(request.Host) > 253 {
			_ = channel.Reject(ssh.Prohibited, "invalid SSH destination")
			continue
		}
		address, err := s.destination(request.Host, server.Permissions.Extensions["key"])
		if err != nil {
			_ = channel.Reject(ssh.Prohibited, "SSH destination not authorized")
			continue
		}
		upstream, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			_ = channel.Reject(ssh.ConnectionFailed, "SSH destination unavailable")
			continue
		}
		stream, reqs, err := channel.Accept()
		if err != nil {
			_ = upstream.Close()
			continue
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			defer stream.Close()
			defer upstream.Close()
			done := make(chan struct{})
			go func() {
				_, _ = io.Copy(upstream, stream)
				if tcp, ok := upstream.(*net.TCPConn); ok {
					_ = tcp.CloseWrite()
				}
				close(done)
			}()
			_, _ = io.Copy(stream, upstream)
			_ = stream.CloseWrite()
			<-done
		}()
	}
}
