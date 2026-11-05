package order

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeGateway struct{ status PaymentStatus }

func TestBuyQuoteAllowsMissingNameButCreateRequiresOne(t *testing.T) {
	req := Request{Kind: "dataDisk", Action: "buy", Namespace: "tenant-a", StorageGi: 10, Quantity: 1, Unit: "month"}
	if err := Validate(req); err != nil {
		t.Fatalf("unnamed purchase quote: %v", err)
	}
	if _, err := (Service{Gateway: fakeGateway{}, Store: &fakeStore{}}).Create(t.Context(), req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unnamed purchase created: %v", err)
	}
	req.Action = "renew"
	if err := Validate(req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unnamed renewal accepted: %v", err)
	}
}

func (g fakeGateway) Create(context.Context, Request) (Payment, error) {
	return Payment{OrderSn: "SN-1", AmountCents: 1200, Expected: CloudTerms{Hour: 72000, CPU: 2, Memory: 4, Storage: 40, Bandwidth: 5}}, nil
}
func (g fakeGateway) Lookup(_ context.Context, _ Record) (PaymentStatus, error) { return g.status, nil }

type fakeStore struct {
	record  Record
	applied int
}

func (s *fakeStore) SavePending(_ context.Context, r Record) error { s.record = r; return nil }
func (s *fakeStore) Get(_ context.Context, ns, sn string) (Record, error) {
	if s.record.Namespace != ns || s.record.OrderSn != sn {
		return Record{}, ErrNotFound
	}
	return s.record, nil
}
func (s *fakeStore) List(context.Context, string) ([]Record, error) { return []Record{s.record}, nil }
func (s *fakeStore) ApplyPaid(_ context.Context, r Record) (Record, error) {
	if s.record.Status != "paid" {
		s.applied++
		s.record.Status = "paid"
	}
	return s.record, nil
}
func TestPaymentRequiresMatchingCloudOrderAndIsIdempotent(t *testing.T) {
	store := &fakeStore{}
	req := Request{Kind: "csm", Action: "buy", Namespace: "tenant-a", Name: "host-a", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	svc := Service{Gateway: fakeGateway{}, Store: store}
	if _, err := svc.Create(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(t.Context(), req.Namespace, "SN-1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched cloud order: %v", err)
	}
	cloud := PaymentStatus{OrderSn: "SN-1", Namespace: req.Namespace, Name: req.Name, Action: "buy", CloudTerms: store.record.Expected}
	svc.Gateway = fakeGateway{status: cloud}
	if _, err := svc.Refresh(t.Context(), req.Namespace, "SN-1"); !errors.Is(err, ErrUnconfirmed) {
		t.Fatalf("unpaid: %v", err)
	}
	if store.applied != 0 {
		t.Fatal("unpaid order applied")
	}
	cloud.Paid = true
	cloud.Storage = 41
	svc.Gateway = fakeGateway{status: cloud}
	if _, err := svc.Refresh(t.Context(), req.Namespace, "SN-1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed resource: %v", err)
	}
	cloud.Storage = 40
	svc.Gateway = fakeGateway{status: cloud}
	for i := 0; i < 2; i++ {
		if _, err := svc.Refresh(t.Context(), req.Namespace, "SN-1"); err != nil {
			t.Fatal(err)
		}
	}
	if store.applied != 1 {
		t.Fatalf("applied %d times", store.applied)
	}
	if _, err := svc.Refresh(t.Context(), "tenant-b", "SN-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant: %v", err)
	}
}

