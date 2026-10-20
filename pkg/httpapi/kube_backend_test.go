package httpapi

import (
	"context"
	"encoding/json"
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
