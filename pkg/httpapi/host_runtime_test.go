package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/w7panel/w7panel-csm/pkg/order"
)

func TestHostPodLogsAndEventsAreScopedToHost(t *testing.T) {
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/csm-alice/") {
			t.Errorf("wrong namespace: %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/v1/namespaces/csm-alice/pods/owned":
			w.Write([]byte(`{"metadata":{"name":"owned","uid":"uid-1","labels":{"csm.w7.cc/host":"host"}}}`))
		case "/api/v1/namespaces/csm-alice/pods/other":
			w.Write([]byte(`{"metadata":{"name":"other","labels":{"csm.w7.cc/host":"someone-else"}}}`))
		case "/api/v1/namespaces/csm-alice/pods/owned/log":
			if r.URL.Query().Get("tailLines") != "200" {
				t.Errorf("unexpected log query: %s", r.URL.RawQuery)
			}
			w.Write([]byte("container output"))
		case "/api/v1/namespaces/csm-alice/events":
			w.Write([]byte(`{"items":[{"metadata":{"name":"ok"},"involvedObject":{"kind":"Pod","name":"owned","uid":"uid-1"},"message":"started"},{"metadata":{"name":"stale"},"involvedObject":{"kind":"Pod","name":"owned","uid":"uid-old"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer kube.Close()
	server := NewServer(func(context.Context, string) (Identity, error) { return Identity{Namespace: "csm-alice"}, nil }, &backendStub{}, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	server.KubeURL, server.KubeToken, server.KubeClient = kube.URL, "token", kube.Client()
	call := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, Prefix+path, nil)
		r.Header.Set("Authorization", "Bearer tenant")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("/csms/host/pods/owned/logs?namespace=other"); w.Code != 200 || !strings.Contains(w.Body.String(), "container output") {
		t.Fatalf("logs: %d %s", w.Code, w.Body.String())
	}
	if w := call("/csms/host/pods/owned/events"); w.Code != 200 || !strings.Contains(w.Body.String(), "started") || strings.Contains(w.Body.String(), "stale") {
		t.Fatalf("events: %d %s", w.Code, w.Body.String())
	}
	if w := call("/csms/host/pods/other/logs"); w.Code != 404 {
		t.Fatalf("other host pod exposed: %d %s", w.Code, w.Body.String())
	}
}