func TestMockPaymentAppliesImmediately(t *testing.T) {
	store := &fakeStore{}
	req := Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	quote := func(context.Context, Request) (int64, string, url.Values, error) {
		return 1200, "", url.Values{"hour": {"720"}, "cpu": {"2"}, "memory": {"4"}, "storage": {"40"}, "bandwidth": {"5"}}, nil
	}
	svc := Service{Gateway: MockGateway{Quote: quote}, Store: store, AutoConfirm: true}
	record, err := svc.Create(t.Context(), req)
	if err != nil || record.Status != "paid" || record.OrderSn == "" || record.AmountCents != 1200 || record.Ticket != "" || store.applied != 1 {
		t.Fatalf("mock order = %+v, applied = %d, err = %v", record, store.applied, err)
	}
	if _, err := svc.Refresh(t.Context(), req.Namespace, record.OrderSn); err != nil || store.applied != 1 {
		t.Fatalf("duplicate confirmation applied %d times: %v", store.applied, err)
	}
	if _, err := (MockGateway{}).Lookup(t.Context(), Record{Payment: Payment{OrderSn: "real-order", NeedPay: true}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("real order accepted by mock gateway: %v", err)
	}
	store = &fakeStore{}
	svc.Store = store
	svc.Gateway = MockGateway{}
	if _, err := svc.Create(t.Context(), req); !errors.Is(err, ErrPricingMissing) || store.applied != 0 {
		t.Fatalf("missing quote applied order: %v", err)
	}
}

func TestFounderFreeOrderUsesZeroPriceAndConfirms(t *testing.T) {
	store := &fakeStore{}
	req := Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month", FounderFree: true}
	quote := func(context.Context, Request) (int64, string, url.Values, error) {
		return 1200, "", url.Values{"hour": {"720"}, "cpu": {"2"}, "memory": {"4"}, "storage": {"40"}, "bandwidth": {"5"}}, nil
	}
	svc := Service{Gateway: MockGateway{Quote: quote}, Store: store, AutoConfirm: true}
	record, err := svc.Create(t.Context(), req)
	if err != nil || record.AmountCents != 0 || record.Status != "paid" || store.applied != 1 {
		t.Fatalf("free order = %+v, applied = %d, err = %v", record, store.applied, err)
	}
	if err := Validate(Request{FounderFree: true, CouponCode: "CSM-TEST"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("free order with coupon: %v", err)
	}
}

func TestW7FounderFreeSendsZeroPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("price") != "0" {
			t.Errorf("free price: %v, %v", r.Form, err)
		}
		w.Write([]byte(`{"ipOrderSn":"SN-FREE","needPay":false}`))
	}))
	defer server.Close()
	gateway := W7Gateway{Endpoint: server.URL, NotifyURL: "https://csm.example/csm-api/v1/order/notify", Credential: func(context.Context, string) (W7Credential, error) {
		return W7Credential{Token: "token", Account: "alice"}, nil
	}, Quote: func(context.Context, Request) (int64, string, url.Values, error) {
		return 1200, "42", url.Values{"hour": {"720"}, "cpu": {"2"}, "memory": {"4"}, "storage": {"40"}, "bandwidth": {"5"}}, nil
	}}
	req := Request{Kind: "csm", Action: "buy", Namespace: "csm-alice", Name: "host", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month", FounderFree: true}
	result, err := gateway.Create(t.Context(), req)
	if err != nil || result.AmountCents != 0 || result.NeedPay {
		t.Fatalf("free W7 order = %+v, %v", result, err)
	}
}
func TestApplySpecExpansionAndRenewal(t *testing.T) {
	spec := map[string]any{"paid": true, "cpu": "2", "memory": "4Gi", "systemDiskGi": int64(40), "bandwidthMbps": int64(5), "expireTime": "2030-01-01T00:00:00Z"}
	if err := applySpec(spec, Record{Request: Request{Kind: "csm", Action: "expand", CPU: 1, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("shrink: %v", err)
	}
	if err := applySpec(spec, Record{Request: Request{Kind: "csm", Action: "expand", CPU: 3, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5}}); err != nil {
		t.Fatal(err)
	}
	if spec["cpu"] != "3" {
		t.Fatal(spec)
	}
	if err := applySpec(spec, Record{Request: Request{Kind: "csm", Action: "renew", Quantity: 1, Unit: "month"}}); err != nil {
		t.Fatal(err)
	}
	if spec["expireTime"] != "2030-02-01T00:00:00Z" || spec["memory"] != "4Gi" {
		t.Fatal(spec)
	}
	if err := applySpec(spec, Record{Request: Request{Kind: "csm", Action: "renew", Quantity: 3, Unit: "day"}}); err != nil {
		t.Fatal(err)
	}
	if spec["expireTime"] != "2030-02-04T00:00:00Z" {
		t.Fatal(spec)
	}
}
func TestW7GatewayChecksRequestAndResponse(t *testing.T) {
	calls := 0
	cloud := `{"ip_order_status":"paid","ip_order_sn":"SN-1","buymode":"base","hour":"720.00","cpu":2,"memory":4,"storage":40,"bandwidth":5,"ckm_name":"host-a"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing W7 token")
		}
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			if r.Form.Get("productId") != "42" || r.Form.Get("price") != "12.00" || r.Form.Get("notify_url") != "https://csm.example/csm-api/v1/order/notify" {
				t.Errorf("W7 form: %v", r.Form)
			}
			w.Write([]byte(`{"ipOrderSn":"SN-1","ticket":"ticket"}`))
			return
		}
		if r.URL.Query().Get("orderSn") != "SN-1" || r.URL.Query().Get("k3kName") != "account" {
			t.Error("W7 lookup query")
		}
		w.Write([]byte(cloud))
	}))
	defer server.Close()
	g := W7Gateway{Endpoint: server.URL, NotifyURL: "https://csm.example/csm-api/v1/order/notify", Credential: func(context.Context, string) (W7Credential, error) {
		return W7Credential{Token: "token", Account: "account", AppID: "app"}, nil
	}, Quote: func(context.Context, Request) (int64, string, url.Values, error) {
		return 1200, "42", url.Values{}, nil
	}, Client: &http.Client{Timeout: time.Second}}
	req := Request{Kind: "csm", Action: "buy", Namespace: "tenant-a", Name: "host-a", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	payment, err := g.Create(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	status, err := g.Lookup(t.Context(), Record{Request: req, Payment: payment})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Paid || status.CloudTerms != payment.Expected || calls != 2 {
		t.Fatalf("status %+v payment %+v calls %d", status, payment, calls)
	}
	if !strings.HasPrefix(g.endpoint(), "http") {
		t.Fatal(g.endpoint())
	}
}

func TestW7RenewAndExpandCloudTerms(t *testing.T) {
	for _, tc := range []struct {
		name, action, mode, hour string
		request                  Request
		terms                    CloudTerms
	}{
		{"renew", "renew", "renew", "720", Request{Kind: "csm", Action: "renew", Namespace: "csm-a", Name: "host-a", Quantity: 1, Unit: "month"}, CloudTerms{Hour: 72000, CPU: 2, Memory: 4, Storage: 40, Bandwidth: 5}},
		{"renew-days", "renew", "renew", "72", Request{Kind: "csm", Action: "renew", Namespace: "csm-a", Name: "host-a", Quantity: 3, Unit: "day"}, CloudTerms{Hour: 7200, CPU: 2, Memory: 4, Storage: 40, Bandwidth: 5}},
		{"expand", "expand", "expand", "100", Request{Kind: "csm", Action: "expand", Namespace: "csm-a", Name: "host-a", CPU: 4, MemoryGi: 8, StorageGi: 60, BandwidthMbps: 10}, CloudTerms{Hour: 10000, CPU: 4, Memory: 8, Storage: 60, Bandwidth: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloudStorage := tc.terms.Storage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_ = r.ParseForm()
					if r.Form.Get("buymode") != tc.mode || r.Form.Get("hour") != tc.hour || r.Form.Get("storage") != strconv.FormatInt(tc.terms.Storage, 10) {
						t.Errorf("incorrect W7 order terms: %v", r.Form)
					}
					_, _ = w.Write([]byte(`{"ipOrderSn":"SN-2","ticket":"pay"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ip_order_status": "paid", "ip_order_sn": "SN-2", "buymode": tc.mode, "hour": tc.hour, "cpu": tc.terms.CPU, "memory": tc.terms.Memory, "storage": cloudStorage, "bandwidth": tc.terms.Bandwidth, "ckm_name": "host-a"})
			}))
			defer server.Close()
			gateway := W7Gateway{Endpoint: server.URL, NotifyURL: "https://csm.example/csm-api/v1/order/notify", Credential: func(context.Context, string) (W7Credential, error) {
				return W7Credential{Token: "token", Account: "a"}, nil
			}, Quote: func(context.Context, Request) (int64, string, url.Values, error) {
				return 100, "42", url.Values{"hour": {tc.hour}, "cpu": {strconv.FormatInt(tc.terms.CPU, 10)}, "memory": {strconv.FormatInt(tc.terms.Memory, 10)}, "storage": {strconv.FormatInt(tc.terms.Storage, 10)}, "bandwidth": {strconv.FormatInt(tc.terms.Bandwidth, 10)}}, nil
			}}
			store := &fakeStore{}
			service := Service{Gateway: gateway, Store: store}
			if _, err := service.Create(t.Context(), tc.request); err != nil {
				t.Fatal(err)
			}
			cloudStorage++
			if _, err := service.Refresh(t.Context(), "csm-a", "SN-2"); !errors.Is(err, ErrInvalid) {
				t.Fatalf("changed cloud size: %v", err)
			}
			cloudStorage--
			if _, err := service.Refresh(t.Context(), "csm-a", "SN-2"); err != nil || store.applied != 1 {
				t.Fatalf("matching cloud order: applied=%d err=%v", store.applied, err)
			}
		})
	}
}
