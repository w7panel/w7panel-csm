package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/w7panel/w7panel-csm/pkg/order"
	"k8s.io/apimachinery/pkg/util/validation"
)

const Prefix = "/csm-api/v1"

type Identity struct {
	Namespace string
	Subject   string
	Founder   bool
}

// Authenticate verifies an OIDC access token and resolves its trusted tenant namespace.
type Authenticate func(context.Context, string) (Identity, error)

// Backend receives a namespace resolved from the authenticated identity.
// KubeBackend is the concrete Kubernetes implementation.
type Backend interface {
	List(context.Context, string, string) (json.RawMessage, error)
	Get(context.Context, string, string, string) (json.RawMessage, error)
	Action(context.Context, string, string, string, string, json.RawMessage) (json.RawMessage, error)
}

type Server struct {
	Authenticate Authenticate
	Backend      Backend
	Orders       order.Service
	Pricing      interface {
		Get(context.Context) (order.Prices, error)
		Put(context.Context, order.Prices) error
		Delete(context.Context) error
		Quote(context.Context, order.Request) (int64, string, url.Values, error)
	}
	Coupons        *order.KubeCoupons
	UIRoot         string // absolute path to ui/dist at runtime
	KubeURL        string
	KubeToken      string
	KubeClient     *http.Client
	ResolveAccount func(context.Context, string) (string, error)
}

func NewServer(auth Authenticate, backend Backend, orders order.Service, uiRoot string) *Server {
	return &Server{Authenticate: auth, Backend: backend, Orders: orders, UIRoot: uiRoot}
}

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrForbidden = errors.New("forbidden")

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	if s.UIRoot != "" {
		mux.HandleFunc("GET /", s.serveUI)
	}
	route := func(method, path string, fn http.HandlerFunc) {
		mux.HandleFunc(method+" "+Prefix+path, s.authorize(fn))
	}
	route("GET", "/csms", s.list("csms"))
	route("GET", "/csms/{name}", s.get("csms"))
	route("GET", "/csms/{name}/runtime", s.hostRuntime)
	route("POST", "/csms/{name}/preinstall", s.action("csms", "preinstall"))
	route("POST", "/csms/{name}/upgrade", s.action("csms", "upgrade"))
	route("GET", "/csms/{name}/terminal", s.terminal)
	route("POST", "/csms/{name}/ssh-keys", s.action("csms", "ssh-key"))
	route("POST", "/csms/{name}/jump-keys", s.action("csms", "jump-key"))
	route("GET", "/system-templates", s.list("system-templates"))
	route("GET", "/system-templates/{name}", s.get("system-templates"))
	route("POST", "/system-templates", s.action("system-templates", "create"))
	route("PUT", "/system-templates/{name}", s.action("system-templates", "update"))
	route("DELETE", "/system-templates/{name}", s.action("system-templates", "delete"))
	route("GET", "/preinstall-executions", s.list("preinstall-executions"))
	route("GET", "/preinstall-applications", s.list("preinstall-applications"))
	route("POST", "/preinstall-applications", s.action("preinstall-applications", "create"))
	route("PUT", "/preinstall-applications/{name}", s.action("preinstall-applications", "update"))
	route("DELETE", "/preinstall-applications/{name}", s.action("preinstall-applications", "delete"))
	route("GET", "/upgrade-versions", s.list("upgrade-versions"))
	route("POST", "/upgrade-versions", s.action("upgrade-versions", "create"))
	route("DELETE", "/upgrade-versions/{name}", s.action("upgrade-versions", "delete"))
	route("GET", "/upgrade-policies", s.list("upgrade-policies"))
	route("POST", "/upgrade-policies", s.action("upgrade-policies", "create"))
	route("PUT", "/upgrade-policies/{name}", s.action("upgrade-policies", "update"))
	route("DELETE", "/upgrade-policies/{name}", s.action("upgrade-policies", "delete"))
	route("GET", "/upgrade-executions", s.list("upgrade-executions"))
	route("GET", "/data-disks", s.list("data-disks"))
	route("GET", "/data-disks/{name}", s.get("data-disks"))
	route("POST", "/data-disks/{name}/mount", s.action("data-disks", "mount"))
	route("POST", "/data-disks/{name}/unmount", s.action("data-disks", "unmount"))
	route("DELETE", "/data-disks/{name}", s.action("data-disks", "delete"))
	route("POST", "/orders", s.createOrder)
	route("GET", "/session", s.session)
	route("GET", "/accounts", s.accounts)
	route("GET", "/coupons", s.listCoupons)
	route("POST", "/coupons", s.createCoupon)
	route("GET", "/pricing", s.getPricing)
	route("PUT", "/pricing", s.putPricing)
	route("DELETE", "/pricing", s.deletePricing)
	route("POST", "/orders/quote", s.quoteOrder)
	route("GET", "/orders", s.listOrders)
	route("POST", "/orders/{sn}/refresh", s.refreshOrder)
	mux.HandleFunc("POST "+Prefix+"/order/notify", s.notifyOrder)
	return mux
}

