package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A shop's checkout as Stripe's docs give it: a payment session without a customer, paid on the hosted page,
// lands as a succeeded payment with its charge and balance transaction, in the run of the key that made it.
func TestGuestPaymentCheckoutPaysIntoItsRun(t *testing.T) {
	srv := newKeyedServer(t)
	key := RunKey(testKeySecret, "team-a")
	body := "mode=payment&customer_email=guest@example.test&success_url=https://shop.example/ok&cancel_url=https://shop.example/no" +
		"&line_items[0][quantity]=1&line_items[0][price_data][currency]=eur" +
		"&line_items[0][price_data][unit_amount]=12000&line_items[0][price_data][product_data][name]=Ghent canal house"
	rec := keyed(t, srv, http.MethodPost, "/v1/checkout/sessions", key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create guest checkout session: %d %s", rec.Code, rec.Body.String())
	}
	var session struct {
		ID               string  `json:"id"`
		Customer         *string `json:"customer"`
		CustomerCreation string  `json:"customer_creation"`
		CustomerEmail    string  `json:"customer_email"`
		AmountTotal      int64   `json:"amount_total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.Customer != nil || session.CustomerCreation != "if_required" || session.CustomerEmail != "guest@example.test" || session.AmountTotal != 12000 {
		t.Fatalf("guest session: customer %v, customer_creation %q, amount_total %d", session.Customer, session.CustomerCreation, session.AmountTotal)
	}

	if rec := keyed(t, srv, http.MethodPost, "/v1/checkout/sessions/"+session.ID+"/complete", key, ""); rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	rec = keyed(t, srv, http.MethodGet, "/v1/checkout/sessions/"+session.ID, key, "")
	var paid struct {
		Status        string `json:"status"`
		PaymentStatus string `json:"payment_status"`
		PaymentIntent string `json:"payment_intent"`
		Details       struct {
			Email string `json:"email"`
		} `json:"customer_details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &paid); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if paid.Status != "complete" || paid.PaymentStatus != "paid" || paid.PaymentIntent == "" || paid.Details.Email != "guest@example.test" {
		t.Fatalf("after paying: %s", rec.Body.String())
	}

	rec = keyed(t, srv, http.MethodGet, "/v1/payment_intents/"+paid.PaymentIntent, key, "")
	var intent struct {
		Status       string `json:"status"`
		Amount       int64  `json:"amount"`
		LatestCharge string `json:"latest_charge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &intent); err != nil {
		t.Fatalf("decode intent: %v", err)
	}
	if intent.Status != "succeeded" || intent.Amount != 12000 || !strings.HasPrefix(intent.LatestCharge, "ch_") {
		t.Fatalf("payment intent: %s", rec.Body.String())
	}
	if rec := keyed(t, srv, http.MethodGet, "/v1/payment_intents/"+paid.PaymentIntent, RunKey(testKeySecret, "team-b"), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("another team reads the payment: %d", rec.Code)
	}
}
