package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
)

// Request contains only product choices. The payment gateway calculates the price.
type Request struct {
	Kind          string `json:"kind"`   // csm or dataDisk
	Action        string `json:"action"` // buy, renew or expand
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	TemplateName  string `json:"templateName,omitempty"`
	CPU           int64  `json:"cpu,omitempty"`
	MemoryGi      int64  `json:"memoryGi,omitempty"`
	StorageGi     int64  `json:"storageGi,omitempty"`
	BandwidthMbps int64  `json:"bandwidthMbps,omitempty"`
	Quantity      int64  `json:"quantity,omitempty"`
	Unit          string `json:"unit,omitempty"`
	CouponCode    string `json:"couponCode,omitempty"`
	FounderFree   bool   `json:"founderFree,omitempty"`
}

type Payment struct {
	OrderSn     string     `json:"orderSn"`
	Ticket      string     `json:"ticket"`
	NeedPay     bool       `json:"needPay"`
	AmountCents int64      `json:"amountCents"`
	Expected    CloudTerms `json:"expected"`
}

type CloudTerms struct {
	Hour, CPU, Memory, Storage, Bandwidth int64
}

type Record struct {
	Request
	Payment
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Gateway must use W7 Cloud's authenticated order lookup. Callback payloads are hints only.
type Gateway interface {
	Create(context.Context, Request) (Payment, error)
	Lookup(context.Context, Record) (PaymentStatus, error)
}

type PaymentStatus struct {
	OrderSn   string
	Namespace string
	Name      string
	Action    string
	Paid      bool
	CloudTerms
}

// Store.ApplyPaid must serialize callback processing and apply the resource
// change idempotently. A duplicate call returns the existing paid record.
type Store interface {
	SavePending(context.Context, Record) error
	Get(context.Context, string, string) (Record, error)
	List(context.Context, string) ([]Record, error)
	ApplyPaid(context.Context, Record) (Record, error)
}

type Service struct {
	Gateway Gateway
	Store   Store
	Coupons interface {
		Lock(context.Context, Request, string) error
		Use(context.Context, string, string) error
		Unlock(context.Context, string, string) error
	}
	AutoConfirm bool
}

var ErrInvalid = errors.New("invalid order")
var ErrUnconfirmed = errors.New("payment is not confirmed")

func (s Service) Create(ctx context.Context, req Request) (Record, error) {
	if s.Gateway == nil || s.Store == nil {
		return Record{}, errors.New("order service is not configured")
	}
	if err := Validate(req); err != nil {
		return Record{}, err
	}
	payment, err := s.Gateway.Create(ctx, req)
	if err != nil {
		return Record{}, err
	}
	if payment.OrderSn == "" || payment.AmountCents < 0 {
		return Record{}, fmt.Errorf("%w: invalid gateway response", ErrInvalid)
	}
	if req.CouponCode != "" {
		if s.Coupons == nil {
			return Record{}, errors.New("coupons are not configured")
		}
		if err := s.Coupons.Lock(ctx, req, payment.OrderSn); err != nil {
			return Record{}, err
		}
	}
	record := Record{Request: req, Payment: payment, Status: "pending", CreatedAt: time.Now().UTC()}
	if err := s.Store.SavePending(ctx, record); err != nil {
		if req.CouponCode != "" {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Coupons.Unlock(releaseCtx, req.CouponCode, payment.OrderSn)
		}
		return Record{}, err
	}
	if req.CouponCode != "" {
		if err := s.Coupons.Use(ctx, req.CouponCode, payment.OrderSn); err != nil {
			// shortcut: a saved order keeps its coupon locked if final marking fails; retry on payment refresh.
			return record, nil
		}
	}
	if s.AutoConfirm || !payment.NeedPay && payment.AmountCents == 0 {
		confirmed, err := s.Refresh(ctx, req.Namespace, payment.OrderSn)
		if errors.Is(err, ErrUnconfirmed) {
			if !payment.NeedPay {
				// shortcut: free orders get one delayed confirmation; a restart relies on the callback or manual refresh.
				time.AfterFunc(2*time.Second, func() {
					checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_, _ = s.Refresh(checkCtx, req.Namespace, payment.OrderSn)
				})
			}
			return record, nil
		}
		return confirmed, err
	}
	return record, nil
}

func Validate(r Request) error {
	if r.FounderFree && r.CouponCode != "" {
		return ErrInvalid
	}
	if r.CouponCode != "" && !validCouponCode(r.CouponCode) {
		return ErrInvalid
	}
	if r.Namespace == "" || r.Name == "" || !dnsName(r.Name) || !dnsName(r.Namespace) {
		return ErrInvalid
	}
	if r.Kind != "csm" && r.Kind != "dataDisk" {
		return ErrInvalid
	}
	switch r.Action {
	case "buy":
		if !validDuration(r.Quantity, r.Unit) {
			return ErrInvalid
		}
		if r.Kind == "csm" && (r.TemplateName == "" || len(r.TemplateName) > 63 || len(validation.IsDNS1123Subdomain(r.TemplateName)) != 0 || r.CPU < 1 || r.MemoryGi < 1 || r.StorageGi < 1 || r.BandwidthMbps < 1) {
			return ErrInvalid
		}
		if r.Kind == "dataDisk" && (r.StorageGi < 1 || r.TemplateName != "" || r.CPU != 0 || r.MemoryGi != 0 || r.BandwidthMbps != 0) {
			return ErrInvalid
		}
	case "renew":
		if !validDuration(r.Quantity, r.Unit) {
			return ErrInvalid
		}
		if r.CPU != 0 || r.MemoryGi != 0 || r.StorageGi != 0 || r.BandwidthMbps != 0 || r.TemplateName != "" {
			return ErrInvalid
		}
	case "expand":
		if r.Quantity != 0 || r.Unit != "" || r.TemplateName != "" {
			return ErrInvalid
		}
		if r.Kind == "csm" && (r.CPU < 1 || r.MemoryGi < 1 || r.StorageGi < 1 || r.BandwidthMbps < 1) {
			return ErrInvalid
		}
		if r.Kind == "dataDisk" && (r.StorageGi < 1 || r.CPU != 0 || r.MemoryGi != 0 || r.BandwidthMbps != 0) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func dnsName(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// Refresh is shared by the user refresh route and public payment callback.
func (s Service) Refresh(ctx context.Context, namespace, sn string) (Record, error) {
	if s.Gateway == nil || s.Store == nil || namespace == "" || sn == "" {
		return Record{}, ErrInvalid
	}
	record, err := s.Store.Get(ctx, namespace, sn)
	if err != nil {
		return Record{}, err
	}
	if record.Status == "paid" {
		return record, nil
	}
	status, err := s.Gateway.Lookup(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if status.OrderSn != sn || status.Namespace != namespace || status.Name != record.Name || status.Action != record.Action || status.CloudTerms != record.Expected {
		return Record{}, fmt.Errorf("%w: gateway order mismatch", ErrInvalid)
	}
	if !status.Paid {
		return record, ErrUnconfirmed
	}
	if record.CouponCode != "" && s.Coupons != nil {
		_ = s.Coupons.Use(ctx, record.CouponCode, record.OrderSn)
	}
	return s.Store.ApplyPaid(ctx, record)
}

func validDuration(quantity int64, unit string) bool {
	return (unit == "day" && quantity >= 1 && quantity <= 3650) || (unit == "month" && quantity >= 1 && quantity <= 120) || (unit == "year" && quantity >= 1 && quantity <= 10)
}