func (s *Server) getPricing(w http.ResponseWriter, r *http.Request) {
	if s.Pricing == nil {
		respondError(w, 503, "pricing is not configured")
		return
	}
	prices, err := s.Pricing.Get(r.Context())
	if errors.Is(err, order.ErrPricingMissing) {
		respondError(w, 404, "pricing is not configured")
		return
	}
	writeResult(w, prices, err)
}

func (s *Server) putPricing(w http.ResponseWriter, r *http.Request) {
	if !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	if s.Pricing == nil {
		respondError(w, 503, "pricing is not configured")
		return
	}
	var input struct {
		CPUCents           *int64                    `json:"cpuCents"`
		MemoryGiCents      *int64                    `json:"memoryGiCents"`
		StorageGiCents     *int64                    `json:"storageGiCents"`
		BandwidthMbpsCents *int64                    `json:"bandwidthMbpsCents"`
		Combinations       *[]order.PriceCombination `json:"combinations"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.CPUCents == nil || input.MemoryGiCents == nil || input.StorageGiCents == nil || input.BandwidthMbpsCents == nil {
		respondError(w, 400, "invalid pricing JSON")
		return
	}
	prices := order.Prices{CPUCents: *input.CPUCents, MemoryGiCents: *input.MemoryGiCents, StorageGiCents: *input.StorageGiCents, BandwidthMbpsCents: *input.BandwidthMbpsCents}
	if input.Combinations != nil {
		prices.Combinations = *input.Combinations
	} else if existing, err := s.Pricing.Get(r.Context()); err == nil {
		prices.Combinations = existing.Combinations
	} else if !errors.Is(err, order.ErrPricingMissing) {
		writeResult(w, nil, err)
		return
	}
	if err := s.Pricing.Put(r.Context(), prices); err != nil {
		writeResult(w, nil, err)
		return
	}
	writeResult(w, prices, nil)
}

func (s *Server) deletePricing(w http.ResponseWriter, r *http.Request) {
	if !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	if s.Pricing == nil {
		respondError(w, 503, "pricing is not configured")
		return
	}
	if err := s.Pricing.Delete(r.Context()); err != nil {
		writeResult(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) quoteOrder(w http.ResponseWriter, r *http.Request) {
	if s.Pricing == nil {
		respondError(w, 503, "pricing is not configured")
		return
	}
	ns, ok := s.orderNamespace(w, r)
	if !ok {
		return
	}
	var req order.Request
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF {
		respondError(w, 400, "invalid JSON")
		return
	}
	req.Namespace = ns
	if req.FounderFree && !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	cents, _, _, err := s.Pricing.Quote(r.Context(), req)
	if req.FounderFree {
		cents = 0
	}
	if errors.Is(err, order.ErrPricingMissing) {
		respondError(w, 503, "pricing is not configured")
		return
	}
	writeResult(w, struct {
		AmountCents int64 `json:"amountCents"`
	}{cents}, err)
}

type identityKey struct{}

func identity(ctx context.Context) Identity { return ctx.Value(identityKey{}).(Identity) }

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authenticate := s.Authenticate
		if authenticate == nil || s.Backend == nil || s.Orders.Store == nil || s.Orders.Gateway == nil {
			respondError(w, http.StatusServiceUnavailable, "service is not configured")
			return
		}
		header := r.Header.Get("Authorization-csm")
		if header == "" {
			header = r.Header.Get("Authorization")
		}
		if header == "" && strings.HasSuffix(r.URL.Path, "/terminal") {
			for _, protocol := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
				protocol = strings.TrimSpace(protocol)
				if strings.HasPrefix(protocol, "bearer.") {
					header = "Bearer " + strings.TrimPrefix(protocol, "bearer.")
					break
				}
			}
		}
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			respondError(w, http.StatusUnauthorized, "access token required")
			return
		}
		user, err := authenticate(r.Context(), token)
		if err != nil || !validName(user.Namespace) {
			respondError(w, http.StatusUnauthorized, "invalid access token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
	}
}

func namespace(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := identity(r.Context())
	requested := r.URL.Query().Get("namespace")
	if !user.Founder || requested == "" {
		requested = user.Namespace
	}
	if !validName(requested) {
		respondError(w, http.StatusBadRequest, "invalid namespace")
		return "", false
	}
	return requested, true
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	user := identity(r.Context())
	writeResult(w, struct {
		Namespace string `json:"namespace"`
		Founder   bool   `json:"founder"`
	}{user.Namespace, user.Founder}, nil)
}

func (s *Server) kubeNamespaces(ctx context.Context, name string, result any) error {
	if s.KubeURL == "" || s.KubeToken == "" {
		return errors.New("Kubernetes account lookup is not configured")
	}
	endpoint := strings.TrimRight(s.KubeURL, "/") + "/api/v1/namespaces"
	if name != "" {
		endpoint += "/" + url.PathEscape(name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.KubeToken)
	client := s.KubeClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Kubernetes account lookup returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result)
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	if !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := s.kubeNamespaces(r.Context(), "", &list); err != nil {
		writeResult(w, nil, err)
		return
	}
	accounts := []string{}
	for _, item := range list.Items {
		name := item.Metadata.Name
		if strings.HasPrefix(name, "csm-") && validName(name) {
			accounts = append(accounts, name)
		}
	}
	writeResult(w, struct {
		Items []string `json:"items"`
	}{accounts}, nil)
}

func (s *Server) listCoupons(w http.ResponseWriter, r *http.Request) {
	if !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	if s.Coupons == nil {
		respondError(w, 503, "coupons are not configured")
		return
	}
	items, err := s.Coupons.List(r.Context())
	writeResult(w, struct {
		Items []order.Coupon `json:"items"`
	}{items}, err)
}

func (s *Server) createCoupon(w http.ResponseWriter, r *http.Request) {
	if !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	if s.Coupons == nil {
		respondError(w, 503, "coupons are not configured")
		return
	}
	var coupon order.Coupon
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&coupon) != nil || decoder.Decode(new(any)) != io.EOF {
		respondError(w, 400, "invalid JSON")
		return
	}
	if err := s.Coupons.Create(r.Context(), coupon); err != nil {
		writeResult(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) orderNamespace(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := identity(r.Context())
	requested := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if requested == "" {
		requested = user.Namespace
	}
	if !user.Founder && requested != user.Namespace {
		respondError(w, 403, "founder required")
		return "", false
	}
	if !validName(requested) || (requested != user.Namespace && (!strings.HasPrefix(requested, "csm-") || len(requested) <= 4)) {
		respondError(w, 400, "invalid target account")
		return "", false
	}
	if user.Founder && requested != user.Namespace {
		var target any
		if err := s.kubeNamespaces(r.Context(), requested, &target); err != nil {
			writeResult(w, nil, err)
			return "", false
		}
	}
	return requested, true
}

func validName(s string) bool {
	return s != "" && len(s) <= 63 && len(validation.IsDNS1123Subdomain(s)) == 0
}

func (s *Server) list(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ns, ok := namespace(w, r)
		if !ok {
			return
		}
		if kind == "system-templates" {
			ns = ""
		}
		if kind == "csms" && identity(r.Context()).Founder && r.URL.Query().Get("namespace") == "" {
			ns = ""
		}
		data, err := s.Backend.List(r.Context(), kind, ns)
		if err == nil && kind == "preinstall-applications" && !identity(r.Context()).Founder {
			data, err = redactInitializationConfigs(data)
		}
		writeResult(w, data, err)
	}
}
func (s *Server) get(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ns, ok := namespace(w, r)
		if !ok {
			return
		}
		if kind == "system-templates" {
			ns = ""
		}
		name := r.PathValue("name")
		if !validName(name) {
			respondError(w, 400, "invalid name")
			return
		}
		data, err := s.Backend.Get(r.Context(), kind, ns, name)
		if err == nil && kind == "preinstall-applications" && !identity(r.Context()).Founder {
			data, err = redactInitializationConfig(data)
		}
		writeResult(w, data, err)
	}
}

func redactInitializationConfigs(data json.RawMessage) (json.RawMessage, error) {
	var list map[string]any
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	items, ok := list["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid initialization config list")
	}
	for _, value := range items {
		if item, ok := value.(map[string]any); ok {
			redactInitializationConfigObject(item)
		}
	}
	return json.Marshal(list)
}

func redactInitializationConfig(data json.RawMessage) (json.RawMessage, error) {
	var item map[string]any
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	redactInitializationConfigObject(item)
	return json.Marshal(item)
}

func redactInitializationConfigObject(item map[string]any) {
	spec, _ := item["spec"].(map[string]any)
	if spec == nil {
		return
	}
	for _, key := range []string{"shell", "cloudConfig"} {
		if value, ok := spec[key].(string); ok && value != "" {
			if key == "shell" {
				spec["hasShell"] = true
			} else {
				spec["hasCloudConfig"] = true
			}
			delete(spec, key)
		}
	}
}
func (s *Server) action(kind, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ns, ok := namespace(w, r)
		if !ok {
			return
		}
		if kind == "system-templates" || kind == "preinstall-applications" || kind == "upgrade-versions" || kind == "upgrade-policies" {
			if !identity(r.Context()).Founder {
				respondError(w, 403, "founder required")
				return
			}
			ns = ""
		}
		name := r.PathValue("name")
		if name != "" && !validName(name) {
			respondError(w, 400, "invalid name")
			return
		}
		var body json.RawMessage
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if err != nil || (len(body) > 0 && !json.Valid(body)) {
				respondError(w, 400, "invalid JSON")
				return
			}
		}
		data, err := s.Backend.Action(r.Context(), kind, ns, name, action, body)
		writeResult(w, data, err)
	}
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	ns, ok := s.orderNamespace(w, r)
	if !ok {
		return
	}
	var req order.Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	req.Namespace = ns // Never accept a tenant from the request body.
	if req.Action == "buy" {
		req.Name = ""
	}
	if req.FounderFree && !identity(r.Context()).Founder {
		respondError(w, 403, "founder required")
		return
	}
	if err := order.Validate(req); err != nil {
		writeResult(w, nil, err)
		return
	}
	kind := "csms"
	if req.Kind == "dataDisk" {
		kind = "data-disks"
	}
	if req.Action == "buy" {
		if req.Kind == "csm" {
			raw, err := s.Backend.Get(r.Context(), "system-templates", "", req.TemplateName)
			if err != nil {
				writeResult(w, nil, err)
				return
			}
			var template struct {
				Spec struct {
					InitMode string `json:"initMode"`
				} `json:"spec"`
			}
			if json.Unmarshal(raw, &template) != nil || template.Spec.InitMode != "cloudInit" {
				respondError(w, 409, "template is unavailable for new purchases")
				return
			}
		}
		// shortcut: pending orders do not reserve names; add reservation if concurrent collisions become material.
		for attempts := 0; attempts < 10; attempts++ {
			n, err := rand.Int(rand.Reader, big.NewInt(100000))
			if err != nil {
				writeResult(w, nil, err)
				return
			}
			req.Name = fmt.Sprintf("ckm-%05d", n.Int64())
			if _, err := s.Backend.Get(r.Context(), kind, ns, req.Name); errors.Is(err, ErrNotFound) {
				break
			} else if err != nil {
				writeResult(w, nil, err)
				return
			}
			req.Name = ""
		}
		if req.Name == "" {
			respondError(w, 409, "resource name unavailable")
			return
		}
	} else {
		raw, err := s.Backend.Get(r.Context(), kind, ns, req.Name)
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		var existing struct {
			Spec map[string]any `json:"spec"`
		}
		if json.Unmarshal(raw, &existing) != nil || existing.Spec["paid"] != true {
			respondError(w, 409, "resource is not paid")
			return
		}
		if req.Action == "expand" {
			currentStorage := number(existing.Spec["systemDiskGi"])
			if req.Kind == "dataDisk" {
				currentStorage = number(existing.Spec["storageGi"])
				if req.StorageGi <= currentStorage {
					respondError(w, 400, "storageGi must increase")
					return
				}
			} else {
				cpu := number(existing.Spec["cpu"])
				memory := strings.TrimSuffix(strings.TrimSpace(fmt.Sprint(existing.Spec["memory"])), "Gi")
				memoryGi, _ := strconv.ParseInt(memory, 10, 64)
				bandwidth := number(existing.Spec["bandwidthMbps"])
				if req.CPU < cpu || req.MemoryGi < memoryGi || req.StorageGi < currentStorage || req.BandwidthMbps < bandwidth ||
					req.CPU == cpu && req.MemoryGi == memoryGi && req.StorageGi == currentStorage && req.BandwidthMbps == bandwidth {
					respondError(w, 400, "expansion must increase at least one resource without reducing others")
					return
				}
			}
		}
	}
	record, err := s.Orders.Create(r.Context(), req)
	writeResult(w, record, err)
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	ns, ok := namespace(w, r)
	if !ok {
		return
	}
	records, err := s.Orders.Store.List(r.Context(), ns)
	writeResult(w, struct {
		Items []order.Record `json:"items"`
	}{records}, err)
}
func (s *Server) refreshOrder(w http.ResponseWriter, r *http.Request) {
	ns, ok := namespace(w, r)
	if !ok {
		return
	}
	sn := r.PathValue("sn")
	if sn == "" {
		respondError(w, 400, "orderSn required")
		return
	}
	record, err := s.Orders.Refresh(r.Context(), ns, sn)
	writeResult(w, record, err)
}
func (s *Server) notifyOrder(w http.ResponseWriter, r *http.Request) {

	var account, sn string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Account string `json:"k3kName"`
			OrderSn string `json:"orderSn"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil {
			respondError(w, 400, "invalid notification")
			return
		}
		account, sn = req.Account, req.OrderSn
	} else {
		r.Body = io.NopCloser(io.LimitReader(r.Body, 8<<10))
		if r.ParseForm() != nil {
			respondError(w, 400, "invalid notification")
			return
		}
		account, sn = r.Form.Get("k3kName"), r.Form.Get("orderSn")
	}
	account = strings.TrimSpace(account)
	sn = strings.TrimSpace(sn)
	if account == "" || sn == "" {
		respondError(w, 400, "k3kName and orderSn are required")
		return
	}
	namespace := "csm-" + account
	var err error
	if s.ResolveAccount != nil {
		namespace, err = s.ResolveAccount(r.Context(), account)
	}
	if err != nil || !validName(account) || !validName(namespace) {
		respondError(w, 403, "unknown W7 account")
		return
	}
	record, err := s.Orders.Refresh(r.Context(), namespace, sn)
	writeResult(w, record, err)
}
func writeResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		code := 500
		switch {
		case errors.Is(err, order.ErrPricingMissing):
			code = 503
		case errors.Is(err, ErrNotFound), errors.Is(err, order.ErrNotFound):
			code = 404
		case errors.Is(err, ErrForbidden):
			code = 403
		case errors.Is(err, ErrConflict), errors.Is(err, order.ErrBusy), errors.Is(err, order.ErrUnconfirmed):
			code = 409
		case errors.Is(err, order.ErrInvalid):
			code = 400
		}
		respondError(w, code, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if value == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
func respondError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// serveUI serves Vite's output, including index.html for client-side routes.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, Prefix+"/") {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.Abs(s.UIRoot)
	if err != nil {
		respondError(w, 500, "invalid UI path")
		return
	}
	name := filepath.Join(root, filepath.Clean("/"+r.URL.Path))
	if name != root && !strings.HasPrefix(name, root+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(name)
	if err != nil || info.IsDir() {
		name = filepath.Join(root, "index.html")
	}
	http.ServeFile(w, r, name)
}

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, 503, "Kubernetes terminal is not configured")
		return
	}
	base, err := url.Parse(s.KubeURL)
	if err != nil || base.Host == "" {
		respondError(w, 500, "invalid Kubernetes URL")
		return
	}
	client := s.KubeClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	podsURL := *base
	podsURL.Path = "/api/v1/namespaces/" + ns + "/pods"
	podsURL.RawQuery = url.Values{"labelSelector": {"csm.w7.cc/host=" + name}, "fieldSelector": {"status.phase=Running"}}.Encode()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, podsURL.String(), nil)
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.KubeToken)
	response, err := client.Do(req)
	if err != nil {
		respondError(w, 502, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		respondError(w, 502, "cannot find host pod")
		return
	}
	var podList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&podList); err != nil || len(podList.Items) == 0 {
		respondError(w, 404, "running host pod not found")
		return
	}
	pod := podList.Items[0].Metadata.Name
	if !validName(pod) {
		respondError(w, 502, "invalid host pod")
		return
	}
	execURL := *base
	execURL.Path = "/api/v1/namespaces/" + ns + "/pods/" + pod + "/exec"
	execURL.RawQuery = url.Values{"container": {"host"}, "command": {"/bin/sh", "-c", "TERM=xterm; export TERM; exec /bin/sh"}, "stdin": {"true"}, "stdout": {"true"}, "stderr": {"true"}, "tty": {"true"}}.Encode()
	proxy := httputil.NewSingleHostReverseProxy(base)
	if client.Transport != nil {
		proxy.Transport = client.Transport
	}
	proxy.Director = func(out *http.Request) {
		out.URL = &execURL
		out.Host = base.Host
		out.Header.Set("Authorization", "Bearer "+s.KubeToken)
		out.Header.Set("Sec-WebSocket-Protocol", "v4.channel.k8s.io")
		out.Header.Del("Authorization-csm")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { respondError(w, 502, "terminal proxy failed") }
	proxy.ServeHTTP(w, r)
}

func number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case string:
		value, _ := strconv.ParseInt(n, 10, 64)
		return value
	}
	return 0
}
