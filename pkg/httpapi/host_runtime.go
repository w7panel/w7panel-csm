package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type hostDeploymentInfo struct {
	Name      string `json:"name"`
	Desired   int32  `json:"desired"`
	Ready     int32  `json:"ready"`
	Updated   int32  `json:"updated"`
	Available int32  `json:"available"`
}

type hostPodInfo struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	Ready    int    `json:"ready"`
	Total    int    `json:"total"`
	IP       string `json:"ip"`
	Node     string `json:"node"`
	Restarts int32  `json:"restarts"`
}

func (s *Server) hostRuntime(w http.ResponseWriter, r *http.Request) {
	ns, ok := namespace(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if !validName(name) {
		respondError(w, 400, "invalid name")
		return
	}
	if _, err := s.Backend.Get(r.Context(), "csms", ns, name); err != nil {
		writeResult(w, nil, err)
		return
	}
	if s.KubeURL == "" || s.KubeToken == "" {
		respondError(w, 503, "Kubernetes runtime is not configured")
		return
	}
	kube := KubeBackend{BaseURL: s.KubeURL, Token: s.KubeToken, Client: s.KubeClient}
	base := strings.TrimRight(s.KubeURL, "/")
	result := struct {
		Deployment *hostDeploymentInfo `json:"deployment"`
		Pods       []hostPodInfo       `json:"pods"`
	}{Pods: []hostPodInfo{}}
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(ns) + "/deployments/" + url.PathEscape(name)
	data, err := kube.call(r.Context(), http.MethodGet, base+path, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		writeResult(w, nil, err)
		return
	}
	if err == nil {
		var deployment appsv1.Deployment
		if json.Unmarshal(data, &deployment) != nil {
			respondError(w, 502, "invalid deployment response")
			return
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		result.Deployment = &hostDeploymentInfo{Name: deployment.Name, Desired: desired, Ready: deployment.Status.ReadyReplicas, Updated: deployment.Status.UpdatedReplicas, Available: deployment.Status.AvailableReplicas}
	}
	query := url.Values{"labelSelector": {"csm.w7.cc/host=" + name}}.Encode()
	data, err = kube.call(r.Context(), http.MethodGet, base+"/api/v1/namespaces/"+url.PathEscape(ns)+"/pods?"+query, nil)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	var pods corev1.PodList
	if json.Unmarshal(data, &pods) != nil {
		respondError(w, 502, "invalid pod response")
		return
	}
	for _, pod := range pods.Items {
		info := hostPodInfo{Name: pod.Name, Phase: string(pod.Status.Phase), Total: len(pod.Spec.Containers), IP: pod.Status.PodIP, Node: pod.Spec.NodeName}
		for _, container := range pod.Status.ContainerStatuses {
			if container.Ready {
				info.Ready++
			}
			info.Restarts += container.RestartCount
		}
		result.Pods = append(result.Pods, info)
	}
	writeResult(w, result, nil)
}
