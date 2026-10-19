package execshell

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestExecParametersIncludeContainer(t *testing.T) {
	params, err := scheme.ParameterCodec.EncodeParameters(&corev1.PodExecOptions{Container: "host", Command: []string{"/bin/sh", "-c", "echo ready"}, Stdout: true, Stderr: true}, corev1.SchemeGroupVersion)
	if err != nil || params.Get("container") != "host" || len(params["command"]) != 3 {
		t.Fatalf("exec parameters: %v, %v", params, err)
	}
}
