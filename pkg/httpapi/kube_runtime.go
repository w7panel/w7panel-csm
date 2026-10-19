package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/w7panel/w7panel-csm/pkg/order"
)

const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// InClusterRuntime connects both HTTP resources and payment persistence to the
// same Kubernetes API using the Pod's service account and cluster CA.
func InClusterRuntime() (KubeBackend, order.KubeStore, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return KubeBackend{}, order.KubeStore{}, fmt.Errorf("Kubernetes service environment is missing")
	}
	token, err := os.ReadFile(serviceAccountDir + "/token")
	if err != nil {
		return KubeBackend{}, order.KubeStore{}, err
	}
	ca, err := os.ReadFile(serviceAccountDir + "/ca.crt")
	if err != nil {
		return KubeBackend{}, order.KubeStore{}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return KubeBackend{}, order.KubeStore{}, fmt.Errorf("invalid Kubernetes CA")
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	endpoint := "https://" + net.JoinHostPort(host, port)
	backend := KubeBackend{BaseURL: endpoint, Token: strings.TrimSpace(string(token)), Client: client}
	store := order.KubeStore{BaseURL: endpoint, Token: backend.Token, Client: client}
	return backend, store, nil
}
