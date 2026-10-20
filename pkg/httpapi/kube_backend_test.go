package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
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

func TestAttachKeyUpdatesHost(t *testing.T) {
	const key = "ssh-ed25519 YWJj local"
	var saved struct {
		Spec struct {
			SSHPublicKeys  []string `json:"sshPublicKeys"`
			JumpPublicKeys []string `json:"jumpPublicKeys"`
		} `json:"spec"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/csm.w7.cc/v1alpha1/namespaces/tenant/csms/host" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"metadata":{"name":"host"},"spec":{"sshPublicKeys":[]}}`))
			return
		}
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	b := KubeBackend{BaseURL: server.URL, Client: server.Client()}
	if _, err := b.Action(context.Background(), "csms", "tenant", "host", "ssh-key", json.RawMessage(`{"publicKey":"`+key+`"}`)); err != nil {
		t.Fatal(err)
	}
	if len(saved.Spec.SSHPublicKeys) != 1 || saved.Spec.SSHPublicKeys[0] != key {
		t.Fatalf("host SSH keys: %v", saved.Spec.SSHPublicKeys)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	jumpKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	body, _ := json.Marshal(map[string]string{"publicKey": jumpKey})
	if _, err := b.Action(context.Background(), "csms", "tenant", "host", "jump-key", body); err != nil {
		t.Fatal(err)
	}
	if len(saved.Spec.JumpPublicKeys) != 1 || saved.Spec.JumpPublicKeys[0] != jumpKey {
		t.Fatalf("jump SSH keys: %v", saved.Spec.JumpPublicKeys)
	}
}

func TestUpgradePolicyLinksToVersion(t *testing.T) {
	policies := []any{}
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/upgradeversions/next":
			json.NewEncoder(w).Encode(map[string]any{"apiVersion": "csm.w7.cc/v1alpha1", "kind": "UpgradeVersion", "metadata": map[string]any{"name": "next", "resourceVersion": "1"}, "spec": map[string]any{"templateName": "ubuntu", "version": "2", "policies": policies}})
		case r.Method == http.MethodPost && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/upgradepolicies":
			created = true
			w.Write([]byte(`{"metadata":{"name":"migrate"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/apis/csm.w7.cc/v1alpha1/upgradeversions/next":
			var version struct {
				Spec struct {
					Policies []any `json:"policies"`
				} `json:"spec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&version); err != nil {
				t.Error(err)
			}
			policies = version.Spec.Policies
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	b := KubeBackend{BaseURL: server.URL, Client: server.Client()}
	_, err := b.Action(context.Background(), "upgrade-policies", "", "", "create", json.RawMessage(`{"metadata":{"name":"migrate"},"spec":{"versionName":"next","enabled":true,"order":1,"shell":"echo migrate"}}`))
	if err != nil || !created || len(policies) != 1 || policies[0] != "migrate" {
		t.Fatalf("policy link: created=%v policies=%v err=%v", created, policies, err)
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

func TestUpgradeSnapshotsVersionPolicies(t *testing.T) {
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/csm.w7.cc/v1alpha1/namespaces/tenant/csms/host":
			w.Write([]byte(`{"spec":{"paid":true,"templateName":"old"},"status":{"templateHash":"old-hash"}}`))
		case "/apis/csm.w7.cc/v1alpha1/upgradeversions/next":
			w.Write([]byte(`{"spec":{"templateName":"new","policies":["legacy"]}}`))
		case "/apis/csm.w7.cc/v1alpha1/systemtemplates/new":
			w.Write([]byte(`{"spec":{"hash":"new-hash"}}`))
		case "/apis/csm.w7.cc/v1alpha1/upgradepolicies/legacy":
			w.Write([]byte(`{"spec":{"versionName":"next","enabled":true,"order":2,"shell":"echo upgrade"}}`))
		case "/apis/csm.w7.cc/v1alpha1/upgradeexecutions":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	b := KubeBackend{BaseURL: server.URL, Client: server.Client()}
	ctx := context.WithValue(context.Background(), identityKey{}, Identity{Subject: "alice"})
	if _, err := b.Action(ctx, "upgrade-versions", "", "", "create", json.RawMessage(`{"metadata":{"name":"newer"},"spec":{"templateName":"new","version":"2","policies":["legacy"]}}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("policy input accepted: %v", err)
	}
	if _, err := b.Action(ctx, "csms", "tenant", "host", "upgrade", json.RawMessage(`{"versionName":"next"}`)); err != nil {
		t.Fatal(err)
	}
	spec := created["spec"].(map[string]any)
	steps, _ := spec["steps"].([]any)
	if spec["targetTemplateName"] != "new" || spec["approvedBy"] != "alice" || len(steps) != 1 || steps[0].(map[string]any)["shell"] != "echo upgrade" {
		t.Fatalf("unexpected upgrade execution: %v", spec)
	}
}
