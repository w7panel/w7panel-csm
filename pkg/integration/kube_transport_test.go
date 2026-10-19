package integration

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestServiceAccountTransportReadsRotatedToken(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	var seen []string
	transport := ServiceAccountTransport(file, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(http.NoBody), Header: make(http.Header)}, nil
	}))
	for _, token := range []string{"first", "second"} {
		if err := os.WriteFile(file, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, "https://kube.example", nil)
		req.Header.Set("Authorization", "Bearer stale")
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || seen[0] != "Bearer first" || seen[1] != "Bearer second" {
		t.Fatalf("rotated headers = %v", seen)
	}
}
