package runtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type DiskMount struct{ Name, PVCName, Path string }

func Deployment(host *api.Csm, image string, disks []DiskMount) (*appsv1.Deployment, error) {
	if image == "" {
		return nil, fmt.Errorf("template %s has no image", host.Spec.TemplateName)
	}
	for _, key := range host.Spec.SSHPublicKeys {
		fields := strings.Fields(key)
		if len(fields) < 2 || !(strings.HasPrefix(fields[0], "ssh-") || strings.HasPrefix(fields[0], "ecdsa-")) || strings.ContainsAny(key, "\r\n") {
			return nil, fmt.Errorf("invalid SSH public key")
		}
		if _, err := base64.StdEncoding.DecodeString(fields[1]); err != nil {
			return nil, fmt.Errorf("invalid SSH public key encoding")
		}
	}
	labels := map[string]string{"csm.w7.cc/host": host.Name}
	container := corev1.Container{Name: "host", Image: image, Ports: []corev1.ContainerPort{{Name: "ssh", ContainerPort: 2222}}}
	if host.Spec.CPU != "" || host.Spec.Memory != "" {
		container.Resources.Requests = corev1.ResourceList{}
		if host.Spec.CPU != "" {
			q, err := resource.ParseQuantity(host.Spec.CPU)
			if err != nil {
				return nil, err
			}
			container.Resources.Requests[corev1.ResourceCPU] = q
		}
		if host.Spec.Memory != "" {
			q, err := resource.ParseQuantity(host.Spec.Memory)
			if err != nil {
				return nil, err
			}
			container.Resources.Requests[corev1.ResourceMemory] = q
		}
	}
	volumes := []corev1.Volume{{Name: "system", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: host.Status.SystemPVCName}}}}
	for _, disk := range disks {
		if !strings.HasPrefix(disk.Path, "/") || disk.Path == "/" || strings.Contains(disk.Path, "..") {
			return nil, fmt.Errorf("invalid disk mount path %q", disk.Path)
		}
		volumes = append(volumes, corev1.Volume{Name: disk.Name, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: disk.PVCName}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: disk.Name, MountPath: disk.Path})
	}
	container.Env = []corev1.EnvVar{{Name: "CSM_SSH_PUBLIC_KEYS", Value: strings.Join(host.Spec.SSHPublicKeys, "\n")}}
	container.Command = []string{"/bin/sh", "-ec"}
	container.Args = []string{`mkdir -p /root/.ssh /run/sshd /etc/ssh/sshd_config.d
chmod 700 /root/.ssh
printf '%s\n' "$CSM_SSH_PUBLIC_KEYS" > /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
printf 'Port 2222\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin prohibit-password\nPubkeyAuthentication yes\n' > /etc/ssh/sshd_config.d/99-csm.conf
ssh-keygen -A
/usr/sbin/sshd -t
/usr/sbin/sshd -T | grep -qx 'port 2222'
/usr/sbin/sshd -T | grep -qx 'passwordauthentication no'
/usr/sbin/sshd -T | grep -qx 'kbdinteractiveauthentication no'
/usr/sbin/sshd -T | grep -Eq '^permitrootlogin (prohibit-password|without-password)$'
systemctl disable ssh.socket >/dev/null 2>&1 || true
exec /sbin/init`}
	intent, _ := json.Marshal([]map[string]any{{"name": "host", "volumeName": "system", "path": "rootfs", "persistentSpecialMounts": true}})
	annotations := map[string]string{"sysbox/rootfs-rw-layer": string(intent)}
	if host.Spec.BandwidthMbps < 0 {
		return nil, fmt.Errorf("bandwidthMbps cannot be negative")
	}
	if host.Spec.BandwidthMbps > 0 {
		bandwidth := fmt.Sprintf("%dM", host.Spec.BandwidthMbps)
		annotations["kubernetes.io/ingress-bandwidth"] = bandwidth
		annotations["kubernetes.io/egress-bandwidth"] = bandwidth
	}
	replicas := int32(1)
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: host.Name, Namespace: host.Namespace}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: labels}, Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations}, Spec: corev1.PodSpec{RuntimeClassName: ptr("sysbox-runc"), Containers: []corev1.Container{container}, Volumes: volumes}}}}, nil
}

func Service(host *api.Csm, nodePort int32) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: host.Name, Namespace: host.Namespace}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Selector: map[string]string{"csm.w7.cc/host": host.Name}, Ports: []corev1.ServicePort{{Name: "ssh", Port: 22, TargetPort: intstr.FromInt32(2222), NodePort: nodePort}}}}
}
func ptr(s string) *string { return &s }
