package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// KubeBackend is the runtime adapter for CSM's namespaced and cluster CRDs.
type KubeBackend struct {
	BaseURL, Token string
	Client         *http.Client
}

func (b KubeBackend) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
func (b KubeBackend) path(kind, namespace, name string) (string, error) {
	resource := map[string]string{"csms": "csms", "data-disks": "datadisks", "system-templates": "systemtemplates", "preinstall-executions": "preinstallexecutions", "upgrade-executions": "upgradeexecutions", "preinstall-applications": "preinstallapplications", "upgrade-versions": "upgradeversions", "upgrade-policies": "upgradepolicies"}[kind]
	if resource == "" {
		return "", ErrNotFound
	}
	path := strings.TrimRight(b.BaseURL, "/") + "/apis/csm.w7.cc/v1alpha1/"
	if kind == "csms" || kind == "data-disks" {
		if !validName(namespace) {
			return "", ErrForbidden
		}
		path += "namespaces/" + url.PathEscape(namespace) + "/"
	}
	path += resource
	if name != "" {
		if !validName(name) {
			return "", ErrNotFound
		}
		path += "/" + url.PathEscape(name)
	}
	return path, nil
}
func (b KubeBackend) call(ctx context.Context, method, path string, input any) (json.RawMessage, error) {
	var reader io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode == 409 {
		return nil, ErrConflict
	}
	if resp.StatusCode == 403 {
		return nil, ErrForbidden
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Kubernetes API returned HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode == 204 {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
func (b KubeBackend) List(ctx context.Context, kind, namespace string) (json.RawMessage, error) {
	if kind == "ssh-keys" {
		return b.sshKeys(ctx, namespace)
	}
	path, err := b.path(kind, namespace, "")
	if err != nil {
		return nil, err
	}
	data, err := b.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if (kind == "preinstall-executions" || kind == "upgrade-executions") && namespace != "" {
		var list struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		filtered := make([]json.RawMessage, 0, len(list.Items))
		for _, item := range list.Items {
			var obj struct {
				Spec struct {
					CsmNamespace string `json:"csmNamespace"`
				} `json:"spec"`
			}
			if json.Unmarshal(item, &obj) == nil && obj.Spec.CsmNamespace == namespace {
				filtered = append(filtered, item)
			}
		}
		return json.Marshal(map[string]any{"items": filtered})
	}
	return data, nil
}
func (b KubeBackend) Get(ctx context.Context, kind, namespace, name string) (json.RawMessage, error) {
	path, err := b.path(kind, namespace, name)
	if err != nil {
		return nil, err
	}
	return b.call(ctx, http.MethodGet, path, nil)
}
func (b KubeBackend) Action(ctx context.Context, kind, namespace, name, action string, body json.RawMessage) (json.RawMessage, error) {
	switch {
	case kind == "system-templates" && (action == "create" || action == "update"):
		return b.saveSystemTemplate(ctx, name, action, body)
	case (kind == "preinstall-applications" || kind == "upgrade-versions" || kind == "upgrade-policies") && action == "create":
		var input struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec map[string]any `json:"spec"`
		}
		if json.Unmarshal(body, &input) != nil || !validName(input.Metadata.Name) || input.Spec == nil {
			return nil, ErrConflict
		}
		path, _ := b.path(kind, "", "")
		resourceKind := map[string]string{"system-templates": "SystemTemplate", "preinstall-applications": "PreinstallApplication", "upgrade-versions": "UpgradeVersion", "upgrade-policies": "UpgradePolicy"}[kind]
		return b.call(ctx, http.MethodPost, path, map[string]any{"apiVersion": "csm.w7.cc/v1alpha1", "kind": resourceKind, "metadata": map[string]string{"name": input.Metadata.Name}, "spec": input.Spec})
	case (kind == "system-templates" || kind == "preinstall-applications" || kind == "upgrade-versions" || kind == "upgrade-policies") && action == "delete":
		return b.delete(ctx, kind, "", name)
	case kind == "data-disks" && (action == "mount" || action == "unmount"):
		return b.diskAction(ctx, namespace, name, action, body)
	case kind == "data-disks" && action == "delete":
		obj, err := b.object(ctx, kind, namespace, name)
		if err != nil {
			return nil, err
		}
		if obj.Spec["attached"] == true || obj.Status["phase"] == "Mounted" || obj.Status["phase"] == "Mounting" || obj.Status["phase"] == "Unmounting" {
			return nil, ErrConflict
		}
		return b.delete(ctx, kind, namespace, name)
	case kind == "csms" && action == "preinstall":
		return b.createExecution(ctx, namespace, name, "preinstall", body)
	case kind == "csms" && action == "upgrade":
		return b.createExecution(ctx, namespace, name, "upgrade", body)
	case kind == "csms" && action == "ssh-key":
		return b.attachKey(ctx, namespace, name, body)
	case kind == "ssh-keys" && action == "create":
		return b.changeKey(ctx, namespace, name, body, false)
	case kind == "ssh-keys" && action == "delete":
		return b.changeKey(ctx, namespace, name, nil, true)
	}
	return nil, ErrNotFound
}

func (b KubeBackend) saveSystemTemplate(ctx context.Context, name, action string, body json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Metadata struct {
			Name            string `json:"name"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Spec struct {
			Version                string   `json:"version"`
			Source                 string   `json:"source"`
			Image                  string   `json:"image"`
			PreinstallApplications []string `json:"preinstallApplications"`
		} `json:"spec"`
	}
	if json.Unmarshal(body, &input) != nil || !validName(input.Metadata.Name) ||
		(action == "update" && (name != input.Metadata.Name || input.Metadata.ResourceVersion == "")) ||
		strings.TrimSpace(input.Spec.Version) == "" || strings.TrimSpace(input.Spec.Image) == "" ||
		(input.Spec.Source != "official" && input.Spec.Source != "manual") {
		return nil, ErrConflict
	}
	if action == "create" && input.Spec.Source != "manual" {
		return nil, ErrConflict
	}
	seen := map[string]bool{}
	for _, app := range input.Spec.PreinstallApplications {
		if !validName(app) || seen[app] {
			return nil, ErrConflict
		}
		seen[app] = true
		if _, err := b.Get(ctx, "preinstall-applications", "", app); err != nil {
			return nil, err
		}
	}
	spec := map[string]any{"version": input.Spec.Version, "source": input.Spec.Source, "image": input.Spec.Image}
	if len(input.Spec.PreinstallApplications) > 0 {
		spec["preinstallApplications"] = input.Spec.PreinstallApplications
	}
	encoded, _ := json.Marshal(spec)
	if action == "update" {
		current, err := b.object(ctx, "system-templates", "", name)
		if err != nil {
			return nil, err
		}
		if current.Metadata.ResourceVersion != input.Metadata.ResourceVersion {
			return nil, ErrConflict
		}
		if current.Spec["source"] != input.Spec.Source {
			return nil, ErrConflict
		}
		oldSpec := current.Spec
		oldHash := oldSpec["hash"]
		delete(oldSpec, "hash")
		if apps, ok := oldSpec["preinstallApplications"].([]any); ok && len(apps) == 0 {
			delete(oldSpec, "preinstallApplications")
		}
		oldEncoded, _ := json.Marshal(oldSpec)
		if bytes.Equal(oldEncoded, encoded) {
			current.Spec["hash"] = oldHash
			return json.Marshal(current)
		}
		digest := sha256.Sum256(encoded)
		spec["hash"] = hex.EncodeToString(digest[:])
		current.Spec = spec
		return b.update(ctx, "system-templates", "", name, current)
	}
	digest := sha256.Sum256(encoded)
	spec["hash"] = hex.EncodeToString(digest[:])
	path, _ := b.path("system-templates", "", "")
	return b.call(ctx, http.MethodPost, path, map[string]any{"apiVersion": "csm.w7.cc/v1alpha1", "kind": "SystemTemplate", "metadata": map[string]string{"name": input.Metadata.Name}, "spec": spec})
}
func (b KubeBackend) delete(ctx context.Context, kind, namespace, name string) (json.RawMessage, error) {
	path, err := b.path(kind, namespace, name)
	if err != nil {
		return nil, err
	}
	return b.call(ctx, http.MethodDelete, path, nil)
}

type kubeObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace,omitempty"`
		ResourceVersion string `json:"resourceVersion,omitempty"`
		GenerateName    string `json:"generateName,omitempty"`
	} `json:"metadata"`
	Spec   map[string]any `json:"spec"`
	Status map[string]any `json:"status,omitempty"`
}

func (b KubeBackend) object(ctx context.Context, kind, ns, name string) (kubeObject, error) {
	var obj kubeObject
	data, err := b.Get(ctx, kind, ns, name)
	if err != nil {
		return obj, err
	}
	err = json.Unmarshal(data, &obj)
	return obj, err
}
func (b KubeBackend) update(ctx context.Context, kind, ns, name string, obj kubeObject) (json.RawMessage, error) {
	path, err := b.path(kind, ns, name)
	if err != nil {
		return nil, err
	}
	return b.call(ctx, http.MethodPut, path, obj)
}
func (b KubeBackend) diskAction(ctx context.Context, ns, name, action string, body json.RawMessage) (json.RawMessage, error) {
	var req struct {
		CsmName string `json:"csmName"`
	}
	if action == "mount" && (json.Unmarshal(body, &req) != nil || !validName(req.CsmName)) {
		return nil, ErrConflict
	}
	for attempt := 0; attempt < 5; attempt++ {
		disk, err := b.object(ctx, "data-disks", ns, name)
		if err != nil {
			return nil, err
		}
		if disk.Spec["paid"] != true {
			return nil, ErrConflict
		}
		if action == "mount" {
			if disk.Spec["attached"] == true {
				return nil, ErrConflict
			}
			host, err := b.object(ctx, "csms", ns, req.CsmName)
			if err != nil {
				return nil, err
			}
			if host.Spec["paid"] != true {
				return nil, ErrConflict
			}
			disk.Spec["csmName"] = req.CsmName
			disk.Spec["attached"] = true
			if disk.Spec["mountPath"] == nil || disk.Spec["mountPath"] == "" {
				disk.Spec["mountPath"] = "/mnt/" + name
			}
		} else {
			disk.Spec["attached"] = false
		}
		result, err := b.update(ctx, "data-disks", ns, name, disk)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return result, err
	}
	return nil, ErrConflict
}
func validPublicKey(key string) bool {
	parts := strings.Fields(key)
	if len(parts) < 2 || len(key) > 8192 || strings.ContainsAny(key, "\r\n") {
		return false
	}
	if parts[0] != "ssh-ed25519" && parts[0] != "ssh-rsa" && !strings.HasPrefix(parts[0], "ecdsa-sha2-") {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(parts[1])
	return err == nil
}
func (b KubeBackend) attachKey(ctx context.Context, ns, name string, body json.RawMessage) (json.RawMessage, error) {
	var req struct {
		PublicKey string `json:"publicKey"`
	}
	if json.Unmarshal(body, &req) != nil || !validPublicKey(req.PublicKey) {
		return nil, ErrConflict
	}
	for i := 0; i < 5; i++ {
		host, err := b.object(ctx, "csms", ns, name)
		if err != nil {
			return nil, err
		}
		keys, _ := host.Spec["sshPublicKeys"].([]any)
		for _, v := range keys {
			if v == req.PublicKey {
				return json.Marshal(host)
			}
		}
		host.Spec["sshPublicKeys"] = append(keys, req.PublicKey)
		data, err := b.update(ctx, "csms", ns, name, host)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return data, err
	}
	return nil, ErrConflict
}
func (b KubeBackend) keyPath(ns string) string {
	return strings.TrimRight(b.BaseURL, "/") + "/api/v1/namespaces/" + url.PathEscape(ns) + "/configmaps/csm-ssh-keys"
}
func (b KubeBackend) sshKeys(ctx context.Context, ns string) (json.RawMessage, error) {
	if !validName(ns) {
		return nil, ErrForbidden
	}
	data, err := b.call(ctx, http.MethodGet, b.keyPath(ns), nil)
	if errors.Is(err, ErrNotFound) {
		return []byte(`{"items":[]}`), nil
	}
	if err != nil {
		return nil, err
	}
	var cm struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(data, &cm); err != nil {
		return nil, err
	}
	items := make([]map[string]string, 0, len(cm.Data))
	for name, key := range cm.Data {
		items = append(items, map[string]string{"name": name, "publicKey": key})
	}
	return json.Marshal(map[string]any{"items": items})
}
func (b KubeBackend) changeKey(ctx context.Context, ns, name string, body json.RawMessage, remove bool) (json.RawMessage, error) {
	if !validName(ns) {
		return nil, ErrForbidden
	}
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
	}
	if !remove {
		if json.Unmarshal(body, &req) != nil || !validName(req.Name) || !validPublicKey(req.PublicKey) {
			return nil, ErrConflict
		}
		name = req.Name
	}
	if !validName(name) {
		return nil, ErrConflict
	}
	for i := 0; i < 5; i++ {
		var cm struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name            string `json:"name"`
				Namespace       string `json:"namespace"`
				ResourceVersion string `json:"resourceVersion,omitempty"`
			} `json:"metadata"`
			Data map[string]string `json:"data"`
		}
		raw, err := b.call(ctx, http.MethodGet, b.keyPath(ns), nil)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if exists {
			if err := json.Unmarshal(raw, &cm); err != nil {
				return nil, err
			}
		} else {
			cm.APIVersion = "v1"
			cm.Kind = "ConfigMap"
			cm.Metadata.Name = "csm-ssh-keys"
			cm.Metadata.Namespace = ns
			cm.Data = map[string]string{}
		}
		if remove {
			if _, ok := cm.Data[name]; !ok {
				return nil, ErrNotFound
			}
			delete(cm.Data, name)
		} else {
			cm.Data[name] = req.PublicKey
		}
		method, path := http.MethodPut, b.keyPath(ns)
		if !exists {
			method = http.MethodPost
			path = strings.TrimSuffix(b.keyPath(ns), "/csm-ssh-keys")
		}
		result, err := b.call(ctx, method, path, cm)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return result, err
	}
	return nil, ErrConflict
}
func (b KubeBackend) createExecution(ctx context.Context, ns, name, kind string, body json.RawMessage) (json.RawMessage, error) {
	host, err := b.object(ctx, "csms", ns, name)
	if err != nil {
		return nil, err
	}
	if host.Spec["paid"] != true {
		return nil, ErrConflict
	}
	source, _ := host.Spec["templateName"].(string)
	sourceHash, _ := host.Status["templateHash"].(string)
	var spec map[string]any
	var steps []map[string]any
	resource, resourceKind := "preinstall-executions", "PreinstallExecution"
	if kind == "preinstall" {
		template, err := b.object(ctx, "system-templates", "", source)
		if err != nil {
			return nil, err
		}
		names, _ := template.Spec["preinstallApplications"].([]any)
		if source == "" || sourceHash == "" || sourceHash != template.Spec["hash"] {
			return nil, ErrConflict
		}
		seen := map[string]bool{}
		for _, value := range names {
			appName, ok := value.(string)
			if !ok || !validName(appName) || seen[appName] {
				return nil, ErrConflict
			}
			seen[appName] = true
			app, err := b.object(ctx, "preinstall-applications", "", appName)
			if err != nil {
				return nil, err
			}
			if app.Spec["enabled"] == true {
				shell, _ := app.Spec["shell"].(string)
				if strings.TrimSpace(shell) == "" {
					return nil, ErrConflict
				}
				steps = append(steps, map[string]any{"name": appName, "order": app.Spec["order"], "shell": app.Spec["shell"]})
			}
		}
		spec = map[string]any{"csmNamespace": ns, "csmName": name, "templateName": source, "templateHash": template.Spec["hash"], "steps": steps}
	} else {
		var req struct {
			VersionName string `json:"versionName"`
		}
		if json.Unmarshal(body, &req) != nil || !validName(req.VersionName) || identity(ctx).Subject == "" {
			return nil, ErrConflict
		}
		version, err := b.object(ctx, "upgrade-versions", "", req.VersionName)
		if err != nil {
			return nil, err
		}
		targetName, _ := version.Spec["templateName"].(string)
		target, err := b.object(ctx, "system-templates", "", targetName)
		if err != nil {
			return nil, err
		}
		names, _ := version.Spec["policies"].([]any)
		targetHash, _ := target.Spec["hash"].(string)
		if source == "" || sourceHash == "" || targetName == "" || targetHash == "" || sourceHash == targetHash {
			return nil, ErrConflict
		}
		seen := map[string]bool{}
		for _, value := range names {
			policyName, ok := value.(string)
			if !ok || !validName(policyName) || seen[policyName] {
				return nil, ErrConflict
			}
			seen[policyName] = true
			policy, err := b.object(ctx, "upgrade-policies", "", policyName)
			if err != nil {
				return nil, err
			}
			if policy.Spec["versionName"] != req.VersionName {
				return nil, ErrConflict
			}
			if policy.Spec["enabled"] == true {
				shell, _ := policy.Spec["shell"].(string)
				if strings.TrimSpace(shell) == "" {
					return nil, ErrConflict
				}
				steps = append(steps, map[string]any{"name": policyName, "order": policy.Spec["order"], "shell": policy.Spec["shell"]})
			}
		}
		spec = map[string]any{"csmNamespace": ns, "csmName": name, "approvedBy": identity(ctx).Subject, "sourceTemplateName": source, "sourceTemplateHash": sourceHash, "targetTemplateName": targetName, "targetTemplateHash": target.Spec["hash"], "versionName": req.VersionName, "steps": steps}
		resource, resourceKind = "upgrade-executions", "UpgradeExecution"
	}
	sort.SliceStable(steps, func(i, j int) bool { return numeric(steps[i]["order"]) < numeric(steps[j]["order"]) })
	path, _ := b.path(resource, "", "")
	return b.call(ctx, http.MethodPost, path, map[string]any{"apiVersion": "csm.w7.cc/v1alpha1", "kind": resourceKind, "metadata": map[string]string{"generateName": kind + "-" + name + "-"}, "spec": spec})
}

func numeric(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return 0
}
