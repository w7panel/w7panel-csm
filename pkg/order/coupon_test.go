package order

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestGenerateCoupons(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	coupons := KubeCoupons{Kube: fake.NewSimpleClientset(), Namespace: "default", Now: func() time.Time { return now }}
	item := Coupon{Code: "IGNORED", Kind: "csm", Action: "buy", Discount: 80, ExpiresAt: now.Add(time.Hour)}
	for _, count := range []int{0, 101} {
		if _, err := coupons.Generate(t.Context(), item, count); !errors.Is(err, ErrInvalid) {
			t.Fatalf("count %d: %v", count, err)
		}
	}
	codes, err := coupons.Generate(t.Context(), item, 3)
	if err != nil || len(codes) != 3 {
		t.Fatalf("generated %v: %v", codes, err)
	}
	listed, err := coupons.List(t.Context())
	if err != nil || len(listed) != 3 {
		t.Fatalf("listed %v: %v", listed, err)
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if !regexp.MustCompile(`^CSM-[0-9A-Z]{8}$`).MatchString(code) || seen[code] {
			t.Fatalf("invalid or duplicate code: %s", code)
		}
		seen[code] = true
	}
}

func TestCouponQuoteLockAndUse(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	kube := fake.NewSimpleClientset()
	coupons := &KubeCoupons{Kube: kube, Namespace: "default", Now: func() time.Time { return now }}
	item := Coupon{Code: "CSM-TEST", Kind: "csm", Action: "buy", Discount: 80, ExpiresAt: now.Add(time.Hour)}
	if err := coupons.Create(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if err := coupons.Create(t.Context(), item); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate coupon: %v", err)
	}
	pricing := KubePricing{Kube: kube, Coupons: coupons, Namespace: "default", ProductID: "42"}
	if err := pricing.Put(t.Context(), Prices{CPUCents: 100, MemoryGiCents: 50, StorageGiCents: 10, BandwidthMbpsCents: 20}); err != nil {
		t.Fatal(err)
	}
	req := Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month", CouponCode: item.Code}
	cents, _, _, err := pricing.Quote(t.Context(), req)
	if err != nil || cents != 720 {
		t.Fatalf("discounted quote = %d, %v", cents, err)
	}
	if err := coupons.Lock(t.Context(), req, "SN-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := coupons.Check(t.Context(), req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("locked coupon accepted: %v", err)
	}
	if err := coupons.Use(t.Context(), item.Code, "SN-2"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other order used coupon: %v", err)
	}
	if err := coupons.Use(t.Context(), item.Code, "SN-1"); err != nil {
		t.Fatal(err)
	}
	if err := coupons.Lock(t.Context(), req, "SN-3"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reused coupon: %v", err)
	}
	if err := coupons.Create(context.Background(), Coupon{Code: "CSM-OLD", Kind: "csm", Action: "buy", Discount: 80, ExpiresAt: now.Add(-time.Second)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired coupon: %v", err)
	}
}
