package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ApplyResource writes paid Csm/DataDisk CRDs. The per-order annotation and
// resourceVersion ensure a callback replay cannot extend or expand twice.
func (s KubeStore) ApplyResource(ctx context.Context, r Record) error {
	resource, kind := "csms", "Csm"
	if r.Kind == "dataDisk" {
		resource, kind = "datadisks", "DataDisk"
	}
	endpoint := strings.TrimRight(s.BaseURL, "/") + "/apis/csm.w7.cc/v1alpha1/namespaces/" + r.Namespace + "/" + resource
	marker := "csm.w7.cc/order-" + strings.TrimPrefix(orderName(r.OrderSn), "csm-order-")
	for attempt := 0; attempt < 5; attempt++ {
		var obj struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name            string            `json:"name"`
				Namespace       string            `json:"namespace"`
				ResourceVersion string            `json:"resourceVersion,omitempty"`
				Annotations     map[string]string `json:"annotations,omitempty"`
			} `json:"metadata"`
			Spec map[string]any `json:"spec"`
		}
		err := s.call(ctx, http.MethodGet, endpoint+"/"+r.Name, nil, &obj)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if !exists && r.Action != "buy" {
			return ErrNotFound
		}
		if exists && obj.Metadata.Annotations[marker] == "true" {
			return nil
		}
		if exists && r.Action == "buy" {
			return ErrBusy
		}
		if !exists {
			obj.APIVersion = "csm.w7.cc/v1alpha1"
			obj.Kind = kind
			obj.Metadata.Name = r.Name
			obj.Metadata.Namespace = r.Namespace
			obj.Spec = map[string]any{}
		}
		if obj.Metadata.Annotations == nil {
			obj.Metadata.Annotations = map[string]string{}
		}
		if obj.Spec == nil {
			obj.Spec = map[string]any{}
		}
		if err := applySpec(obj.Spec, r); err != nil {
			return err
		}
		obj.Metadata.Annotations[marker] = "true"
		method, target := http.MethodPut, endpoint+"/"+r.Name
		if !exists {
			method, target = http.MethodPost, endpoint
		}
		err = s.call(ctx, method, target, obj, nil)
		if errors.Is(err, ErrBusy) {
			continue
		}
		return err
	}
	return ErrBusy
}
func applySpec(spec map[string]any, r Record) error {
	if r.Action != "buy" && spec["paid"] != true {
		return ErrInvalid
	}
	switch r.Action {
	case "buy":
		spec["paid"] = true
		if r.StorageClassName != "" {
			spec["storageClassName"] = r.StorageClassName
		}
		spec["storageGi"] = r.StorageGi
		if r.Kind == "csm" {
			delete(spec, "storageGi")
			spec["templateName"] = r.TemplateName
			spec["cpu"] = strconv.FormatInt(r.CPU, 10)
			spec["memory"] = strconv.FormatInt(r.MemoryGi, 10) + "Gi"
			spec["systemDiskGi"] = r.StorageGi
			spec["bandwidthMbps"] = r.BandwidthMbps
		}
	case "expand":
		if r.Kind == "dataDisk" {
			if r.StorageGi <= number(spec["storageGi"]) {
				return ErrInvalid
			}
			spec["storageGi"] = r.StorageGi
		} else {
			if r.CPU < number(spec["cpu"]) || r.MemoryGi < memoryGi(spec["memory"]) || r.StorageGi < number(spec["systemDiskGi"]) || r.BandwidthMbps < number(spec["bandwidthMbps"]) {
				return ErrInvalid
			}
			if r.CPU == number(spec["cpu"]) && r.MemoryGi == memoryGi(spec["memory"]) && r.StorageGi == number(spec["systemDiskGi"]) && r.BandwidthMbps == number(spec["bandwidthMbps"]) {
				return ErrInvalid
			}
			spec["cpu"] = strconv.FormatInt(r.CPU, 10)
			spec["memory"] = strconv.FormatInt(r.MemoryGi, 10) + "Gi"
			spec["systemDiskGi"] = r.StorageGi
			spec["bandwidthMbps"] = r.BandwidthMbps
		}
	case "renew":
		// Resource sizes remain untouched.
	default:
		return ErrInvalid
	}
	if r.Action == "buy" || r.Action == "renew" {
		base := time.Now().UTC()
		if raw, ok := spec["expireTime"].(string); ok && raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return fmt.Errorf("invalid current expiry: %w", err)
			}
			if parsed.After(base) {
				base = parsed
			}
		}
		if r.Unit == "day" {
			base = base.Add(time.Duration(r.Quantity) * 24 * time.Hour)
		} else if r.Unit == "month" {
			base = base.AddDate(0, int(r.Quantity), 0)
		} else {
			base = base.AddDate(int(r.Quantity), 0, 0)
		}
		spec["expireTime"] = base.Format(time.RFC3339)
		if r.Kind == "csm" {
			spec["recycleTime"] = base.Add(72 * time.Hour).Format(time.RFC3339)
		}
	}
	return nil
}
func number(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		value, _ := n.Int64()
		return value
	case float64:
		return int64(n)
	case string:
		value, _ := strconv.ParseInt(n, 10, 64)
		return value
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
func memoryGi(v any) int64 {
	text, _ := v.(string)
	value, _ := strconv.ParseInt(strings.TrimSuffix(text, "Gi"), 10, 64)
	return value
}
