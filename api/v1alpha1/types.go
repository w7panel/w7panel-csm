// Package v1alpha1 defines the CSM host and independently purchased data disk API.
package v1alpha1

import (
	"crypto/sha256"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var GroupVersion = schema.GroupVersion{Group: "csm.w7.cc", Version: "v1alpha1"}
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
var AddToScheme = SchemeBuilder.AddToScheme

const (
	CsmFinalizer  = "csm.w7.cc/ssh-port"
	DiskFinalizer = "csm.w7.cc/data-disk"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=csms,scope=Namespaced
// +kubebuilder:subresource:status
type Csm struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CsmSpec   `json:"spec"`
	Status            CsmStatus `json:"status,omitempty"`
}

type CsmSpec struct {
	TemplateName     string   `json:"templateName"`
	Paid             bool     `json:"paid,omitempty"`
	BandwidthMbps    int64    `json:"bandwidthMbps,omitempty"`
	ExpireTime       string   `json:"expireTime,omitempty"`
	RecycleTime      string   `json:"recycleTime,omitempty"`
	StorageClassName string   `json:"storageClassName,omitempty"`
	SystemDiskGi     int64    `json:"systemDiskGi"`
	CPU              string   `json:"cpu,omitempty"`
	Memory           string   `json:"memory,omitempty"`
	SSHPublicKeys    []string `json:"sshPublicKeys,omitempty"`
	JumpPublicKeys   []string `json:"jumpPublicKeys,omitempty"`
}

type CsmStatus struct {
	Phase         string `json:"phase,omitempty"`
	InitMode      string `json:"initMode,omitempty"`
	SystemPVCName string `json:"systemPVCName,omitempty"`
	SSHPort       int32  `json:"sshPort,omitempty"`
	SSHHost       string `json:"sshHost,omitempty"`
	TemplateHash  string `json:"templateHash,omitempty"`
	Message       string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type CsmList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Csm `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=datadisks,scope=Namespaced
// +kubebuilder:subresource:status
type DataDisk struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DataDiskSpec   `json:"spec"`
	Status            DataDiskStatus `json:"status,omitempty"`
}

type DataDiskSpec struct {
	CsmName          string `json:"csmName,omitempty"`
	ExpireTime       string `json:"expireTime,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	StorageGi        int64  `json:"storageGi"`
	Paid             bool   `json:"paid,omitempty"`
	Attached         bool   `json:"attached,omitempty"`
	MountPath        string `json:"mountPath,omitempty"`
}

type DataDiskStatus struct {
	Phase   string `json:"phase,omitempty"`
	PVCName string `json:"pvcName,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
type DataDiskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataDisk `json:"items"`
}

func (in *Csm) DeepCopyObject() runtime.Object {
	out := new(Csm)
	*out = *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Spec.SSHPublicKeys = append([]string(nil), in.Spec.SSHPublicKeys...)
	out.Spec.JumpPublicKeys = append([]string(nil), in.Spec.JumpPublicKeys...)
	return out
}

func SSHDomain(namespace, name, suffix string) string {
	sum := sha256.Sum256([]byte(namespace + "/" + name))
	if len(name) > 24 {
		name = name[:24]
	}
	if len(namespace) > 24 {
		namespace = namespace[:24]
	}
	return fmt.Sprintf("%s-%s-%x.%s", name, namespace, sum[:6], suffix)
}
func (in *CsmList) DeepCopyObject() runtime.Object {
	out := new(CsmList)
	*out = *in
	out.ListMeta = in.ListMeta
	out.Items = make([]Csm, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *(in.Items[i].DeepCopyObject().(*Csm))
	}
	return out
}
func (in *DataDisk) DeepCopyObject() runtime.Object {
	out := new(DataDisk)
	*out = *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	return out
}
func (in *DataDiskList) DeepCopyObject() runtime.Object {
	out := new(DataDiskList)
	*out = *in
	out.Items = make([]DataDisk, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *(in.Items[i].DeepCopyObject().(*DataDisk))
	}
	return out
}

func init() { SchemeBuilder.Register(&Csm{}, &CsmList{}, &DataDisk{}, &DataDiskList{}) }
