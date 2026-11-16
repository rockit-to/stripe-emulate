package api

import (
	"net/url"
	"testing"
)

func TestProductDefaultPriceAndActiveFilter(t *testing.T) {
	handler := newTestHandler(t)
	loft := postForm[map[string]any](t, handler, "/v1/products", url.Values{
		"name":                            {"Lisbon loft"},
		"default_price_data[currency]":    {"eur"},
		"default_price_data[unit_amount]": {"42000"},
	})
	priceID, _ := loft["default_price"].(string)
	if priceID == "" {
		t.Fatalf("default_price = %#v, want the created price id", loft["default_price"])
	}
	price := getJSON[map[string]any](t, handler, "/v1/prices/"+priceID)
	if price["product"] != loft["id"] || price["unit_amount"].(float64) != 42000 || price["currency"] != "eur" {
		t.Fatalf("default price = %#v", price)
	}

	old := postForm[map[string]any](t, handler, "/v1/products", url.Values{"name": {"Old product"}, "active": {"false"}})
	// Updating another field leaves an archived product archived.
	renamed := postForm[map[string]any](t, handler, "/v1/products/"+old["id"].(string), url.Values{"description": {"No longer sold"}})
	if renamed["active"] != false {
		t.Fatalf("active after update without active = %#v, want false", renamed["active"])
	}

	active := getJSON[map[string]any](t, handler, "/v1/products?active=true&expand[]=data.default_price")["data"].([]any)
	if len(active) != 1 {
		t.Fatalf("active products = %d, want 1", len(active))
	}
	expanded, _ := active[0].(map[string]any)["default_price"].(map[string]any)
	if expanded["id"] != priceID || expanded["unit_amount"].(float64) != 42000 {
		t.Fatalf("expanded default_price = %#v", active[0].(map[string]any)["default_price"])
	}
	if archived := getJSON[map[string]any](t, handler, "/v1/products?active=false")["data"].([]any); len(archived) != 1 {
		t.Fatalf("archived products = %d, want 1", len(archived))
	}

	second := postForm[map[string]any](t, handler, "/v1/prices", url.Values{"product": {loft["id"].(string)}, "currency": {"eur"}, "unit_amount": {"39000"}})
	updated := postForm[map[string]any](t, handler, "/v1/products/"+loft["id"].(string), url.Values{"default_price": {second["id"].(string)}})
	if updated["default_price"] != second["id"] {
		t.Fatalf("default_price after update = %#v, want %s", updated["default_price"], second["id"])
	}
}
