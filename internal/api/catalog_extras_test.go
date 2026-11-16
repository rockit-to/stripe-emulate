package api

import (
	"net/url"
	"strings"
	"testing"
)

func TestShippingRatesFeaturesAndTaxTypes(t *testing.T) {
	handler := newTestHandler(t)

	rate := postForm[map[string]any](t, handler, "/v1/shipping_rates", url.Values{
		"display_name": {"Standard delivery"}, "type": {"fixed_amount"}, "fixed_amount[amount]": {"500"}, "fixed_amount[currency]": {"eur"},
	})
	if !strings.HasPrefix(rate["id"].(string), "shr_") || rate["active"] != true || rate["fixed_amount"].(map[string]any)["amount"].(float64) != 500 {
		t.Fatalf("shipping rate = %#v", rate)
	}
	postForm[map[string]any](t, handler, "/v1/shipping_rates/"+rate["id"].(string), url.Values{"active": {"false"}})
	if active := getJSON[map[string]any](t, handler, "/v1/shipping_rates?active=true")["data"].([]any); len(active) != 0 {
		t.Fatalf("active shipping rates after archiving = %d, want 0", len(active))
	}

	feature := postForm[map[string]any](t, handler, "/v1/entitlements/features", url.Values{"name": {"Free breakfast"}, "lookup_key": {"free-breakfast"}})
	if !strings.HasPrefix(feature["id"].(string), "feat_") || feature["object"] != "entitlements.feature" || feature["lookup_key"] != "free-breakfast" {
		t.Fatalf("feature = %#v", feature)
	}
	postForm[map[string]any](t, handler, "/v1/entitlements/features/"+feature["id"].(string), url.Values{"active": {"false"}})
	if archived := getJSON[map[string]any](t, handler, "/v1/entitlements/features?archived=true")["data"].([]any); len(archived) != 1 {
		t.Fatalf("archived features = %d, want 1", len(archived))
	}

	tax := postForm[map[string]any](t, handler, "/v1/tax_rates", url.Values{
		"display_name": {"VAT"}, "percentage": {"21"}, "inclusive": {"false"}, "country": {"BE"}, "tax_type": {"vat"},
	})
	if tax["tax_type"] != "vat" {
		t.Fatalf("tax rate tax_type = %v, want vat", tax["tax_type"])
	}
}
