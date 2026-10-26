package preinstall

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Snapshot resolves names once, before execution is created.
func Snapshot(ctx context.Context, c client.Client, names []string) ([]csm.ShellStep, error) {
	steps := make([]csm.ShellStep, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			return nil, fmt.Errorf("duplicate or empty preinstall application %q", name)
		}
		seen[name] = true
		app := &csm.PreinstallApplication{}
		if err := c.Get(ctx, client.ObjectKey{Name: name}, app); err != nil {
			return nil, err
		}
		if !app.Spec.Enabled {
			continue
		}
		if strings.TrimSpace(app.Spec.Shell) == "" {
			return nil, fmt.Errorf("preinstall application %q has empty shell", name)
		}
		steps = append(steps, csm.ShellStep{Name: name, Order: app.Spec.Order, Shell: app.Spec.Shell})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Order < steps[j].Order })
	return steps, nil
}

// CloudConfig validates one cloud-config document before it is stored.
func CloudConfig(raw string) (map[string]any, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "#cloud-config\n") {
		return nil, fmt.Errorf("cloud-config must begin with #cloud-config")
	}
	var value map[string]any
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("cloud-config must contain a mapping")
	}
	return value, nil
}

// CloudConfigSnapshot freezes enabled entries in their declared order.
func CloudConfigSnapshot(ctx context.Context, c client.Client, names []string) (string, error) {
	type entry struct {
		order  int32
		config map[string]any
	}
	entries := make([]entry, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			return "", fmt.Errorf("duplicate or empty initialization config %q", name)
		}
		seen[name] = true
		app := &csm.PreinstallApplication{}
		if err := c.Get(ctx, client.ObjectKey{Name: name}, app); err != nil {
			return "", err
		}
		if !app.Spec.Enabled {
			continue
		}
		config, err := CloudConfig(app.Spec.CloudConfig)
		if err != nil {
			return "", fmt.Errorf("initialization config %q: %w", name, err)
		}
		entries = append(entries, entry{app.Spec.Order, config})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].order < entries[j].order })
	merged := map[string]any{}
	for _, item := range entries {
		mergeConfig(merged, item.config)
	}
	data, err := yaml.Marshal(merged)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(data), nil
}

func mergeConfig(dst, src map[string]any) {
	for key, value := range src {
		if previous, ok := dst[key]; ok {
			if a, ok := previous.(map[string]any); ok {
				if b, ok := value.(map[string]any); ok {
					mergeConfig(a, b)
					continue
				}
			}
			if a, ok := previous.([]any); ok {
				if b, ok := value.([]any); ok {
					dst[key] = append(a, b...)
					continue
				}
			}
		}
		dst[key] = value
	}
}
