package upgrade

import (
	"context"
	"fmt"
	"strings"

	csm "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	execution := &csm.UpgradeExecution{Spec: csm.UpgradeExecutionSpec{CsmNamespace: host.GetNamespace(), CsmName: host.GetName(), ApprovedBy: approvedBy, SourceTemplateName: name, SourceTemplateHash: hash, TargetTemplateName: target.Name, TargetTemplateHash: target.Spec.Hash, VersionName: version.Name}}
	if err := ValidateApproval(execution.Spec); err != nil {
		return nil, err
	}
	return execution, nil
}
