package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

// The balance, its history and the list of charges: views of what the emulator already keeps.
// A charge is its PaymentIntent's (paymentIntentChargeID), with the balance transaction stripeBalanceTransaction
// gives it; a refund and a payout each take their amount out again, a top-up adds its amount.
// A charge and a refund settle after settlementDelay, as in Stripe's sandbox (pending until then); a top-up and a
// payout count at once. The balance is the sum of the transactions, available or pending by that, per source type.

// settlementDelay: how long a charge or a refund stays pending, as a new Stripe sandbox account has it.
const settlementDelay = 7 * 24 * time.Hour

func settlementStatus(availableOn time.Time) string {
	if time.Now().Before(availableOn) {
		return "pending"
	}
	return "available"
}

// sourceType: the source_types entry of the balance a transaction counts in (a top-up's money came from a bank).
func sourceType(txn map[string]any) string {
	if txn["type"] == "topup" {
		return "bank_account"
	}
	return "card"
}

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
	// per status (available, pending), per currency, per source type
	sums := map[string]map[string]map[string]int64{"available": {}, "pending": {}}
	currencies := map[string]bool{}
	for _, txn := range transactions {
		currency := txn["currency"].(string)
		currencies[currency] = true
		bySource := sums[txn["status"].(string)][currency]
		if bySource == nil {
			bySource = map[string]int64{"card": 0, "bank_account": 0}
			sums[txn["status"].(string)][currency] = bySource
		}
		bySource[sourceType(txn)] += txn["net"].(int64)
	}
	if len(currencies) == 0 {
		currencies["usd"] = true // the platform account's default currency (handlePlatformAccount)
	}
	sorted := make([]string, 0, len(currencies))
	for currency := range currencies {
		sorted = append(sorted, currency)
	}
	sort.Strings(sorted)
	funds := func(status string) []map[string]any {
		out := []map[string]any{}
		for _, currency := range sorted {
			bySource := sums[status][currency]
			out = append(out, map[string]any{"amount": bySource["card"] + bySource["bank_account"], "currency": currency, "source_types": map[string]any{"card": bySource["card"], "bank_account": bySource["bank_account"]}})
		}
		return out
	}
	available, pending := funds("available"), funds("pending")
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
	descriptions := map[string]string{}
	for _, intent := range intents {
		if intent.Status == "succeeded" {
			out = append(out, stripeBalanceTransaction(intent))
		}
		descriptions[intent.ID] = intent.Metadata["emulator_description"]
	}
	for _, refund := range refunds {
		if refund.Status != "failed" && refund.Status != "canceled" {
			txn := balanceMovement("txn_"+refund.ID, refund.ID, "refund", -refund.Amount, refund.Currency, refund.CreatedAt, refund.CreatedAt.Add(settlementDelay))
			// As Stripe describes a refund: after the payment it gives back.
			if description := descriptions[refund.PaymentIntentID]; description != "" {
				txn["description"] = "REFUND FOR CHARGE (" + description + ")"
			}
			out = append(out, txn)
		}
	}
	topups, err := h.billing.ListConnectResources(r.Context(), billing.ConnectResourceFilter{Object: billing.ObjectTopup, Status: "succeeded"})
	if err != nil {
		return nil, err
	}
	for _, topup := range topups {
		if topup.AccountID == r.Header.Get("Stripe-Account") {
			txn := balanceMovement("txn_"+topup.ID, topup.ID, "topup", topup.Amount, strings.ToLower(topup.Currency), topup.CreatedAt, topup.CreatedAt)
			txn["description"] = emptyToNil(topup.Description)
			out = append(out, txn)
		}
	}
	account := r.Header.Get("Stripe-Account")
	for _, payout := range payouts {
		if payout.AccountID != account || payout.Status == "failed" || payout.Status == "canceled" {
			continue
		}
		out = append(out, balanceMovement("txn_"+payout.ID, payout.ID, "payout", -payout.Amount, strings.ToLower(payout.Currency), payout.CreatedAt, payout.CreatedAt))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i]["created"] != out[j]["created"] {
			return out[i]["created"].(int64) > out[j]["created"].(int64)
		}
		return out[i]["id"].(string) > out[j]["id"].(string)
	})
	return out, nil
}

// balanceMovement: money moving without a fee: out (a refund, a payout: a negative amount) or in (a top-up).
// Its type is also its reporting category.
func balanceMovement(id, source, kind string, amount int64, currency string, created, availableOn time.Time) map[string]any {
	return map[string]any{
		"id":                 id,
		"object":             "balance_transaction",
		"amount":             amount,
		"available_on":       unix(availableOn),
		"created":            unix(created),
		"currency":           currency,
		"description":        nil,
		"exchange_rate":      nil,
		"fee":                int64(0),
		"fee_details":        []map[string]any{},
		"net":                amount,
		"reporting_category": kind,
		"source":             source,
		"status":             settlementStatus(availableOn),
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
