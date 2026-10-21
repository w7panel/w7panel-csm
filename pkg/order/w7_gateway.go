package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const W7OrderURL = "https://console.w7.cc/api/thirdparty-cd/k8s-offline/order"

type W7Credential struct {
	Token, Account, AppID, ClusterID, OpenID string
	ExpiresAt                                time.Time
}

// W7Gateway uses tenant-scoped credentials and a server-side quote. The quote
// supplies W7's product ID, amount in cents and its product-specific parameters.
type W7Gateway struct {
	Client     *http.Client
	Endpoint   string
	NotifyURL  string
	Credential func(context.Context, string) (W7Credential, error)
	SaveToken  func(context.Context, string, string, time.Time) error
	Quote      func(context.Context, Request) (int64, string, url.Values, error)
}

func (g W7Gateway) endpoint() string {
	if g.Endpoint != "" {
		return g.Endpoint
	}
	return W7OrderURL
}
func (g W7Gateway) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
func (g W7Gateway) credentials(ctx context.Context, namespace string) (W7Credential, error) {
	if g.Credential == nil {
		return W7Credential{}, errors.New("W7 credential provider is not configured")
	}
	c, err := g.Credential(ctx, namespace)
	if err != nil {
		return c, err
	}
	if c.Account == "" {
		return c, errors.New("W7 account is missing")
	}
	if c.Token == "" || (c.OpenID != "" && c.ExpiresAt.IsZero()) || (!c.ExpiresAt.IsZero() && time.Until(c.ExpiresAt) < time.Minute) {
		if c.OpenID == "" {
			return c, errors.New("W7 account credential is expired and OpenID is missing")
		}
		token, err := g.refreshToken(ctx, c.OpenID)
		if err != nil {
			return c, err
		}
		c.Token = token
		c.ExpiresAt = time.Now().Add(55 * time.Minute)
		if g.SaveToken != nil {
			if err := g.SaveToken(ctx, namespace, token, c.ExpiresAt); err != nil {
				return c, err
			}
		}
	}
	return c, nil
}
func (g W7Gateway) Create(ctx context.Context, r Request) (Payment, error) {
	if g.Quote == nil || g.NotifyURL == "" {
		return Payment{}, errors.New("W7 quote or callback URL is not configured")
	}
	notify, err := url.Parse(g.NotifyURL)
	if err != nil || notify.Scheme != "https" || notify.Host == "" || notify.Path != "/csm-api/v1/order/notify" {
		return Payment{}, errors.New("invalid W7 callback URL")
	}
	c, err := g.credentials(ctx, r.Namespace)
	if err != nil {
		return Payment{}, err
	}
	cents, productID, params, err := g.Quote(ctx, r)
	if err != nil {
		return Payment{}, err
	}
	if cents < 0 || productID == "" {
		return Payment{}, ErrInvalid
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("productId", productID)
	params.Set("clusterId", c.ClusterID)
	params.Set("k3kName", c.Account)
	params.Set("appid", c.AppID)
	params.Set("notify_url", g.NotifyURL)
	params.Set("domain", "https://"+notify.Host)
	params.Set("ckm_name", r.Name)
	params.Set("buymode", map[string]string{"buy": "base", "renew": "renew", "expand": "expand"}[r.Action])
	params.Set("price", strconv.FormatInt(cents/100, 10)+"."+fmt.Sprintf("%02d", cents%100))
	if r.Quantity > 0 {
		hours := int64(720)
		if r.Unit == "day" {
			hours = 24
		} else if r.Unit == "year" {
			hours = 8640
		}
		params.Set("hour", strconv.FormatInt(hours*r.Quantity, 10))
	}
	if r.Action == "buy" {
		params.Set("cpu", strconv.FormatInt(r.CPU, 10))
		params.Set("memory", strconv.FormatInt(r.MemoryGi, 10))
		params.Set("storage", strconv.FormatInt(r.StorageGi, 10))
		params.Set("bandwidth", strconv.FormatInt(r.BandwidthMbps, 10))
	}
	if r.Kind == "dataDisk" {
		params.Set("resource_type", "data_disk")
		params.Set("resource_name", r.Name)
	}
	expected, err := termsFromParams(params)
	if err != nil {
		return Payment{}, err
	}
	body, err := g.request(ctx, http.MethodPost, c.Token, params)
	if err != nil {
		return Payment{}, err
	}
	var result struct {
		OrderSn string `json:"ipOrderSn"`
		Ticket  string `json:"ticket"`
		NeedPay bool   `json:"needPay"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Payment{}, err
	}
	if result.OrderSn == "" || (result.NeedPay && result.Ticket == "") {
		return Payment{}, errors.New("W7 returned incomplete payment details")
	}
	return Payment{OrderSn: result.OrderSn, Ticket: result.Ticket, NeedPay: result.NeedPay, AmountCents: cents, Expected: expected}, nil
}
func (g W7Gateway) Lookup(ctx context.Context, r Record) (PaymentStatus, error) {
	c, err := g.credentials(ctx, r.Namespace)
	if err != nil {
		return PaymentStatus{}, err
	}
	params := url.Values{"orderSn": {r.OrderSn}, "k3kName": {c.Account}}
	body, err := g.request(ctx, http.MethodGet, c.Token, params)
	if err != nil {
		return PaymentStatus{}, err
	}
	var result struct {
		Status    string `json:"ip_order_status"`
		OrderSn   string `json:"ip_order_sn"`
		Name      string `json:"ckm_name"`
		Action    string `json:"buymode"`
		Hour      string `json:"hour"`
		CPU       int64  `json:"cpu"`
		Memory    int64  `json:"memory"`
		Storage   int64  `json:"storage"`
		Bandwidth int64  `json:"bandwidth"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return PaymentStatus{}, err
	}
	action := map[string]string{"base": "buy", "renew": "renew", "expand": "expand"}[result.Action]
	hour, err := parseHour(result.Hour)
	if err != nil {
		return PaymentStatus{}, ErrInvalid
	}
	return PaymentStatus{OrderSn: result.OrderSn, Namespace: r.Namespace, Name: result.Name, Action: action, Paid: result.Status == "paid", CloudTerms: CloudTerms{Hour: hour, CPU: result.CPU, Memory: result.Memory, Storage: result.Storage, Bandwidth: result.Bandwidth}}, nil
}
func (g W7Gateway) request(ctx context.Context, method, token string, params url.Values) ([]byte, error) {
	endpoint, err := url.Parse(g.endpoint())
	if err != nil || endpoint.Scheme != "https" && endpoint.Scheme != "http" || endpoint.Host == "" {
		return nil, errors.New("invalid W7 endpoint")
	}
	var body io.Reader
	if method == http.MethodGet {
		endpoint.RawQuery = params.Encode()
	} else {
		body = strings.NewReader(params.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("W7 order API returned HTTP %d", resp.StatusCode)
	}
	return data, nil
}

func (g W7Gateway) refreshToken(ctx context.Context, openID string) (string, error) {
	endpoint, err := url.Parse(g.endpoint())
	if err != nil {
		return "", err
	}
	endpoint.Path = "/api/thirdparty-cd/k8s-offline/openid-to-cd-token"
	form := url.Values{"openid": {openID}, "useDefaultAppid": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("W7 token refresh returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", errors.New("W7 token refresh returned no token")
	}
	return result.Token, nil
}

func termsFromParams(params url.Values) (CloudTerms, error) {
	var t CloudTerms
	var err error
	t.Hour, err = parseHour(params.Get("hour"))
	if err != nil {
		return t, err
	}
	for _, item := range []struct {
		key    string
		target *int64
	}{{"cpu", &t.CPU}, {"memory", &t.Memory}, {"storage", &t.Storage}, {"bandwidth", &t.Bandwidth}} {
		raw := params.Get(item.key)
		if raw == "" {
			return t, fmt.Errorf("%w: missing %s in W7 quote", ErrInvalid, item.key)
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return t, ErrInvalid
		}
		*item.target = value
	}
	return t, nil
}
func parseHour(raw string) (int64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || value < 0 || value > 1e7 {
		return 0, ErrInvalid
	}
	return int64(math.Round(value * 100)), nil
}
