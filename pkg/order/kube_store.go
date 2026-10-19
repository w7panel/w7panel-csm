package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotFound = errors.New("order not found")
var ErrBusy = errors.New("order is being applied")

// KubeStore keeps each order in a tenant namespace CsmOrder CRD. Apply must be
// idempotent by order number; Kubernetes cannot transact across two resources.
type KubeStore struct {
	BaseURL string
	Token   string
	Client  *http.Client
	Apply   func(context.Context, Record) error
}
type orderObject struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Metadata   struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace,omitempty"`
		ResourceVersion string            `json:"resourceVersion,omitempty"`
		Labels          map[string]string `json:"labels,omitempty"`
	} `json:"metadata"`
	Spec struct {
		Record Record `json:"record"`
	} `json:"spec"`
}

func orderName(sn string) string {
	digest := sha256.Sum256([]byte(sn))
	return "csm-order-" + hex.EncodeToString(digest[:12])
}
func (s KubeStore) endpoint(ns, name string) string {
	return strings.TrimRight(s.BaseURL, "/") + "/apis/csm.w7.cc/v1alpha1/namespaces/" + url.PathEscape(ns) + "/csmorders" + name
}
func (s KubeStore) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
func (s KubeStore) call(ctx context.Context, method, endpoint string, input, output any) error {
	var reader io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrNotFound
	}
	if resp.StatusCode == 409 {
		return ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Kubernetes API returned HTTP %d", resp.StatusCode)
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(output)
	}
	return nil
}
func (s KubeStore) SavePending(ctx context.Context, r Record) error {
	if !dnsName(r.Namespace) || r.OrderSn == "" || r.Status != "pending" {
		return ErrInvalid
	}
	cm := orderObject{APIVersion: "csm.w7.cc/v1alpha1", Kind: "CsmOrder"}
	cm.Spec.Record = r
	cm.Metadata.Name = orderName(r.OrderSn)
	cm.Metadata.Namespace = r.Namespace
	cm.Metadata.Labels = map[string]string{"app.kubernetes.io/name": "w7panel-csm-order"}
	return s.call(ctx, http.MethodPost, s.endpoint(r.Namespace, ""), cm, nil)
}
func (s KubeStore) load(ctx context.Context, ns, sn string) (orderObject, Record, error) {
	var cm orderObject
	if !dnsName(ns) || sn == "" {
		return cm, Record{}, ErrInvalid
	}
	if err := s.call(ctx, http.MethodGet, s.endpoint(ns, "/"+orderName(sn)), nil, &cm); err != nil {
		return cm, Record{}, err
	}
	r := cm.Spec.Record
	if r.Namespace != ns || r.OrderSn != sn {
		return cm, r, ErrInvalid
	}
	return cm, r, nil
}
func (s KubeStore) Get(ctx context.Context, ns, sn string) (Record, error) {
	_, r, err := s.load(ctx, ns, sn)
	return r, err
}
func (s KubeStore) List(ctx context.Context, ns string) ([]Record, error) {
	if !dnsName(ns) {
		return nil, ErrInvalid
	}
	var result struct {
		Items []orderObject `json:"items"`
	}
	if err := s.call(ctx, http.MethodGet, s.endpoint(ns, "")+"?labelSelector=app.kubernetes.io%2Fname%3Dw7panel-csm-order", nil, &result); err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(result.Items))
	for _, cm := range result.Items {
		r := cm.Spec.Record
		if r.Namespace != ns {
			return nil, ErrInvalid
		}
		records = append(records, r)
	}
	return records, nil
}
func (s KubeStore) save(ctx context.Context, cm orderObject, r Record) error {
	cm.Spec.Record = r
	return s.call(ctx, http.MethodPut, s.endpoint(r.Namespace, "/"+cm.Metadata.Name), cm, nil)
}
func (s KubeStore) ApplyPaid(ctx context.Context, paid Record) (Record, error) {
	apply := s.Apply
	if apply == nil {
		apply = s.ApplyResource
	}
	cm, current, err := s.load(ctx, paid.Namespace, paid.OrderSn)
	if err != nil {
		return Record{}, err
	}
	if current.Status == "paid" {
		return current, nil
	}
	if current.Status == "applying" && time.Since(current.UpdatedAt) < time.Minute {
		return Record{}, ErrBusy
	}
	if current.Status != "pending" && current.Status != "applying" {
		return Record{}, ErrInvalid
	}
	current.Status = "applying"
	current.UpdatedAt = time.Now().UTC()
	if err := s.save(ctx, cm, current); err != nil {
		return Record{}, err
	}
	if err := apply(ctx, current); err != nil {
		s.retryPending(ctx, current)
		return Record{}, err
	}
	cm, current, err = s.load(ctx, paid.Namespace, paid.OrderSn)
	if err != nil {
		return Record{}, err
	}
	current.Status = "paid"
	if err := s.save(ctx, cm, current); err != nil {
		s.retryPending(ctx, current)
		return Record{}, err
	}
	return current, nil
}

// A failed resource write can be retried immediately; an already-written
// resource carries its order marker, so the next attempt cannot apply twice.
func (s KubeStore) retryPending(ctx context.Context, applying Record) {
	cm, current, err := s.load(ctx, applying.Namespace, applying.OrderSn)
	if err != nil || current.Status != "applying" || !current.UpdatedAt.Equal(applying.UpdatedAt) {
		return
	}
	current.Status = "pending"
	_ = s.save(ctx, cm, current)
}
