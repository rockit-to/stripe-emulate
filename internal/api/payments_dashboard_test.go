package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// What a dashboard's payments list reads: the card of a payment, a declined card, a refund of a charge.
func TestPaymentsAsTheDashboardListsThem(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[map[string]any](t, handler, "/v1/customers", url.Values{"name": {"Sofia Maes"}})
	paid := postForm[map[string]any](t, handler, "/v1/payment_intents", url.Values{
		"amount": {"18500"}, "currency": {"eur"}, "customer": {customer["id"].(string)},
		"payment_method": {"pm_card_mastercard"}, "confirm": {"true"}, "payment_method_types[]": {"card"},
		"description": {"Booking FLT-4823 · Antwerp studio"},
	})
	if paid["status"] != "succeeded" || paid["description"] != "Booking FLT-4823 · Antwerp studio" {
		t.Fatalf("mastercard payment = %v", paid["status"])
	}

	code, body := postFormStatus(t, handler, "/v1/payment_intents", url.Values{
		"amount": {"12000"}, "currency": {"eur"}, "payment_method": {"pm_card_chargeDeclined"}, "confirm": {"true"}, "payment_method_types[]": {"card"},
	})
	if code != http.StatusPaymentRequired || !strings.Contains(body, "card_declined") {
		t.Fatalf("declined card = %d %s, want 402 card_declined", code, body)
	}

	// Sending a receipt: the PaymentIntent's receipt_email.
	sent := postForm[map[string]any](t, handler, "/v1/payment_intents/"+paid["id"].(string), url.Values{"receipt_email": {"jenny.rosen@example.com"}})
	if sent["receipt_email"] != "jenny.rosen@example.com" || sent["description"] != "Booking FLT-4823 · Antwerp studio" {
		t.Fatalf("after receipt_email = %v / %v", sent["receipt_email"], sent["description"])
	}

	charge := paid["latest_charge"].(string)
	refund := postForm[map[string]any](t, handler, "/v1/refunds", url.Values{"charge": {charge}, "amount": {"9500"}})
	if refund["currency"] != "eur" || refund["payment_intent"] != paid["id"] {
		t.Fatalf("refund = %v %v, want eur for %v", refund["currency"], refund["payment_intent"], paid["id"])
	}

	intents := getJSON[map[string]any](t, handler, "/v1/payment_intents?expand[]=data.customer&expand[]=data.payment_method&expand[]=data.latest_charge")["data"].([]any)
	var listed map[string]any
	for _, raw := range intents {
		if raw.(map[string]any)["id"] == paid["id"] {
			listed = raw.(map[string]any)
		}
	}
	card := listed["payment_method"].(map[string]any)["card"].(map[string]any)
	if card["brand"] != "mastercard" || card["last4"] != "4444" {
		t.Fatalf("payment method card = %v", card)
	}
	latest := listed["latest_charge"].(map[string]any)
	details := latest["payment_method_details"].(map[string]any)["card"].(map[string]any)
	if details["last4"] != "4444" || latest["amount_refunded"].(float64) != 9500 {
		t.Fatalf("latest charge = %v refunded %v", details, latest["amount_refunded"])
	}
	if listed["customer"].(map[string]any)["name"] != "Sofia Maes" {
		t.Fatalf("customer = %v", listed["customer"])
	}
	for _, raw := range getJSON[map[string]any](t, handler, "/v1/balance_transactions?type=refund")["data"].([]any) {
		txn := raw.(map[string]any)
		if txn["currency"] != "eur" || txn["description"] != "REFUND FOR CHARGE (Booking FLT-4823 · Antwerp studio)" {
			t.Fatalf("refund balance transaction = %v %v", txn["currency"], txn["description"])
		}
	}
	expanded := getJSON[map[string]any](t, handler, "/v1/balance_transactions?type=refund&expand[]=data.source")["data"].([]any)[0].(map[string]any)
	if expanded["source"].(map[string]any)["charge"] != charge {
		t.Fatalf("refund transaction source = %v, want the refund of %s", expanded["source"], charge)
	}
	chargeTxn := getJSON[map[string]any](t, handler, "/v1/balance_transactions?type=charge")["data"].([]any)[0].(map[string]any)
	if chargeTxn["description"] != "Booking FLT-4823 · Antwerp studio" {
		t.Fatalf("charge balance transaction description = %v", chargeTxn["description"])
	}
}
