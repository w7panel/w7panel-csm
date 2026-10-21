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
		{Request{Kind: "dataDisk", Action: "buy", Namespace: "csm-alice", Name: "disk", StorageGi: 30, Quantity: 1, Unit: "month"}, 300},
		{Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu-24.04", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, Quantity: 1, Unit: "year"}, 8400},
		{Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu-24.04", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, Quantity: 3, Unit: "day"}, 70},
		{Request{Kind: "csm", Action: "renew", Namespace: "csm-alice", Name: "host", Quantity: 1, Unit: "month"}, 700},
		{Request{Kind: "csm", Action: "expand", Namespace: "csm-alice", Name: "host", CPU: 3, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5}, 50},
	} {
		got, _, params, err := p.Quote(ctx, tc.request)
		if err != nil || got != tc.want || params.Get("cpu") == "" {
			t.Fatalf("%s quote: cents=%d params=%v err=%v", tc.request.Action, got, params, err)
		}
	}
	prices := Prices{CPUCents: 100, MemoryGiCents: 50, StorageGiCents: 10, BandwidthMbpsCents: 20,
		Combinations: []PriceCombination{{Quantity: 1, Unit: "month", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, DiscountNew: 80, DiscountRenew: 90}, {Quantity: 3, Unit: "day", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, DiscountNew: 80, DiscountRenew: 90}}}
	if err := p.Put(ctx, prices); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request Request
		want    int64
	}{
		{buy, 560},
		{Request{Kind: "csm", Action: "renew", Namespace: "csm-alice", Name: "host", Quantity: 1, Unit: "month"}, 630},
		{Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu-24.04", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, Quantity: 3, Unit: "day"}, 56},
		{Request{Kind: "csm", Action: "renew", Namespace: "csm-alice", Name: "host", Quantity: 3, Unit: "day"}, 63},
		{Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu-24.04", CPU: 2, MemoryGi: 4, StorageGi: 21, BandwidthMbps: 5, Quantity: 1, Unit: "month"}, 710},
		{Request{Kind: "csm", Action: "expand", Namespace: "csm-alice", Name: "host", CPU: 3, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5}, 50},
	} {
		got, _, _, err := p.Quote(ctx, tc.request)
		if err != nil || got != tc.want {
			t.Fatalf("%s combination quote: got %d, want %d, err %v", tc.request.Action, got, tc.want, err)
		}
	}
	prices.CPUCents = 101
	if err := p.Put(ctx, prices); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := p.Quote(ctx, buy); err != nil || got != 562 {
		t.Fatalf("fractional-cent discount: got %d, want 562, err %v", got, err)
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
	combo := PriceCombination{Quantity: 1, Unit: "month", CPU: 2, MemoryGi: 4, StorageGi: 20, BandwidthMbps: 5, DiscountNew: 80, DiscountRenew: 90}
	if err := p.Put(context.Background(), Prices{Combinations: []PriceCombination{combo, combo}}); err != ErrInvalid {
		t.Fatalf("duplicate combination accepted: %v", err)
	}
	combo.DiscountNew = 0
	if err := p.Put(context.Background(), Prices{Combinations: []PriceCombination{combo}}); err != ErrInvalid {
		t.Fatalf("zero discount accepted: %v", err)
	}
}
