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

func (s *Server) hostPod(w http.ResponseWriter, r *http.Request) (string, corev1.Pod, bool) {
	ns, ok := namespace(w, r)
	if !ok {
		return "", corev1.Pod{}, false
	}
	name, podName := r.PathValue("name"), r.PathValue("pod")
	if !validName(name) || !validName(podName) {
		respondError(w, 400, "invalid name")
		return "", corev1.Pod{}, false
	}
	if _, err := s.Backend.Get(r.Context(), "csms", ns, name); err != nil {
		writeResult(w, nil, err)
		return "", corev1.Pod{}, false
	}
	if s.KubeURL == "" || s.KubeToken == "" {
		respondError(w, 503, "Kubernetes runtime is not configured")
		return "", corev1.Pod{}, false
	}
	kube := KubeBackend{BaseURL: s.KubeURL, Token: s.KubeToken, Client: s.KubeClient}
	data, err := kube.call(r.Context(), http.MethodGet, strings.TrimRight(s.KubeURL, "/")+"/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(podName), nil)
	if err != nil {
		writeResult(w, nil, err)
		return "", corev1.Pod{}, false
	}
	var pod corev1.Pod
	if json.Unmarshal(data, &pod) != nil {
		respondError(w, 502, "invalid pod response")
		return "", corev1.Pod{}, false
	}
	if pod.Name != podName || pod.Labels["csm.w7.cc/host"] != name {
		respondError(w, 404, "pod not found")
		return "", corev1.Pod{}, false
	}
	return ns, pod, true
}

func (s *Server) hostPodLogs(w http.ResponseWriter, r *http.Request) {
	ns, pod, ok := s.hostPod(w, r)
	if !ok {
		return
	}
	query := url.Values{"tailLines": {"200"}}
	data, err := (KubeBackend{BaseURL: s.KubeURL, Token: s.KubeToken, Client: s.KubeClient}).call(r.Context(), http.MethodGet, strings.TrimRight(s.KubeURL, "/")+"/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod.Name)+"/log?"+query.Encode(), nil)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeResult(w, struct {
		Log string `json:"log"`
	}{string(data)}, nil)
}

func (s *Server) hostPodEvents(w http.ResponseWriter, r *http.Request) {
	ns, pod, ok := s.hostPod(w, r)
	if !ok {
		return
	}
	query := url.Values{"fieldSelector": {"involvedObject.kind=Pod,involvedObject.name=" + pod.Name}}
	data, err := (KubeBackend{BaseURL: s.KubeURL, Token: s.KubeToken, Client: s.KubeClient}).call(r.Context(), http.MethodGet, strings.TrimRight(s.KubeURL, "/")+"/api/v1/namespaces/"+url.PathEscape(ns)+"/events?"+query.Encode(), nil)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	var list corev1.EventList
	if json.Unmarshal(data, &list) != nil {
		respondError(w, 502, "invalid event response")
		return
	}
	events := make([]corev1.Event, 0, len(list.Items))
	for _, event := range list.Items {
		if event.InvolvedObject.Kind == "Pod" && event.InvolvedObject.Name == pod.Name && (event.InvolvedObject.UID == "" || pod.UID == "" || event.InvolvedObject.UID == pod.UID) {
			events = append(events, event)
		}
	}
	writeResult(w, struct {
		Events []corev1.Event `json:"events"`
	}{events}, nil)
}
