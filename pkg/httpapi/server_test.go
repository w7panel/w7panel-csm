package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/w7panel/w7panel-csm/pkg/order"
)

type pricingStub struct{ saved *order.Prices }

func (p *pricingStub) Get(context.Context) (order.Prices, error) {
	if p.saved == nil {
		return order.Prices{}, order.ErrPricingMissing
	}
	return *p.saved, nil
}
func (p *pricingStub) Put(_ context.Context, v order.Prices) error { p.saved = &v; return nil }
func (p *pricingStub) Delete(context.Context) error                { p.saved = nil; return nil }
func (p *pricingStub) Quote(_ context.Context, r order.Request) (int64, string, url.Values, error) {
	if p.saved == nil {
		return 0, "", nil, order.ErrPricingMissing
	}
	return p.saved.CPUCents * r.CPU, "", nil, nil
}

func TestPricingFounderGuardAndQuote(t *testing.T) {
	pricing := &pricingStub{}
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "tenant-a", Founder: token == "founder"}, nil
	}, &backendStub{}, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	server.Pricing = pricing
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	if w := call("GET", Prefix+"/pricing", "tenant", ""); w.Code != 404 {
		t.Fatalf("missing pricing: %d", w.Code)
	}
	body := `{"cpuCents":100,"memoryGiCents":50,"storageGiCents":10,"bandwidthMbpsCents":20}`
	if w := call("PUT", Prefix+"/pricing", "tenant", body); w.Code != 403 {
		t.Fatalf("tenant changed pricing: %d", w.Code)
	}
	if w := call("PUT", Prefix+"/pricing", "founder", `{"cpuCents":100}`); w.Code != 400 {
		t.Fatalf("partial pricing accepted: %d", w.Code)
	}
	if w := call("PUT", Prefix+"/pricing", "founder", body); w.Code != 200 {
		t.Fatalf("founder pricing update: %d %s", w.Code, w.Body.String())
	}
	combination := `{"quantity":1,"unit":"month","cpu":2,"memoryGi":4,"storageGi":20,"bandwidthMbps":5,"discountNew":80,"discountRenew":90}`
	if w := call("PUT", Prefix+"/pricing", "founder", strings.TrimSuffix(body, "}")+`,"combinations":[`+combination+`]}`); w.Code != 200 {
		t.Fatalf("combination pricing update: %d %s", w.Code, w.Body.String())
	}
	if w := call("PUT", Prefix+"/pricing", "founder", body); w.Code != 200 || len(pricing.saved.Combinations) != 1 {
		t.Fatalf("legacy pricing update lost combinations: %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", Prefix+"/orders/quote", "tenant", `{"kind":"csm","action":"buy","name":"host","cpu":2}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"amountCents":200`) {
		t.Fatalf("quote: %d %s", w.Code, w.Body.String())
	}
	if w := call("DELETE", Prefix+"/pricing", "founder", ""); w.Code != 204 {
		t.Fatalf("reset: %d", w.Code)
	}
	if w := call("POST", Prefix+"/orders/quote", "tenant", `{}`); w.Code != 503 {
		t.Fatalf("quote after reset: %d", w.Code)
	}
}

