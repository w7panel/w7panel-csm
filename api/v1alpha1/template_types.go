package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	SchemeBuilder.Register(&SystemTemplate{}, &SystemTemplateList{}, &PreinstallApplication{}, &PreinstallApplicationList{}, &PreinstallExecution{}, &PreinstallExecutionList{}, &UpgradeVersion{}, &UpgradeVersionList{}, &UpgradeExecution{}, &UpgradeExecutionList{})
}

type SystemTemplateSpec struct {
	Version                string   `json:"version"`
	Source                 string   `json:"source"`
	Image                  string   `json:"image"`
	Hash                   string   `json:"hash"`
	PreinstallApplications []string `json:"preinstallApplications,omitempty"`
}

type SystemTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SystemTemplateSpec `json:"spec"`
}
type SystemTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SystemTemplate `json:"items"`
}

type PreinstallApplicationSpec struct {
	Enabled bool   `json:"enabled"`
	Order   int32  `json:"order"`
	Shell   string `json:"shell"`
}
type PreinstallApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PreinstallApplicationSpec `json:"spec"`
}
type PreinstallApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PreinstallApplication `json:"items"`
}

type ShellStep struct {
	Name  string `json:"name"`
	Order int32  `json:"order"`
	Shell string `json:"shell"`
}
type ExecutionStatus struct {
	Phase       string       `json:"phase,omitempty"`
	Completed   []string     `json:"completed,omitempty"`
	Current     string       `json:"current,omitempty"`
	Log         string       `json:"log,omitempty"`
	Message     string       `json:"message,omitempty"`
	StartedAt   *metav1.Time `json:"startedAt,omitempty"`
	AppliedAt   *metav1.Time `json:"appliedAt,omitempty"`
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
type PreinstallExecutionSpec struct {
	CsmNamespace string      `json:"csmNamespace"`
	CsmName      string      `json:"csmName"`
	TemplateName string      `json:"templateName"`
	TemplateHash string      `json:"templateHash"`
	Steps        []ShellStep `json:"steps,omitempty"`
}
type PreinstallExecution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PreinstallExecutionSpec `json:"spec"`
	Status            ExecutionStatus         `json:"status,omitempty"`
}
type PreinstallExecutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PreinstallExecution `json:"items"`
}

type UpgradeVersionSpec struct {
	TemplateName string `json:"templateName"`
	Version      string `json:"version"`
}
type UpgradeVersion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              UpgradeVersionSpec `json:"spec"`
}
type UpgradeVersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpgradeVersion `json:"items"`
}
type UpgradeExecutionSpec struct {
	CsmNamespace       string `json:"csmNamespace"`
	CsmName            string `json:"csmName"`
	ApprovedBy         string `json:"approvedBy"`
	SourceTemplateName string `json:"sourceTemplateName"`
	SourceTemplateHash string `json:"sourceTemplateHash"`
	TargetTemplateName string `json:"targetTemplateName"`
	TargetTemplateHash string `json:"targetTemplateHash"`
	VersionName        string `json:"versionName"`
}
type UpgradeExecution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              UpgradeExecutionSpec `json:"spec"`
	Status            ExecutionStatus      `json:"status,omitempty"`
}
type UpgradeExecutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpgradeExecution `json:"items"`
}

func copyObject[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func (v *SystemTemplate) DeepCopyObject() runtime.Object            { return copyObject(v) }
func (v *SystemTemplateList) DeepCopyObject() runtime.Object        { return copyObject(v) }
func (v *PreinstallApplication) DeepCopyObject() runtime.Object     { return copyObject(v) }
func (v *PreinstallApplicationList) DeepCopyObject() runtime.Object { return copyObject(v) }
func (v *PreinstallExecution) DeepCopyObject() runtime.Object       { return copyObject(v) }
func (v *PreinstallExecutionList) DeepCopyObject() runtime.Object   { return copyObject(v) }
func (v *UpgradeVersion) DeepCopyObject() runtime.Object            { return copyObject(v) }
func (v *UpgradeVersionList) DeepCopyObject() runtime.Object        { return copyObject(v) }
func (v *UpgradeExecution) DeepCopyObject() runtime.Object          { return copyObject(v) }
func (v *UpgradeExecutionList) DeepCopyObject() runtime.Object      { return copyObject(v) }
