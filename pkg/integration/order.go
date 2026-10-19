package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/w7panel/w7panel-csm/pkg/order"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecretCredential reads the tenant's real W7 account credential from its namespace.
// Secret csm-w7-order has account, appId, clusterId, token, openId and expiresAt keys.
func SecretCredential(kube kubernetes.Interface, secretName string) func(context.Context, string) (order.W7Credential, error) {
	return func(ctx context.Context, namespace string) (order.W7Credential, error) {
		secret, err := kube.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return order.W7Credential{}, err
		}
		get := func(key string) string { return string(secret.Data[key]) }
		credential := order.W7Credential{Token: get("token"), Account: get("account"), AppID: get("appId"), ClusterID: get("clusterId"), OpenID: get("openId")}
		if credential.Account == "" || credential.AppID == "" || credential.ClusterID == "" || (credential.Token == "" && credential.OpenID == "") {
			return credential, fmt.Errorf("tenant W7 order secret is incomplete")
		}
		if !strings.HasPrefix(namespace, "csm-") || credential.Account != strings.TrimPrefix(namespace, "csm-") {
			return credential, fmt.Errorf("tenant W7 account does not match namespace")
		}
		if raw := get("expiresAt"); raw != "" {
			credential.ExpiresAt, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				return credential, fmt.Errorf("invalid W7 token expiry: %w", err)
			}
		}
		return credential, nil
	}
}

// RemoteQuote calls a trusted server-side pricing service; request bodies contain no price.
// The response supplies amountCents and the W7 parameter map for the chosen product.
func RemoteQuote(endpoint, token, productID string) func(context.Context, order.Request) (int64, string, url.Values, error) {
	return func(ctx context.Context, choice order.Request) (int64, string, url.Values, error) {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || token == "" || productID == "" {
			return 0, "", nil, fmt.Errorf("trusted HTTPS quote service is not configured")
		}
		body, err := json.Marshal(choice)
		if err != nil {
			return 0, "", nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return 0, "", nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, "", nil, fmt.Errorf("quote service returned HTTP %d", resp.StatusCode)
		}
		var quote struct {
			AmountCents *int64            `json:"amountCents"`
			Free        bool              `json:"free"`
			Params      map[string]string `json:"params"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&quote); err != nil {
			return 0, "", nil, err
		}
		if quote.AmountCents == nil || *quote.AmountCents < 0 || (*quote.AmountCents == 0 && !quote.Free) || quote.Params == nil {
			return 0, "", nil, order.ErrInvalid
		}
		params := url.Values{}
		for key, value := range quote.Params {
			params.Set(key, value)
		}
		// W7Gateway verifies these terms again against payment confirmation.
		for _, key := range []string{"hour", "cpu", "memory", "storage", "bandwidth"} {
			if _, err := strconv.ParseFloat(params.Get(key), 64); err != nil {
				return 0, "", nil, fmt.Errorf("quote missing or invalid %s", key)
			}
		}
		if err := validateQuoteTerms(choice, params, productID, *quote.AmountCents); err != nil {
			return 0, "", nil, err
		}
		return *quote.AmountCents, productID, params, nil
	}
}

func validateQuoteTerms(choice order.Request, params url.Values, productID string, cents int64) error {
	match := func(key, expected string) error {
		if actual := params.Get(key); actual != "" && actual != expected {
			return fmt.Errorf("quote %s differs from order terms", key)
		}
		return nil
	}
	for _, field := range []struct{ key, expected string }{
		{"productId", productID}, {"buymode", map[string]string{"buy": "base", "renew": "renew", "expand": "expand"}[choice.Action]},
		{"ckm_name", choice.Name}, {"price", fmt.Sprintf("%d.%02d", cents/100, cents%100)},
	} {
		if err := match(field.key, field.expected); err != nil {
			return err
		}
	}
	if choice.Quantity > 0 {
		hours := int64(720)
		if choice.Unit == "year" {
			hours = 8640
		}
		if params.Get("hour") != strconv.FormatInt(hours*choice.Quantity, 10) {
			return fmt.Errorf("quote hour differs from order duration")
		}
	}
	if choice.Action == "buy" || choice.Action == "expand" {
		values := []struct {
			key      string
			expected int64
		}{{"cpu", choice.CPU}, {"memory", choice.MemoryGi}, {"storage", choice.StorageGi}, {"bandwidth", choice.BandwidthMbps}}
		for _, value := range values {
			actual, err := strconv.ParseInt(params.Get(value.key), 10, 64)
			if err != nil || actual != value.expected {
				return fmt.Errorf("quote %s differs from order size", value.key)
			}
		}
	}
	if choice.Kind == "dataDisk" {
		if err := match("resource_type", "data_disk"); err != nil {
			return err
		}
		if err := match("resource_name", choice.Name); err != nil {
			return err
		}
	}
	return nil
}
