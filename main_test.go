package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKubernetesConfigFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	data := []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: https://localhost:6443\nusers:\n- name: local\n  user:\n    token: local-token\ncontexts:\n- name: local\n  context:\n    cluster: local\n    user: local\ncurrent-context: local\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
	cfg, err := kubernetesConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://localhost:6443" || cfg.BearerToken != "local-token" {
		t.Fatalf("unexpected Kubernetes config: host=%q token loaded=%t", cfg.Host, cfg.BearerToken != "")
	}
}
