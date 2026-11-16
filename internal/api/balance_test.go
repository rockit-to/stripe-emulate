package api

import (
	"net/url"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

func TestBalanceChargesAndBalanceTransactions(t *testing.T) {
	handler := newTestHandler(t)
	customer := postForm[billing.Customer](t, handler, "/v1/customers", url.Values{"email": {"balance@example.test"}})
	created := postForm[billing.PaymentIntent](t, handler, "/v1/payment_intents", url.Values{
		"amount": {"4900"}, "currency": {"usd"}, "customer": {customer.ID},
	})
	paid := postForm[billing.PaymentIntent](t, handler, "/v1/payment_intents/"+created.ID+"/confirm", url.Values{"payment_method": {"pm_card_visa"}})
	if paid.Status != "succeeded" {
		t.Fatalf("payment intent = %s, want succeeded", paid.Status)
	}
	// A second one that is never paid makes no charge.
	postForm[billing.PaymentIntent](t, handler, "/v1/payment_intents", url.Values{"amount": {"1000"}, "currency": {"usd"}})
	postForm[map[string]any](t, handler, "/v1/refunds", url.Values{"payment_intent": {paid.ID}, "amount": {"900"}})

	charges := getJSON[map[string]any](t, handler, "/v1/charges")
	data := charges["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("charges = %d, want 1 (only the paid intent)", len(data))
	}
	charge := data[0].(map[string]any)
	if charge["id"] != "ch_"+paid.ID || charge["amount_refunded"].(float64) != 900 || charge["refunded"] != false {
		t.Fatalf("charge = %#v", charge)
	}
	single := getJSON[map[string]any](t, handler, "/v1/charges/ch_"+paid.ID)
	if single["amount_refunded"] != charge["amount_refunded"] || single["balance_transaction"] != charge["balance_transaction"] {
		t.Fatalf("GET /v1/charges/{id} differs from the list: %#v vs %#v", single, charge)
	}
	if filtered := getJSON[map[string]any](t, handler, "/v1/charges?customer=cus_other")["data"].([]any); len(filtered) != 0 {
		t.Fatalf("charges of another customer = %d, want 0", len(filtered))
	}

	// 4900 less the simulated fee (2.9% + 30 = 172), less the 900 refunded.
	fee := int64(4900*29/1000 + 30)
	want := 4900 - fee - 900
	balance := getJSON[map[string]any](t, handler, "/v1/balance")
	available := balance["available"].([]any)[0].(map[string]any)
	if available["currency"] != "usd" || int64(available["amount"].(float64)) != want {
		t.Fatalf("balance available = %#v, want %d usd", available, want)
	}

	txns := getJSON[map[string]any](t, handler, "/v1/balance_transactions")["data"].([]any)
	if len(txns) != 2 {
		t.Fatalf("balance transactions = %d, want 2 (charge, refund)", len(txns))
	}
	sum := int64(0)
	kinds := map[string]bool{}
	for _, raw := range txns {
		txn := raw.(map[string]any)
		sum += int64(txn["net"].(float64))
		kinds[txn["type"].(string)] = true
	}
	if sum != want || !kinds["charge"] || !kinds["refund"] {
		t.Fatalf("transactions net %d types %v, want %d with charge and refund", sum, kinds, want)
	}
	refunds := getJSON[map[string]any](t, handler, "/v1/balance_transactions?type=refund")["data"].([]any)
	if len(refunds) != 1 || int64(refunds[0].(map[string]any)["amount"].(float64)) != -900 {
		t.Fatalf("refund transactions = %#v", refunds)
	}
	one := getJSON[map[string]any](t, handler, "/v1/balance_transactions/txn_"+paid.ID)
	if one["type"] != "charge" || one["source"] != "ch_"+paid.ID {
		t.Fatalf("balance transaction = %#v", one)
	}
}

func TestBalanceWithNothingYet(t *testing.T) {
	handler := newTestHandler(t)
	balance := getJSON[map[string]any](t, handler, "/v1/balance")
	available := balance["available"].([]any)
	if len(available) != 1 || available[0].(map[string]any)["amount"].(float64) != 0 {
		t.Fatalf("empty balance = %#v", balance)
	}
	if status, _ := getStatus(t, handler, "/v1/balance_transactions/txn_missing"); status != 404 {
		t.Fatalf("missing balance transaction status = %d, want 404", status)
	}
}
