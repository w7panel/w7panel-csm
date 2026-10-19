package integration

import (
	"net/url"
	"testing"

	"github.com/w7panel/w7panel-csm/pkg/order"
)

func TestValidateQuoteTermsRejectsOverwrittenSize(t *testing.T) {
	choice := order.Request{Kind: "csm", Action: "buy", Name: "host", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	params := url.Values{"hour": {"720"}, "cpu": {"2"}, "memory": {"4"}, "storage": {"40"}, "bandwidth": {"5"}}
	if err := validateQuoteTerms(choice, params, "42", 1200); err != nil {
		t.Fatal(err)
	}
	params.Set("cpu", "1")
	if err := validateQuoteTerms(choice, params, "42", 1200); err == nil {
		t.Fatal("mismatched CPU quote was accepted")
	}
}
