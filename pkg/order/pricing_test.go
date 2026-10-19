package order

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestKubePricingQuoteAndReset(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/csm.w7.cc/v1alpha1/namespaces/csm-alice/csms/host" {
			t.Errorf("unexpected resource path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"spec": map[string]any{"paid": true, "cpu": "2", "memory": "4Gi", "systemDiskGi": 20, "bandwidthMbps": 5, "expireTime": now.Add(360 * time.Hour).Format(time.RFC3339)}})
	}))
	defer resource.Close()
	p := KubePricing{Kube: fake.NewSimpleClientset(), Store: KubeStore{BaseURL: resource.URL, Client: resource.Client()}, Namespace: "default", Now: func() time.Time { return now }}
	buy := Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu-24.04", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	if _, _, _, err := p.Quote(ctx, buy); err != ErrPricingMissing {
		t.Fatalf("unconfigured pricing: %v", err)
	}
	if err := p.Put(ctx, Prices{CPUCents: 100, MemoryGiCents: 50, StorageGiCents: 10, BandwidthMbpsCents: 20}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request Request
		want    int64
	}{
		{buy, 700},
		{Request{Kind: "csm", Action: "renew", Namespace: "csm-alice", Name: "host", Quantity: 1, Unit: "month"}, 700},
		{Request{Kind: "csm", Action: "expand", Namespace: "csm-alice", Name: "host", CPU: 3, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5}, 50},
	} {
		got, _, params, err := p.Quote(ctx, tc.request)
		if err != nil || got != tc.want || params.Get("cpu") == "" {
			t.Fatalf("%s quote: cents=%d params=%v err=%v", tc.request.Action, got, params, err)
		}
	}
	if err := p.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := p.Quote(ctx, buy); err != ErrPricingMissing {
		t.Fatalf("pricing remained active after reset: %v", err)
	}
}

func TestPricingRejectsInvalidValues(t *testing.T) {
	p := KubePricing{Kube: fake.NewSimpleClientset(), Namespace: "default"}
	if err := p.Put(context.Background(), Prices{CPUCents: -1}); err != ErrInvalid {
		t.Fatalf("negative unit price accepted: %v", err)
	}
	if _, err := weightedCents(Prices{CPUCents: 100_000_000}, Request{CPU: 1_000_001}); err != ErrInvalid {
		t.Fatalf("unbounded resource count accepted: %v", err)
	}
}