func TestRedactInitializationConfigs(t *testing.T) {
	data, err := redactInitializationConfigs(json.RawMessage(`{"metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":"cfg"},"spec":{"shell":"echo secret-shell","cloudConfig":"#cloud-config\npassword: secret-cloud","order":2}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-shell") || strings.Contains(string(data), "secret-cloud") || !strings.Contains(string(data), `"hasShell":true`) || !strings.Contains(string(data), `"hasCloudConfig":true`) || !strings.Contains(string(data), `"resourceVersion":"1"`) {
		t.Fatalf("config redaction failed: %s", data)
	}
}

func TestUpdateInitializationConfigRequiresFounder(t *testing.T) {
	backend := &backendStub{}
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "tenant", Founder: token == "founder"}, nil
	}, backend, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	for _, tc := range []struct {
		token string
		want  int
	}{{"tenant", http.StatusForbidden}, {"founder", http.StatusOK}} {
		req := httptest.NewRequest(http.MethodPut, Prefix+"/preinstall-applications/config", strings.NewReader(`{"metadata":{"name":"config","resourceVersion":"1"},"spec":{"order":2,"cloudConfig":"#cloud-config"}}`))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s update: got %d, want %d", tc.token, w.Code, tc.want)
		}
	}
	if backend.actions != 1 {
		t.Fatalf("backend calls: %d", backend.actions)
	}
}

type backendStub struct {
	namespace string
	actions   int
}

func (b *backendStub) List(_ context.Context, kind, ns string) (json.RawMessage, error) {
	b.namespace = ns
	return json.RawMessage(`{"items":[]}`), nil
}
func (b *backendStub) Get(_ context.Context, kind, ns, name string) (json.RawMessage, error) {
	b.namespace = ns
	return json.RawMessage(`{"metadata":{"name":"host"}}`), nil
}
func (b *backendStub) Action(_ context.Context, kind, ns, name, action string, _ json.RawMessage) (json.RawMessage, error) {
	b.namespace = ns
	b.actions++
	return json.RawMessage(`{}`), nil
}

type gatewayStub struct{}

func (gatewayStub) Create(context.Context, order.Request) (order.Payment, error) {
	return order.Payment{OrderSn: "SN"}, nil
}
func (gatewayStub) Lookup(context.Context, order.Record) (order.PaymentStatus, error) {
	return order.PaymentStatus{}, errors.New("unconfigured mock")
}

type storeStub struct{}

func (storeStub) SavePending(context.Context, order.Record) error { return nil }
func (storeStub) Get(context.Context, string, string) (order.Record, error) {
	return order.Record{}, order.ErrNotFound
}
func (storeStub) List(context.Context, string) ([]order.Record, error) { return nil, nil }
func (storeStub) ApplyPaid(context.Context, order.Record) (order.Record, error) {
	return order.Record{}, nil
}
func TestTenantAndFounderGuards(t *testing.T) {
	backend := &backendStub{}
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		if token != "good" {
			return Identity{}, errors.New("bad")
		}
		return Identity{Namespace: "tenant-a", Subject: "alice"}, nil
	}, backend, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	for _, tc := range []struct {
		path   string
		method string
		code   int
		ns     string
	}{
		{"/csm-api/v1/csms?namespace=tenant-b", "GET", 200, "tenant-a"},
		{"/csm-api/v1/csms/host", "GET", 200, "tenant-a"},
		{"/csm-api/v1/system-templates", "POST", 403, ""},
		{"/csm-api/v1/system-templates/ubuntu-24.04", "PUT", 403, ""},
		{"/csm-api/v1/upgrade-policies/script", "PUT", 403, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		req.Header.Set("Authorization-csm", "Bearer good")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s %d: %s", tc.path, w.Code, w.Body.String())
		}
		if tc.ns != "" && backend.namespace != tc.ns {
			t.Fatalf("namespace %q", backend.namespace)
		}
	}
	req := httptest.NewRequest("POST", "/csm-api/v1/data-disks/disk/mount", strings.NewReader(`{"csmName":"host"}`))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 401 || backend.actions != 0 {
		t.Fatalf("unauthorized write: %d, %d", w.Code, backend.actions)
	}
}
func TestStaticUIAndHealth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("CSM UI"), 0600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, order.Service{}, dir)
	for path, want := range map[string]string{"/healthz": "", "/": "CSM UI", "/hosts": "CSM UI"} {
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s: %d %q", path, w.Code, w.Body.String())
		}
	}
}

type callbackStore struct{ namespace string }

func (s *callbackStore) SavePending(context.Context, order.Record) error { return nil }
func (s *callbackStore) Get(_ context.Context, namespace, _ string) (order.Record, error) {
	s.namespace = namespace
	return order.Record{}, order.ErrNotFound
}
func (s *callbackStore) List(context.Context, string) ([]order.Record, error) { return nil, nil }
func (s *callbackStore) ApplyPaid(context.Context, order.Record) (order.Record, error) {
	return order.Record{}, nil
}
func TestW7FormCallbackResolvesTenant(t *testing.T) {
	store := &callbackStore{}
	server := NewServer(nil, nil, order.Service{Gateway: gatewayStub{}, Store: store}, "")
	req := httptest.NewRequest(http.MethodPost, "/csm-api/v1/order/notify", strings.NewReader("k3kName=alice&orderSn=SN-1&namespace=tenant-b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 404 || store.namespace != "csm-alice" {
		t.Fatalf("callback %d namespace %q: %s", w.Code, store.namespace, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/csm-api/v1/order/notify", strings.NewReader("k3kName=../bad&orderSn=SN-1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("invalid account accepted: %d", w.Code)
	}
}
func TestClusterExecutionListIsTenantFiltered(t *testing.T) {
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"metadata":{"name":"a"},"spec":{"csmNamespace":"tenant-a"}},{"metadata":{"name":"b"},"spec":{"csmNamespace":"tenant-b"}}]}`))
	}))
	defer kube.Close()
	backend := KubeBackend{BaseURL: kube.URL, Token: "token", Client: kube.Client()}
	data, err := backend.List(t.Context(), "upgrade-executions", "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"name":"b"`) || !strings.Contains(string(data), `"name":"a"`) {
		t.Fatal(string(data))
	}
}
