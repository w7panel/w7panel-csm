package preinstall

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
