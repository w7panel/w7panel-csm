package order

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestKubeStoreRetriesFailedApply(t *testing.T) {
	var saved orderObject
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&saved)
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(saved)
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&saved)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	attempts := 0
	store := KubeStore{BaseURL: server.URL, Apply: func(_ context.Context, _ Record) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary Kubernetes failure")
		}
		return nil
	}}
	r := Record{Request: Request{Namespace: "csm-test", Name: "host-a"}, Payment: Payment{OrderSn: "SN-2"}, Status: "pending"}
	if err := store.SavePending(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPaid(t.Context(), r); err == nil {
		t.Fatal("first resource write should fail")
	}
	if saved.Spec.Record.Status != "pending" {
		t.Fatalf("failed order must become retryable: %s", saved.Spec.Record.Status)
	}
	paid, err := store.ApplyPaid(t.Context(), r)
	if err != nil || paid.Status != "paid" || attempts != 2 {
		t.Fatalf("retry: record=%+v attempts=%d err=%v", paid, attempts, err)
	}
}

func TestKubeStoreAppliesPaidOrderOnce(t *testing.T) {
	var mu sync.Mutex
	var saved orderObject
	var host map[string]any
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(r.URL.Path, "/csmorders") {
			switch r.Method {
			case http.MethodPost:
				if r.URL.Path != "/apis/csm.w7.cc/v1alpha1/namespaces/tenant-a/csmorders" {
					t.Errorf("wrong CRD endpoint: %s", r.URL.Path)
				}
				_ = json.NewDecoder(r.Body).Decode(&saved)
				saved.Metadata.ResourceVersion = "1"
				w.WriteHeader(201)
			case http.MethodGet:
				if saved.Metadata.Name == "" {
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(saved)
			case http.MethodPut:
				var update orderObject
				_ = json.NewDecoder(r.Body).Decode(&update)
				if update.Metadata.ResourceVersion != saved.Metadata.ResourceVersion {
					w.WriteHeader(409)
					return
				}
				saved = update
				saved.Metadata.ResourceVersion = "2"
				w.WriteHeader(200)
			}
			return
		}
		if strings.Contains(r.URL.Path, "/csms") {
			switch r.Method {
			case http.MethodGet:
				if host == nil {
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(host)
			case http.MethodPost:
				creates++
				_ = json.NewDecoder(r.Body).Decode(&host)
				w.WriteHeader(201)
			}
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	store := KubeStore{BaseURL: server.URL, Token: "test", Client: server.Client()}
	r := Record{Request: Request{Kind: "csm", Action: "buy", Namespace: "tenant-a", Name: "host-a", TemplateName: "ubuntu", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}, Payment: Payment{OrderSn: "SN-1"}, Status: "pending"}
	if err := store.SavePending(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		paid, err := store.ApplyPaid(t.Context(), r)
		if err != nil {
			t.Fatal(err)
		}
		if paid.Status != "paid" {
			t.Fatal(paid.Status)
		}
	}
	if creates != 1 {
		t.Fatalf("created host %d times", creates)
	}
	spec := host["spec"].(map[string]any)
	if spec["paid"] != true || spec["cpu"] != "2" || spec["memory"] != "4Gi" || spec["systemDiskGi"] != float64(40) {
		t.Fatalf("host spec: %#v", spec)
	}
}
