package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const PricingConfigMap = "w7panel-csm-pricing"

// Prices are integer cents per resource unit per 720-hour month.
type Prices struct {
	CPUCents           int64              `json:"cpuCents"`
	MemoryGiCents      int64              `json:"memoryGiCents"`
	StorageGiCents     int64              `json:"storageGiCents"`
	BandwidthMbpsCents int64              `json:"bandwidthMbpsCents"`
	Combinations       []PriceCombination `json:"combinations,omitempty"`
}

type PriceCombination struct {
	Quantity      int64  `json:"quantity"`
	Unit          string `json:"unit"`
	CPU           int64  `json:"cpu"`
	MemoryGi      int64  `json:"memoryGi"`
	StorageGi     int64  `json:"storageGi"`
	BandwidthMbps int64  `json:"bandwidthMbps"`
	DiscountNew   int64  `json:"discountNew"`
	DiscountRenew int64  `json:"discountRenew"`
}

var ErrPricingMissing = errors.New("pricing is not configured")

type KubePricing struct {
	Kube      kubernetes.Interface
	Store     KubeStore
	Namespace string
	ProductID string
	Now       func() time.Time
}

func (p KubePricing) Get(ctx context.Context) (Prices, error) {
	cm, err := p.Kube.CoreV1().ConfigMaps(p.Namespace).Get(ctx, PricingConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Prices{}, ErrPricingMissing
	}
	if err != nil {
		return Prices{}, err
	}
	var prices Prices
	if err := json.Unmarshal([]byte(cm.Data["prices"]), &prices); err != nil || prices.validate() != nil {
		return Prices{}, fmt.Errorf("stored pricing is invalid")
	}
	return prices, nil
}

