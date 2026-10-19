package upgrade

import (
	"context"
	"fmt"
	"sort"
	"strings"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Snapshot captures the policy scripts named by a version at approval time.
func Snapshot(ctx context.Context, c client.Client, version *csm.UpgradeVersion) ([]csm.ShellStep, error) {
	if version == nil {
		return nil, fmt.Errorf("upgrade version is required")
	}
	steps := make([]csm.ShellStep, 0, len(version.Spec.Policies))
	seen := map[string]bool{}
	for _, name := range version.Spec.Policies {
		if name == "" || seen[name] {
			return nil, fmt.Errorf("duplicate or empty upgrade policy %q", name)
		}
		seen[name] = true
		policy := &csm.UpgradePolicy{}
		if err := c.Get(ctx, client.ObjectKey{Name: name}, policy); err != nil {
			return nil, err
		}
		if policy.Spec.VersionName != version.Name {
			return nil, fmt.Errorf("policy %q belongs to version %q", name, policy.Spec.VersionName)
		}
		if !policy.Spec.Enabled {
			continue
		}
		if strings.TrimSpace(policy.Spec.Shell) == "" {
			return nil, fmt.Errorf("upgrade policy %q has empty shell", name)
		}
		steps = append(steps, csm.ShellStep{Name: name, Order: policy.Spec.Order, Shell: policy.Spec.Shell})
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Order < steps[j].Order })
	return steps, nil
}

func ValidateApproval(spec csm.UpgradeExecutionSpec) error {
	if strings.TrimSpace(spec.ApprovedBy) == "" {
		return fmt.Errorf("single-host upgrade requires explicit approvedBy")
	}
	if spec.CsmNamespace == "" || spec.CsmName == "" || spec.SourceTemplateName == "" || spec.SourceTemplateHash == "" || spec.TargetTemplateName == "" || spec.TargetTemplateHash == "" || spec.VersionName == "" {
		return fmt.Errorf("upgrade source, target, version and host must be specified")
	}
	if spec.SourceTemplateHash == spec.TargetTemplateHash {
		return fmt.Errorf("target template hash must differ from source")
	}
	for _, step := range spec.Steps {
		if step.Name == "" || strings.TrimSpace(step.Shell) == "" {
			return fmt.Errorf("upgrade step name and shell are required")
		}
	}
	return nil
}

// NewExecution builds the immutable single-host request after the caller has
// obtained a user's approval. The caller assigns a unique name and creates it.
func NewExecution(ctx context.Context, c client.Client, host *csm.Csm, versionName, approvedBy string) (*csm.UpgradeExecution, error) {
	if host == nil {
		return nil, fmt.Errorf("host is required")
	}
	name, hash := host.Spec.TemplateName, host.Status.TemplateHash
	version := &csm.UpgradeVersion{}
	if err := c.Get(ctx, client.ObjectKey{Name: versionName}, version); err != nil {
		return nil, err
	}
	target := &csm.SystemTemplate{}
	if err := c.Get(ctx, client.ObjectKey{Name: version.Spec.TemplateName}, target); err != nil {
		return nil, err
	}
	steps, err := Snapshot(ctx, c, version)
	if err != nil {
		return nil, err
	}
	execution := &csm.UpgradeExecution{Spec: csm.UpgradeExecutionSpec{CsmNamespace: host.GetNamespace(), CsmName: host.GetName(), ApprovedBy: approvedBy, SourceTemplateName: name, SourceTemplateHash: hash, TargetTemplateName: target.Name, TargetTemplateHash: target.Spec.Hash, VersionName: version.Name, Steps: steps}}
	if err := ValidateApproval(execution.Spec); err != nil {
		return nil, err
	}
	return execution, nil
}
