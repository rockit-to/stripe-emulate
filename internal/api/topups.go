package api

import (
	"net/http"
	"strings"

	"github.com/chelokot/stripe-emulate/internal/billing"
	"github.com/chelokot/stripe-emulate/internal/webhooks"
)

// Top-ups add money to the balance from a bank account. Kept as payouts are (a ConnectResource); in test mode a
// top-up succeeds at once and its money is available at once, as Stripe's sandbox shows it.

// handleTopups serves GET and POST /v1/topups.
func (h *Handler) handleTopups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resources, err := h.billing.ListConnectResources(r.Context(), billing.ConnectResourceFilter{
			Object: billing.ObjectTopup,
			Status: r.URL.Query().Get("status"),
		})
		data := make([]map[string]any, 0, len(resources))
		for _, resource := range resources {
			data = append(data, stripeTopup(resource))
		}
		writeResult(w, stripeListFromRequest(r, data), err)
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateTopupCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resource, err := h.billing.CreateConnectResource(r.Context(), billing.ConnectResource{
			ID:          p.string("id"),
			Object:      billing.ObjectTopup,
			AccountID:   r.Header.Get("Stripe-Account"),
			Amount:      p.int64("amount"),
			Currency:    p.string("currency"),
			Description: p.string("description"),
			Status:      "succeeded",
			Metadata:    p.metadata(),
			Data: map[string]string{
				"statement_descriptor": p.string("statement_descriptor"),
				"transfer_group":       p.string("transfer_group"),
			},
		})
		if err == nil {
			h.emitGenericWebhook(r, "topup.created", resource.ID, stripeTopup(resource), webhooks.SourceAPI)
			h.emitGenericWebhook(r, "topup.succeeded", resource.ID, stripeTopup(resource), webhooks.SourceAPI)
		}
		writeResult(w, stripeTopup(resource), err)
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

// handleTopup serves GET and POST /v1/topups/{id} and POST /v1/topups/{id}/cancel.
func (h *Handler) handleTopup(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/topups/"), "/"), "/")
	id := parts[0]
	switch {
	case id == "" || len(parts) > 2 || (len(parts) == 2 && parts[1] != "cancel"):
		h.notFound(w, r)
	case len(parts) == 2 && r.Method != http.MethodPost:
		h.methodNotAllowed(w, r, "POST")
	case len(parts) == 2:
		h.cancelTopup(w, r, id)
	case r.Method == http.MethodGet:
		resource, err := h.billing.GetConnectResource(r.Context(), billing.ObjectTopup, id)
		writeResult(w, stripeTopup(resource), err)
	case r.Method == http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateTopupUpdate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resource, err := h.billing.UpdateConnectResource(r.Context(), billing.ObjectTopup, id, billing.ConnectResource{
			Description: p.string("description"),
			Metadata:    p.metadata(),
		})
		writeResult(w, stripeTopup(resource), err)
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

// cancelTopup: like Stripe, only a pending top-up can be canceled.
func (h *Handler) cancelTopup(w http.ResponseWriter, r *http.Request, id string) {
	resource, err := h.billing.GetConnectResource(r.Context(), billing.ObjectTopup, id)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	if resource.Status != "pending" {
		writeResult(w, nil, &billing.InvalidRequestError{Message: "This topup can no longer be canceled because it is " + resource.Status + "."})
		return
	}
	resource, err = h.billing.UpdateConnectResource(r.Context(), billing.ObjectTopup, id, billing.ConnectResource{Status: "canceled"})
	if err == nil {
		h.emitGenericWebhook(r, "topup.canceled", resource.ID, stripeTopup(resource), webhooks.SourceAPI)
	}
	writeResult(w, stripeTopup(resource), err)
}

func stripeTopup(resource billing.ConnectResource) map[string]any {
	return map[string]any{
		"id":                         resource.ID,
		"object":                     billing.ObjectTopup,
		"amount":                     resource.Amount,
		"balance_transaction":        "txn_" + resource.ID,
		"created":                    unix(resource.CreatedAt),
		"currency":                   strings.ToLower(resource.Currency),
		"description":                emptyToNil(resource.Description),
		"expected_availability_date": unix(resource.CreatedAt),
		"failure_code":               nil,
		"failure_message":            nil,
		"livemode":                   false,
		"metadata":                   nonNilMap(resource.Metadata),
		"source":                     nil,
		"statement_descriptor":       emptyToNil(resource.Data["statement_descriptor"]),
		"status":                     resource.Status,
		"transfer_group":             emptyToNil(resource.Data["transfer_group"]),
	}
}
