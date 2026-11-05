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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/w7panel/w7panel-csm/pkg/order"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCouponGenerationRequiresFounderAndRejectsProvidedCode(t *testing.T) {
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "csm-alice", Founder: token == "founder"}, nil
	}, &backendStub{}, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	server.Coupons = &order.KubeCoupons{Kube: fake.NewSimpleClientset(), Namespace: "default"}
	call := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, Prefix+"/coupons", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	valid := `{"kind":"csm","action":"buy","discount":80,"count":2,"expiresAt":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	if w := call("tenant", valid); w.Code != 403 {
		t.Fatalf("tenant generated codes: %d", w.Code)
	}
	if w := call("founder", strings.TrimSuffix(valid, "}")+`,"code":"CUSTOM"}`); w.Code != 400 {
		t.Fatalf("client provided code accepted: %d %s", w.Code, w.Body.String())
	}
	if w := call("founder", valid); w.Code != 201 || !strings.Contains(w.Body.String(), `"codes"`) {
		t.Fatalf("generation: %d %s", w.Code, w.Body.String())
	}
}

type pricingStub struct{ saved *order.Prices }

func (p *pricingStub) Get(context.Context) (order.Prices, error) {
	if p.saved == nil {
		return order.Prices{}, order.ErrPricingMissing
	}
	return *p.saved, nil
}
func (p *pricingStub) Put(_ context.Context, v order.Prices) error { p.saved = &v; return nil }
func (p *pricingStub) Delete(context.Context) error                { p.saved = nil; return nil }
func (p *pricingStub) StorageClasses(context.Context) ([]string, error) {
	return []string{"fast", "slow"}, nil
}
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
	if w := call("GET", Prefix+"/storage-classes", "tenant", ""); w.Code != 403 {
		t.Fatalf("tenant listed storage classes: %d", w.Code)
	}
	if w := call("GET", Prefix+"/storage-classes", "founder", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "fast") {
		t.Fatalf("storage classes: %d %s", w.Code, w.Body.String())
	}
	classBody := strings.TrimSuffix(body, "}") + `,"storageClassName":"fast"}`
	if w := call("PUT", Prefix+"/pricing", "founder", classBody); w.Code != 200 || pricing.saved.StorageClassName != "fast" {
		t.Fatalf("storage class pricing update: %d %s", w.Code, w.Body.String())
	}
	combination := `{"quantity":1,"unit":"month","cpu":2,"memoryGi":4,"storageGi":20,"bandwidthMbps":5,"discountNew":80,"discountRenew":90}`
	if w := call("PUT", Prefix+"/pricing", "founder", strings.TrimSuffix(body, "}")+`,"combinations":[`+combination+`]}`); w.Code != 200 {
		t.Fatalf("combination pricing update: %d %s", w.Code, w.Body.String())
	}
	if w := call("PUT", Prefix+"/pricing", "founder", body); w.Code != 200 || len(pricing.saved.Combinations) != 1 || pricing.saved.StorageClassName != "fast" {
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

func TestUnknownAPIRouteDoesNotReturnUI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := &Server{UIRoot: root}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, Prefix+"/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing API returned %d: %s", w.Code, w.Body.String())
	}
}

func TestOrderTargetAndFreeRequireFounder(t *testing.T) {
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			w.Write([]byte(`{"items":[{"metadata":{"name":"csm-bob"}},{"metadata":{"name":"default"}}]}`))
			return
		}
		if r.URL.Path == "/api/v1/namespaces/csm-bob" {
			w.Write([]byte(`{"metadata":{"name":"csm-bob"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer kube.Close()
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "csm-alice", Founder: token == "founder"}, nil
	}, &backendStub{}, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	server.Pricing = &pricingStub{saved: &order.Prices{CPUCents: 100}}
	server.KubeURL, server.KubeToken = kube.URL, "test"
	call := func(path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		path, token, body string
		want              int
	}{
		{Prefix + "/orders/quote?namespace=csm-bob", "tenant", `{"kind":"csm","action":"buy","name":"host","cpu":2}`, 403},
		{Prefix + "/orders/quote", "tenant", `{"kind":"csm","action":"buy","name":"host","cpu":2,"founderFree":true}`, 403},
		{Prefix + "/orders", "tenant", `{"kind":"csm","action":"buy","name":"host","founderFree":true}`, 403},
		{Prefix + "/orders/quote?namespace=csm-bob", "founder", `{"kind":"csm","action":"buy","name":"host","cpu":2}`, 200},
		{Prefix + "/orders/quote?namespace=csm-missing", "founder", `{"kind":"csm","action":"buy","name":"host","cpu":2}`, 404},
	} {
		if w := call(tc.path, tc.token, tc.body); w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.token, tc.path, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, Prefix+"/accounts", nil)
	req.Header.Set("Authorization", "Bearer founder")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "csm-bob") || strings.Contains(w.Body.String(), "default") {
		t.Fatalf("accounts: %d %s", w.Code, w.Body.String())
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
	missing   bool
}

func TestHostDeleteUsesAuthorizedNamespace(t *testing.T) {
	backend := &backendStub{}
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "csm-alice", Founder: token == "founder"}, nil
	}, backend, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, Prefix+"/csms/host?namespace=csm-bob", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	if w := call("tenant"); w.Code != 200 || backend.namespace != "csm-alice" {
		t.Fatalf("tenant delete namespace: %d %s", w.Code, backend.namespace)
	}
	if w := call("founder"); w.Code != 200 || backend.namespace != "csm-bob" {
		t.Fatalf("founder delete namespace: %d %s", w.Code, backend.namespace)
	}
}

