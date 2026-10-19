package runtime

import (
	"strings"
	"testing"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
)

func TestDeploymentSSHSecurityAndSysbox(t *testing.T) {
	h := &api.Csm{Spec: api.CsmSpec{TemplateName: "ubuntu", SSHPublicKeys: []string{"ssh-ed25519 YWJj test"}}, Status: api.CsmStatus{SystemPVCName: "system-test"}}
	h.Name = "test"
	h.Namespace = "tenant"
	d, err := Deployment(h, "ubuntu:24.04", []DiskMount{{Name: "disk", PVCName: "data-disk", Path: "/data"}})
	if err != nil {
		t.Fatal(err)
	}
	c := d.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 2 || c.Command[0] != "/bin/sh" || !strings.Contains(c.Args[0], "authorized_keys") || strings.Index(c.Args[0], "authorized_keys") > strings.Index(c.Args[0], "exec /sbin/init") || !strings.Contains(c.Args[0], "passwordauthentication no") || !strings.Contains(c.Args[0], "kbdinteractiveauthentication no") || !strings.Contains(c.Args[0], "ssh-keygen -A") {
		t.Fatalf("unsafe systemd startup: %#v", c)
	}
	if d.Spec.Template.Spec.RuntimeClassName == nil || *d.Spec.Template.Spec.RuntimeClassName != "sysbox-runc" || !strings.Contains(d.Spec.Template.Annotations["sysbox/rootfs-rw-layer"], `"volumeName":"system"`) {
		t.Fatalf("missing sysbox disk: %#v", d.Spec.Template)
	}
	if len(d.Spec.Template.Spec.Volumes) != 2 || len(c.VolumeMounts) != 1 {
		t.Fatal("data disk mount missing")
	}
	if c.Ports[0].ContainerPort != 2222 || !strings.Contains(c.Args[0], "Port 2222") || Service(h).Spec.Ports[0].TargetPort.IntVal != 2222 {
		t.Fatal("SSH does not use the sysbox-safe container port")
	}
}

func TestDeploymentRejectsUnsafeMount(t *testing.T) {
	h := &api.Csm{Status: api.CsmStatus{SystemPVCName: "system"}}
	if _, err := Deployment(h, "image", []DiskMount{{Name: "d", PVCName: "p", Path: "../etc"}}); err == nil {
		t.Fatal("accepted unsafe mount")
	}
}
