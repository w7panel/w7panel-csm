package execshell

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Runner executes Shell steps in the selected host's Linux container.
type Runner interface {
	Run(context.Context, string, string, string) (string, error)
}

var ErrPodUnavailable = fmt.Errorf("Csm pod is not ready")

type KubernetesRunner struct {
	Kube   kubernetes.Interface
	Config *rest.Config
}

func (r KubernetesRunner) Run(ctx context.Context, namespace, host, shell string) (string, error) {
	if shell == "" {
		return "", fmt.Errorf("empty shell command")
	}
	pods, err := r.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "csm.w7.cc/host=" + host})
	if err != nil {
		return "", err
	}
	var pod *corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, condition := range p.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				pod = p
				break
			}
		}
		if pod != nil {
			break
		}
	}
	if pod == nil || len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("ready Csm pod %s/%s is unavailable: %w", namespace, host, ErrPodUnavailable)
	}
	container := pod.Spec.Containers[0].Name
	req := r.Kube.CoreV1().RESTClient().Post().Namespace(namespace).Resource("pods").Name(pod.Name).SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{Container: container, Command: []string{"/bin/sh", "-c", shell}, Stdout: true, Stderr: true}, metav1.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(r.Config, "POST", req.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	return stdout.String() + stderr.String(), err
}
