package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

// The balance, its history and the list of charges: views of what the emulator already keeps.
// A charge is its PaymentIntent's (paymentIntentChargeID), with the balance transaction stripeBalanceTransaction
// gives it; a refund and a payout each take their amount out again. Every transaction is available at once, as
// stripeBalanceTransaction has it, so the whole balance is available and nothing is pending.

// handleCharges serves GET /v1/charges: the charges of the PaymentIntents that made one, newest first.
func (h *Handler) handleCharges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	intents, err := h.billing.ListPaymentIntents(r.Context())
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	query := r.URL.Query()
	customer, intentID := query.Get("customer"), query.Get("payment_intent")
	data := []map[string]any{}
	for _, intent := range newestIntentsFirst(intents) {
		if paymentIntentChargeID(intent) == "" || (customer != "" && intent.CustomerID != customer) || (intentID != "" && intent.ID != intentID) {
			continue
		}
		refunds, err := h.billing.ListRefunds(r.Context(), billing.RefundFilter{ChargeID: paymentIntentChargeID(intent)})
		if err != nil {
			writeResult(w, nil, err)
			return
		}
		data = append(data, stripeCharge(intent, refunds))
	}
	writeJSON(w, http.StatusOK, stripeListFromRequest(r, data))
}

// handleBalance serves GET /v1/balance: per currency, the sum of the balance transactions.
func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/balance" {
		h.notFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	transactions, err := h.balanceTransactions(r)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	totals := map[string]int64{}
	for _, txn := range transactions {
		totals[txn["currency"].(string)] += txn["net"].(int64)
	}
	if len(totals) == 0 {
		totals["usd"] = 0 // the platform account's default currency (handlePlatformAccount)
	}
	currencies := make([]string, 0, len(totals))
	for currency := range totals {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	available, pending := []map[string]any{}, []map[string]any{}
	for _, currency := range currencies {
		available = append(available, map[string]any{"amount": totals[currency], "currency": currency, "source_types": map[string]any{"card": totals[currency]}})
		pending = append(pending, map[string]any{"amount": int64(0), "currency": currency, "source_types": map[string]any{"card": int64(0)}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "balance", "available": available, "pending": pending, "livemode": false})
}

// handleBalanceTransactions serves GET /v1/balance_transactions, newest first, filtered by type, source or currency.
func (h *Handler) handleBalanceTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	transactions, err := h.balanceTransactions(r)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	query := r.URL.Query()
	data := []map[string]any{}
	for _, txn := range transactions {
		if (query.Get("type") != "" && txn["type"] != query.Get("type")) ||
			(query.Get("source") != "" && txn["source"] != query.Get("source")) ||
			(query.Get("currency") != "" && txn["currency"] != strings.ToLower(query.Get("currency"))) {
			continue
		}
		data = append(data, txn)
	}
	writeJSON(w, http.StatusOK, stripeListFromRequest(r, data))
}

// handleBalanceTransaction serves GET /v1/balance_transactions/{id}.
func (h *Handler) handleBalanceTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, r, "GET")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/balance_transactions/"), "/")
	transactions, err := h.balanceTransactions(r)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	for _, txn := range transactions {
		if txn["id"] == id {
			writeJSON(w, http.StatusOK, txn)
			return
		}
	}
	writeResult(w, nil, billing.ErrNotFound)
}

// balanceTransactions: a charge for each succeeded PaymentIntent, a refund for each refund that went through,
// a payout for each payout of this account (the request's Stripe-Account) that did not fail; newest first.
func (h *Handler) balanceTransactions(r *http.Request) ([]map[string]any, error) {
	intents, err := h.billing.ListPaymentIntents(r.Context())
	if err != nil {
		return nil, err
	}
	refunds, err := h.billing.ListRefunds(r.Context(), billing.RefundFilter{})
	if err != nil {
		return nil, err
	}
	payouts, err := h.billing.ListConnectResources(r.Context(), billing.ConnectResourceFilter{Object: billing.ObjectPayout})
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, intent := range intents {
		if intent.Status == "succeeded" {
			out = append(out, stripeBalanceTransaction(intent))
		}
	}
	for _, refund := range refunds {
		if refund.Status != "failed" && refund.Status != "canceled" {
			out = append(out, outgoingTransaction("txn_"+refund.ID, refund.ID, "refund", "refund", refund.Amount, refund.Currency, unix(refund.CreatedAt)))
		}
	}
	account := r.Header.Get("Stripe-Account")
	for _, payout := range payouts {
		if payout.AccountID != account || payout.Status == "failed" || payout.Status == "canceled" {
			continue
		}
		out = append(out, outgoingTransaction("txn_"+payout.ID, payout.ID, "payout", "payout", payout.Amount, strings.ToLower(payout.Currency), unix(payout.CreatedAt)))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i]["created"] != out[j]["created"] {
			return out[i]["created"].(int64) > out[j]["created"].(int64)
		}
		return out[i]["id"].(string) > out[j]["id"].(string)
	})
	return out, nil
}

// outgoingTransaction: money leaving the balance (a refund, a payout), without a fee.
func outgoingTransaction(id, source, kind, category string, amount int64, currency string, created int64) map[string]any {
	return map[string]any{
		"id":                 id,
		"object":             "balance_transaction",
		"amount":             -amount,
		"available_on":       created,
		"created":            created,
		"currency":           currency,
		"description":        nil,
		"exchange_rate":      nil,
		"fee":                int64(0),
		"fee_details":        []map[string]any{},
		"net":                -amount,
		"reporting_category": category,
		"source":             source,
		"status":             "available",
		"type":               kind,
	}
}

func newestIntentsFirst(intents []billing.PaymentIntent) []billing.PaymentIntent {
	sorted := append([]billing.PaymentIntent(nil), intents...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].ID > sorted[j].ID
	})
	return sorted
}
