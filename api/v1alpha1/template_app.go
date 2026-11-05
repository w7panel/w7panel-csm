package v1alpha1

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

type TemplateEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type TemplateContainer struct {
	Name    string           `json:"name"`
	Image   string           `json:"image"`
	Command []string         `json:"command,omitempty"`
	Args    []string         `json:"args,omitempty"`
	Env     []TemplateEnvVar `json:"env,omitempty"`
}

type TemplateAppSpec struct {
	Env            []TemplateEnvVar    `json:"env,omitempty"`
	Containers     []TemplateContainer `json:"containers,omitempty"`
	InitContainers []TemplateContainer `json:"initContainers,omitempty"`
}

func (app *TemplateAppSpec) Empty() bool {
	return app == nil || len(app.Env) == 0 && len(app.Containers) == 0 && len(app.InitContainers) == 0
}

func (app *TemplateAppSpec) Validate() error {
	if app == nil {
		return nil
	}
	validateEnv := func(env []TemplateEnvVar) error {
		seen := map[string]bool{}
		for _, item := range env {
			if len(validation.IsEnvVarName(item.Name)) != 0 || item.Name == "CSM_SSH_PUBLIC_KEYS" || seen[item.Name] {
				return fmt.Errorf("invalid or duplicate environment variable %q", item.Name)
			}
			seen[item.Name] = true
		}
		return nil
	}
	if err := validateEnv(app.Env); err != nil {
		return err
	}
	seen := map[string]bool{"host": true}
	for _, container := range append(append([]TemplateContainer{}, app.Containers...), app.InitContainers...) {
		if len(validation.IsDNS1123Label(container.Name)) != 0 || seen[container.Name] || strings.TrimSpace(container.Image) == "" {
			return fmt.Errorf("invalid or duplicate container %q", container.Name)
		}
		seen[container.Name] = true
		if err := validateEnv(container.Env); err != nil {
			return err
		}
	}
	return nil
}
