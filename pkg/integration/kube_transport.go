package integration

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

type serviceAccountTransport struct {
	file string
	next http.RoundTripper
}

// ServiceAccountTransport reads the current projected token for each API call.
func ServiceAccountTransport(tokenFile string, next http.RoundTripper) http.RoundTripper {
	return serviceAccountTransport{file: tokenFile, next: next}
}

func (t serviceAccountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := os.ReadFile(t.file)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes service account token: %w", err)
	}
	if strings.TrimSpace(string(token)) == "" {
		return nil, fmt.Errorf("Kubernetes service account token is empty")
	}
	copy := req.Clone(req.Context())
	copy.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	return t.next.RoundTrip(copy)
}
