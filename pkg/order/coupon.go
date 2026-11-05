package order

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const CouponConfigMap = "w7panel-csm-coupons"

type Coupon struct {
	Code      string     `json:"code"`
	Kind      string     `json:"kind"`
	Action    string     `json:"action"`
	Discount  int64      `json:"discount"`
	ExpiresAt time.Time  `json:"expiresAt"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	Status    string     `json:"status"`
	OrderSn   string     `json:"orderSn,omitempty"`
}

type KubeCoupons struct {
	Kube      kubernetes.Interface
	Namespace string
	Now       func() time.Time
}

func validCouponCode(code string) bool {
	if len(code) < 4 || len(code) > 32 {
		return false
	}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			if c < '0' || c > '9' {
				if c != '-' {
					return false
				}
			}
		}
	}
	return true
}

func (s KubeCoupons) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s KubeCoupons) load(ctx context.Context) (*corev1.ConfigMap, error) {
	cm, err := s.Kube.CoreV1().ConfigMaps(s.Namespace).Get(ctx, CouponConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: CouponConfigMap}, Data: map[string]string{}}, nil
	}
	return cm, err
}

func (s KubeCoupons) get(ctx context.Context, code string) (*corev1.ConfigMap, Coupon, error) {
	cm, err := s.load(ctx)
	if err != nil {
		return nil, Coupon{}, err
	}
	var coupon Coupon
	if json.Unmarshal([]byte(cm.Data[code]), &coupon) != nil || coupon.Code != code {
		return cm, Coupon{}, ErrInvalid
	}
	return cm, coupon, nil
}

func (s KubeCoupons) List(ctx context.Context) ([]Coupon, error) {
	cm, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Coupon, 0, len(cm.Data))
	for code, raw := range cm.Data {
		var item Coupon
		if json.Unmarshal([]byte(raw), &item) != nil || item.Code != code {
			return nil, fmt.Errorf("stored coupon is invalid")
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == nil || items[j].CreatedAt == nil {
			if items[i].CreatedAt == nil && items[j].CreatedAt == nil {
				return items[i].Code > items[j].Code
			}
			return items[i].CreatedAt != nil
		}
		if items[i].CreatedAt.Equal(*items[j].CreatedAt) {
			return items[i].Code > items[j].Code
		}
		return items[i].CreatedAt.After(*items[j].CreatedAt)
	})
	return items, nil
}

func (s KubeCoupons) Create(ctx context.Context, item Coupon) error {
	item.Code = strings.ToUpper(strings.TrimSpace(item.Code))
	if !validCouponCode(item.Code) || (item.Kind != "csm" && item.Kind != "dataDisk") || (item.Action != "buy" && item.Action != "renew") || item.Discount < 1 || item.Discount > 99 || !item.ExpiresAt.After(s.now()) {
		return ErrInvalid
	}
	item.Status = "available"
	item.OrderSn = ""
	now := s.now()
	item.CreatedAt = &now
	for attempt := 0; attempt < 3; attempt++ {
		cm, err := s.load(ctx)
		if err != nil {
			return err
		}
		if _, exists := cm.Data[item.Code]; exists {
			return ErrInvalid
		}
		if len(cm.Data) >= 500 {
			return ErrInvalid
		}
		data, _ := json.Marshal(item)
		cm.Data[item.Code] = string(data)
		if cm.ResourceVersion == "" {
			_, err = s.Kube.CoreV1().ConfigMaps(s.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		} else {
			_, err = s.Kube.CoreV1().ConfigMaps(s.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
		}
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return errors.New("coupon update conflict")
}

// Generate creates a batch in one ConfigMap update so a failed request cannot leave a partial batch.
func (s KubeCoupons) Generate(ctx context.Context, item Coupon, count int) ([]string, error) {
	if count < 1 || count > 100 || (item.Kind != "csm" && item.Kind != "dataDisk") || (item.Action != "buy" && item.Action != "renew") || item.Discount < 1 || item.Discount > 99 || !item.ExpiresAt.After(s.now()) {
		return nil, ErrInvalid
	}
	item.Status, item.OrderSn = "available", ""
	now := s.now()
	item.CreatedAt = &now
	for attempt := 0; attempt < 3; attempt++ {
		cm, err := s.load(ctx)
		if err != nil {
			return nil, err
		}
		if len(cm.Data)+count > 500 {
			return nil, ErrInvalid
		}
		codes := make([]string, 0, count)
		for len(codes) < count {
			const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
			code := "CSM-"
			for i := 0; i < 8; i++ {
				n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
				if err != nil {
					return nil, err
				}
				code += string(alphabet[n.Int64()])
			}
			if _, exists := cm.Data[code]; exists {
				continue
			}
			item.Code = code
			data, _ := json.Marshal(item)
			cm.Data[code] = string(data)
			codes = append(codes, code)
		}
		if cm.ResourceVersion == "" {
			_, err = s.Kube.CoreV1().ConfigMaps(s.Namespace).Create(ctx, cm, metav1.CreateOptions{})
		} else {
			_, err = s.Kube.CoreV1().ConfigMaps(s.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
		}
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			continue
		}
		return codes, err
	}
	return nil, errors.New("coupon update conflict")
}

func (s KubeCoupons) Check(ctx context.Context, r Request) (Coupon, error) {
	if !validCouponCode(r.CouponCode) {
		return Coupon{}, ErrInvalid
	}
	_, item, err := s.get(ctx, r.CouponCode)
	if err != nil {
		return Coupon{}, err
	}
	if item.Status != "available" || item.Kind != r.Kind || item.Action != r.Action || !item.ExpiresAt.After(s.now()) {
		return Coupon{}, ErrInvalid
	}
	return item, nil
}

func (s KubeCoupons) change(ctx context.Context, code, sn, from, to string, r *Request) error {
	for attempt := 0; attempt < 3; attempt++ {
		cm, item, err := s.get(ctx, code)
		if err != nil {
			return err
		}
		if to == "used" && item.Status == "used" && item.OrderSn == sn {
			return nil
		}
		if item.Status != from || (from != "available" && item.OrderSn != sn) || !item.ExpiresAt.After(s.now()) {
			return ErrInvalid
		}
		if r != nil && (item.Kind != r.Kind || item.Action != r.Action) {
			return ErrInvalid
		}
		item.Status, item.OrderSn = to, sn
		if to == "available" {
			item.OrderSn = ""
		}
		data, _ := json.Marshal(item)
		cm.Data[code] = string(data)
		_, err = s.Kube.CoreV1().ConfigMaps(s.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return errors.New("coupon update conflict")
}

func (s KubeCoupons) Lock(ctx context.Context, r Request, sn string) error {
	return s.change(ctx, r.CouponCode, sn, "available", "locked", &r)
}
func (s KubeCoupons) Use(ctx context.Context, code, sn string) error {
	return s.change(ctx, code, sn, "locked", "used", nil)
}
func (s KubeCoupons) Unlock(ctx context.Context, code, sn string) error {
	return s.change(ctx, code, sn, "locked", "available", nil)
}
