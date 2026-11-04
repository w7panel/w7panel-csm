package order

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// MockGateway is only used when MOCK_PAY=true. It never calls W7 Cloud.
type MockGateway struct {
	Quote func(context.Context, Request) (int64, string, url.Values, error)
}

func (g MockGateway) Create(ctx context.Context, r Request) (Payment, error) {
	if g.Quote == nil {
		return Payment{}, ErrPricingMissing
	}
	cents, _, params, err := g.Quote(ctx, r)
	if err != nil {
		return Payment{}, err
	}
	if cents < 0 {
		return Payment{}, ErrInvalid
	}
	if r.FounderFree {
		cents = 0
	}
	terms, err := termsFromParams(params)
	if err != nil {
		return Payment{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Payment{}, err
	}
	return Payment{OrderSn: fmt.Sprintf("csm-mock-%x", id), AmountCents: cents, Expected: terms}, nil
}

func (MockGateway) Lookup(_ context.Context, r Record) (PaymentStatus, error) {
	id, ok := strings.CutPrefix(r.OrderSn, "csm-mock-")
	if !ok || len(id) != 32 || r.NeedPay || r.Ticket != "" {
		return PaymentStatus{}, ErrInvalid
	}
	if _, err := hex.DecodeString(id); err != nil {
		return PaymentStatus{}, ErrInvalid
	}
	return PaymentStatus{OrderSn: r.OrderSn, Namespace: r.Namespace, Name: r.Name, Action: r.Action, Paid: true, CloudTerms: r.Expected}, nil
}
