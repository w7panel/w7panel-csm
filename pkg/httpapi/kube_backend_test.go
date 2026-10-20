package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMountSetsDefaultPath(t *testing.T) {
	var mounted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/namespaces/tenant/datadisks/disk-a":
			w.Write([]byte(`{"spec":{"paid":true}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/namespaces/tenant/csms/host-a":
			w.Write([]byte(`{"spec":{"paid":true}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/namespaces/tenant/datadisks/disk-a":
			if err := json.NewDecoder(r.Body).Decode(&mounted); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	backend := KubeBackend{BaseURL: server.URL, Client: server.Client()}
	if _, err := backend.diskAction(context.Background(), "tenant", "disk-a", "mount", json.RawMessage(`{"csmName":"host-a"}`)); err != nil {
		t.Fatal(err)
	}
	spec, _ := mounted["spec"].(map[string]any)
	if spec["mountPath"] != "/mnt/disk-a" || spec["csmName"] != "host-a" || spec["attached"] != true {
		t.Fatalf("mounted spec: %v", spec)
	}
}

func TestSystemTemplateSave(t *testing.T) {
	var created, updated map[string]any
	putCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/systemtemplates/ubuntu-24.04" {
			w.Write([]byte(`{"apiVersion":"csm.w7.cc/v1alpha1","kind":"SystemTemplate","metadata":{"name":"ubuntu-24.04","resourceVersion":"4"},"spec":{"version":"24.04","source":"official","image":"ubuntu:old","hash":"legacy"}}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/systemtemplates" {
			json.NewDecoder(r.Body).Decode(&created)
			w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/systemtemplates/ubuntu-24.04" {
			putCount++
			json.NewDecoder(r.Body).Decode(&updated)
			w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	b := KubeBackend{BaseURL: server.URL, Client: server.Client()}
	create := json.RawMessage(`{"metadata":{"name":"custom"},"spec":{"version":"1","source":"manual","image":"ubuntu:new"}}`)
	if _, err := b.Action(context.Background(), "system-templates", "", "", "create", create); err != nil {
		t.Fatal(err)
	}
	createdSpec := created["spec"].(map[string]any)
	if createdSpec["hash"] == "" || createdSpec["image"] != "ubuntu:new" {
		t.Fatalf("created spec: %v", createdSpec)
	}
	unchanged := json.RawMessage(`{"metadata":{"name":"ubuntu-24.04","resourceVersion":"4"},"spec":{"version":"24.04","source":"official","image":"ubuntu:old"}}`)
	result, err := b.Action(context.Background(), "system-templates", "", "ubuntu-24.04", "update", unchanged)
	if err != nil || putCount != 0 || !json.Valid(result) {
		t.Fatalf("unchanged template: %s, %v, put=%d", result, err, putCount)
	}
	changed := json.RawMessage(`{"metadata":{"name":"ubuntu-24.04","resourceVersion":"4"},"spec":{"version":"24.04","source":"official","image":"ubuntu:new"}}`)
	if _, err := b.Action(context.Background(), "system-templates", "", "ubuntu-24.04", "update", changed); err != nil {
		t.Fatal(err)
	}
	updatedSpec := updated["spec"].(map[string]any)
	if putCount != 1 || updatedSpec["hash"] == "legacy" || updatedSpec["hash"] == "" {
		t.Fatalf("updated spec: %v, put=%d", updatedSpec, putCount)
	}
	stale := json.RawMessage(`{"metadata":{"name":"ubuntu-24.04","resourceVersion":"3"},"spec":{"version":"24.04","source":"official","image":"ubuntu:new"}}`)
	if _, err := b.Action(context.Background(), "system-templates", "", "ubuntu-24.04", "update", stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
}