func (b *backendStub) List(_ context.Context, kind, ns string) (json.RawMessage, error) {
	b.namespace = ns
	return json.RawMessage(`{"items":[]}`), nil
}
func (b *backendStub) Get(_ context.Context, kind, ns, name string) (json.RawMessage, error) {
	b.namespace = ns
	if b.missing {
		if kind == "system-templates" {
			return json.RawMessage(`{"spec":{"initMode":"cloudInit"}}`), nil
		}
		return nil, ErrNotFound
	}
	return json.RawMessage(`{"metadata":{"name":"host"}}`), nil
}
func (b *backendStub) Action(_ context.Context, kind, ns, name, action string, _ json.RawMessage) (json.RawMessage, error) {
	b.namespace = ns
	b.actions++
	return json.RawMessage(`{}`), nil
}

type gatewayStub struct{}

type purchaseGateway struct {
	gatewayStub
	last order.Request
}

func (g *purchaseGateway) Create(_ context.Context, r order.Request) (order.Payment, error) {
	g.last = r
	return order.Payment{OrderSn: "SN", NeedPay: true, AmountCents: 100}, nil
}

func TestPurchaseNameGeneratedByServer(t *testing.T) {
	gateway := &purchaseGateway{}
	server := NewServer(func(context.Context, string) (Identity, error) {
		return Identity{Namespace: "tenant-a"}, nil
	}, &backendStub{missing: true}, order.Service{Gateway: gateway, Store: storeStub{}}, "")
	server.Pricing = &pricingStub{saved: &order.Prices{StorageClassName: "fast"}}
	for _, tc := range []struct{ kind, body string }{
		{"csm", `{"kind":"csm","action":"buy","name":"client-name","storageClassName":"slow","templateName":"ubuntu","cpu":2,"memoryGi":4,"storageGi":40,"bandwidthMbps":5,"quantity":1,"unit":"month"}`},
		{"dataDisk", `{"kind":"dataDisk","action":"buy","storageGi":10,"quantity":1,"unit":"month"}`},
	} {
		req := httptest.NewRequest(http.MethodPost, Prefix+"/orders", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer tenant")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		var result order.Record
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !regexp.MustCompile(`^csm-\d{5}$`).MatchString(result.Name) || result.Kind != tc.kind || result.StorageClassName != "fast" || gateway.last.StorageClassName != "fast" {
			t.Fatalf("%s purchase: %d %s", tc.kind, w.Code, w.Body.String())
		}
	}
}

func TestHostRuntimeUsesTenantNamespaceAndShowsDeploymentPods(t *testing.T) {
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kube-token" {
			t.Errorf("Kubernetes token missing")
		}
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/tenant-a/deployments/host":
			w.Write([]byte(`{"metadata":{"name":"host"},"spec":{"replicas":1},"status":{"readyReplicas":1,"updatedReplicas":1,"availableReplicas":1}}`))
		case "/api/v1/namespaces/tenant-a/pods":
			if r.URL.Query().Get("labelSelector") != "csm.w7.cc/host=host" {
				t.Errorf("unexpected pod selector: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"items":[{"metadata":{"name":"host-pod"},"spec":{"nodeName":"node-a","containers":[{"name":"host"}]},"status":{"phase":"Running","podIP":"10.0.0.1","containerStatuses":[{"name":"host","ready":true,"restartCount":2}]}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer kube.Close()
	backend := &backendStub{}
	server := NewServer(func(context.Context, string) (Identity, error) {
		return Identity{Namespace: "tenant-a"}, nil
	}, backend, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	server.KubeURL, server.KubeToken, server.KubeClient = kube.URL, "kube-token", kube.Client()
	req := httptest.NewRequest(http.MethodGet, Prefix+"/csms/host/runtime?namespace=other", nil)
	req.Header.Set("Authorization", "Bearer tenant")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	var result struct {
		Deployment hostDeploymentInfo `json:"deployment"`
		Pods       []hostPodInfo      `json:"pods"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || backend.namespace != "tenant-a" || result.Deployment.Name != "host" || result.Deployment.Ready != 1 || len(result.Pods) != 1 || result.Pods[0].Name != "host-pod" || result.Pods[0].Ready != 1 || result.Pods[0].Restarts != 2 {
		t.Fatalf("runtime: %d %s", w.Code, w.Body.String())
	}
	backend.missing = true
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("missing host runtime exposed: %d %s", w.Code, w.Body.String())
	}
}

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
func TestFounderListsAllHostsButTenantStaysNamespaced(t *testing.T) {
	backend := &backendStub{}
	server := NewServer(func(_ context.Context, token string) (Identity, error) {
		return Identity{Namespace: "csm-alice", Founder: token == "founder"}, nil
	}, backend, order.Service{Gateway: gatewayStub{}, Store: storeStub{}}, "")
	for _, tc := range []struct{ token, path, namespace string }{
		{"tenant", Prefix + "/csms?namespace=csm-bob", "csm-alice"},
		{"founder", Prefix + "/csms", ""},
		{"founder", Prefix + "/csms?namespace=csm-bob", "csm-bob"},
		{"tenant", Prefix + "/csms/host?namespace=csm-bob", "csm-alice"},
		{"founder", Prefix + "/csms/host?namespace=csm-bob", "csm-bob"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization-csm", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK || backend.namespace != tc.namespace {
			t.Fatalf("%s %s: code=%d namespace=%q", tc.token, tc.path, w.Code, backend.namespace)
		}
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
