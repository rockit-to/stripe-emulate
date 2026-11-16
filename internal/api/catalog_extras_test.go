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
	newer := postForm[map[string]any](t, handler, "/v1/tax_rates", url.Values{"display_name": {"GST"}, "percentage": {"9"}, "inclusive": {"false"}})
	if first := getJSON[map[string]any](t, handler, "/v1/tax_rates")["data"].([]any)[0].(map[string]any); first["id"] != newer["id"] {
		t.Fatalf("first tax rate = %v, want the newest %v", first["id"], newer["id"])
	}
}

func TestPromotionCodeRestrictions(t *testing.T) {
	handler := newTestHandler(t)
	coupon := postForm[map[string]any](t, handler, "/v1/coupons", url.Values{"percent_off": {"10"}})
	promo := postForm[map[string]any](t, handler, "/v1/promotion_codes", url.Values{
		"coupon": {coupon["id"].(string)}, "code": {"FIRST10"}, "max_redemptions": {"5"}, "expires_at": {"1900000000"},
		"restrictions[first_time_transaction]": {"true"}, "restrictions[minimum_amount]": {"2000"}, "restrictions[minimum_amount_currency]": {"eur"},
	})
	restrictions := promo["restrictions"].(map[string]any)
	if promo["max_redemptions"] != float64(5) || promo["expires_at"] != float64(1900000000) || restrictions["first_time_transaction"] != true || restrictions["minimum_amount"] != float64(2000) {
		t.Fatalf("promotion code = %v", promo)
	}
}

func TestCouponCurrencyOptions(t *testing.T) {
	handler := newTestHandler(t)
	coupon := postForm[map[string]any](t, handler, "/v1/coupons", url.Values{"amount_off": {"500"}, "currency": {"eur"}, "currency_options[aed][amount_off]": {"2000"}})
	if coupon["currency_options"].(map[string]any)["aed"].(map[string]any)["amount_off"] != float64(2000) {
		t.Fatalf("currency_options = %v", coupon["currency_options"])
	}
}

func TestFeatureMetadataUnset(t *testing.T) {
	handler := newTestHandler(t)
	feature := postForm[map[string]any](t, handler, "/v1/entitlements/features", url.Values{"name": {"F"}, "lookup_key": {"f"}, "metadata[tier]": {"gold"}})
	id := feature["id"].(string)
	updated := postForm[map[string]any](t, handler, "/v1/entitlements/features/"+id, url.Values{"metadata[tier]": {""}})
	if _, ok := updated["metadata"].(map[string]any)["tier"]; ok {
		t.Fatalf("metadata = %v, want tier removed", updated["metadata"])
	}
}