func (p KubePricing) Put(ctx context.Context, prices Prices) error {
	if err := prices.validate(); err != nil {
		return err
	}
	data, _ := json.Marshal(prices)
	cm, err := p.Kube.CoreV1().ConfigMaps(p.Namespace).Get(ctx, PricingConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.Kube.CoreV1().ConfigMaps(p.Namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: PricingConfigMap}, Data: map[string]string{"prices": string(data)}}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["prices"] = string(data)
	_, err = p.Kube.CoreV1().ConfigMaps(p.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (p KubePricing) Delete(ctx context.Context) error {
	err := p.Kube.CoreV1().ConfigMaps(p.Namespace).Delete(ctx, PricingConfigMap, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (p Prices) validate() error {
	for _, cents := range []int64{p.CPUCents, p.MemoryGiCents, p.StorageGiCents, p.BandwidthMbpsCents} {
		if cents < 0 || cents > 100_000_000 {
			return ErrInvalid
		}
	}
	if len(p.Combinations) > 100 {
		return ErrInvalid
	}
	for i, combo := range p.Combinations {
		if !validDuration(combo.Quantity, combo.Unit) || combo.CPU < 1 || combo.MemoryGi < 1 || combo.StorageGi < 1 || combo.BandwidthMbps < 1 ||
			combo.CPU > 1_000_000 || combo.MemoryGi > 1_000_000 || combo.StorageGi > 1_000_000 || combo.BandwidthMbps > 1_000_000 ||
			combo.DiscountNew < 1 || combo.DiscountNew > 100 || combo.DiscountRenew < 1 || combo.DiscountRenew > 100 {
			return ErrInvalid
		}
		for _, previous := range p.Combinations[:i] {
			if combo.Quantity == previous.Quantity && combo.Unit == previous.Unit && combo.CPU == previous.CPU && combo.MemoryGi == previous.MemoryGi && combo.StorageGi == previous.StorageGi && combo.BandwidthMbps == previous.BandwidthMbps {
				return ErrInvalid
			}
		}
	}
	return nil
}

func (p KubePricing) Quote(ctx context.Context, r Request) (int64, string, url.Values, error) {
	if err := Validate(r); err != nil {
		return 0, "", nil, err
	}
	prices, err := p.Get(ctx)
	if err != nil {
		return 0, "", nil, err
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	var old Request
	var expiry time.Time
	if r.Action != "buy" {
		old, expiry, err = p.current(ctx, r)
		if err != nil {
			return 0, "", nil, err
		}
	}
	resources := r
	if r.Action == "renew" {
		resources = old
	}
	if r.Action == "expand" {
		resources.CPU -= old.CPU
		resources.MemoryGi -= old.MemoryGi
		resources.StorageGi -= old.StorageGi
		resources.BandwidthMbps -= old.BandwidthMbps
		if resources.CPU < 0 || resources.MemoryGi < 0 || resources.StorageGi < 0 || resources.BandwidthMbps < 0 ||
			resources.CPU+resources.MemoryGi+resources.StorageGi+resources.BandwidthMbps == 0 || !expiry.After(now) {
			return 0, "", nil, ErrInvalid
		}
	}
	unit, err := weightedCents(prices, resources)
	if err != nil {
		return 0, "", nil, err
	}
	hours := int64(720 * r.Quantity)
	if r.Unit == "year" {
		hours = 8640 * r.Quantity
	}
	if r.Action == "expand" {
		hours = int64(math.Ceil(expiry.Sub(now).Hours()))
	}
	if hours < 1 || unit > (math.MaxInt64-719)/hours {
		return 0, "", nil, ErrInvalid
	}
	cents := (unit*hours + 719) / 720
	if unit > 0 && cents == 0 {
		cents = 1
	}
	target := r
	if r.Action == "renew" {
		target = old
	}
	if r.Kind == "csm" && (r.Action == "buy" || r.Action == "renew") {
		for _, combo := range prices.Combinations {
			if combo.Quantity == r.Quantity && combo.Unit == r.Unit && combo.CPU == target.CPU && combo.MemoryGi == target.MemoryGi && combo.StorageGi == target.StorageGi && combo.BandwidthMbps == target.BandwidthMbps {
				discount := combo.DiscountNew
				if r.Action == "renew" {
					discount = combo.DiscountRenew
				}
				cents = cents/100*discount + (cents%100*discount+99)/100
				break
			}
		}
	}
	params := url.Values{
		"hour":      {strconv.FormatInt(hours, 10)},
		"cpu":       {strconv.FormatInt(target.CPU, 10)},
		"memory":    {strconv.FormatInt(target.MemoryGi, 10)},
		"storage":   {strconv.FormatInt(target.StorageGi, 10)},
		"bandwidth": {strconv.FormatInt(target.BandwidthMbps, 10)},
	}
	return cents, p.ProductID, params, nil
}

func weightedCents(prices Prices, r Request) (int64, error) {
	var sum int64
	for _, item := range []struct{ count, price int64 }{{r.CPU, prices.CPUCents}, {r.MemoryGi, prices.MemoryGiCents}, {r.StorageGi, prices.StorageGiCents}, {r.BandwidthMbps, prices.BandwidthMbpsCents}} {
		if item.count < 0 || item.count > 1_000_000 || item.price > 0 && item.count > (math.MaxInt64-sum)/item.price {
			return 0, ErrInvalid
		}
		sum += item.count * item.price
	}
	return sum, nil
}

func (p KubePricing) current(ctx context.Context, r Request) (Request, time.Time, error) {
	resource := "csms"
	if r.Kind == "dataDisk" {
		resource = "datadisks"
	}
	endpoint := strings.TrimRight(p.Store.BaseURL, "/") + "/apis/csm.w7.cc/v1alpha1/namespaces/" + r.Namespace + "/" + resource + "/" + r.Name
	var obj struct {
		Spec struct {
			Paid          bool   `json:"paid"`
			CPU           string `json:"cpu"`
			Memory        string `json:"memory"`
			SystemDiskGi  int64  `json:"systemDiskGi"`
			StorageGi     int64  `json:"storageGi"`
			BandwidthMbps int64  `json:"bandwidthMbps"`
			ExpireTime    string `json:"expireTime"`
		} `json:"spec"`
	}
	if err := p.Store.call(ctx, http.MethodGet, endpoint, nil, &obj); err != nil {
		return Request{}, time.Time{}, err
	}
	if !obj.Spec.Paid {
		return Request{}, time.Time{}, ErrInvalid
	}
	old := Request{StorageGi: obj.Spec.StorageGi}
	if r.Kind == "csm" {
		old.CPU, _ = strconv.ParseInt(obj.Spec.CPU, 10, 64)
		old.MemoryGi, _ = strconv.ParseInt(strings.TrimSuffix(obj.Spec.Memory, "Gi"), 10, 64)
		old.StorageGi = obj.Spec.SystemDiskGi
		old.BandwidthMbps = obj.Spec.BandwidthMbps
	}
	if old.StorageGi < 1 || r.Kind == "csm" && (old.CPU < 1 || old.MemoryGi < 1 || old.BandwidthMbps < 1) {
		return Request{}, time.Time{}, ErrInvalid
	}
	expiry, err := time.Parse(time.RFC3339, obj.Spec.ExpireTime)
	if err != nil {
		return Request{}, time.Time{}, ErrInvalid
	}
	return old, expiry, nil
}
