package api

import (
	"net/http"
	"strings"

	"github.com/chelokot/stripe-emulate/internal/billing"
)

// Shipping rates and entitlement features: what a product catalogue lists beside its products. Kept as top-ups and
// payouts are (a ConnectResource), active until archived.

// handleShippingRates serves GET and POST /v1/shipping_rates.
func (h *Handler) handleShippingRates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resources, err := h.billing.ListConnectResources(r.Context(), billing.ConnectResourceFilter{Object: billing.ObjectShippingRate})
		data := []map[string]any{}
		for _, resource := range resources {
			if activeFilter(r, resource) {
				data = append(data, stripeShippingRate(resource))
			}
		}
		writeResult(w, stripeListFromRequest(r, data), err)
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateShippingRateCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resource, err := h.billing.CreateConnectResource(r.Context(), billing.ConnectResource{
			Object:      billing.ObjectShippingRate,
			Amount:      p.int64("fixed_amount[amount]"),
			Currency:    p.string("fixed_amount[currency]"),
			Description: p.string("display_name"),
			Status:      "active",
			Metadata:    p.metadata(),
			Data:        map[string]string{"tax_behavior": p.stringDefault("tax_behavior", "unspecified")},
		})
		writeResult(w, stripeShippingRate(resource), err)
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

// handleShippingRate serves GET and POST /v1/shipping_rates/{id}.
func (h *Handler) handleShippingRate(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/shipping_rates/"), "/")
	h.handleCatalogItem(w, r, id, billing.ObjectShippingRate, validateShippingRateUpdate, stripeShippingRate)
}

// handleFeatures serves GET and POST /v1/entitlements/features.
func (h *Handler) handleFeatures(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resources, err := h.billing.ListConnectResources(r.Context(), billing.ConnectResourceFilter{Object: billing.ObjectFeature})
		archived := r.URL.Query().Get("archived")
		lookupKey := r.URL.Query().Get("lookup_key")
		data := []map[string]any{}
		for _, resource := range resources {
			if (archived == "" || (resource.Status == "archived") == (archived == "true")) && (lookupKey == "" || resource.Data["lookup_key"] == lookupKey) {
				data = append(data, stripeFeature(resource))
			}
		}
		writeResult(w, stripeListFromRequest(r, data), err)
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateFeatureCreate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resource, err := h.billing.CreateConnectResource(r.Context(), billing.ConnectResource{
			Object:      billing.ObjectFeature,
			Description: p.string("name"),
			Status:      "active",
			Metadata:    p.metadata(),
			Data:        map[string]string{"lookup_key": p.string("lookup_key")},
		})
		writeResult(w, stripeFeature(resource), err)
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

// handleFeature serves GET and POST /v1/entitlements/features/{id}.
func (h *Handler) handleFeature(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/entitlements/features/"), "/")
	h.handleCatalogItem(w, r, id, billing.ObjectFeature, validateFeatureUpdate, stripeFeature)
}

// handleCatalogItem: retrieve one, or update its name, metadata and whether it is active.
func (h *Handler) handleCatalogItem(w http.ResponseWriter, r *http.Request, id, object string, validate func(params) error, render func(billing.ConnectResource) map[string]any) {
	if id == "" || strings.Contains(id, "/") {
		h.notFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		resource, err := h.billing.GetConnectResource(r.Context(), object, id)
		writeResult(w, render(resource), err)
	case http.MethodPost:
		p, err := parseParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validate(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		status := ""
		if p.has("active") {
			status = map[bool]string{true: "active", false: "archived"}[p.boolDefault("active", true)]
		}
		resource, err := h.billing.UpdateConnectResource(r.Context(), object, id, billing.ConnectResource{
			Description: p.string("name"),
			Status:      status,
			Metadata:    p.metadata(),
		})
		writeResult(w, render(resource), err)
	default:
		h.methodNotAllowed(w, r, "GET, POST")
	}
}

// activeFilter: the list's ?active=true|false, as Stripe filters shipping rates.
func activeFilter(r *http.Request, resource billing.ConnectResource) bool {
	active := r.URL.Query().Get("active")
	return active == "" || (resource.Status == "active") == (active == "true")
}

func stripeShippingRate(resource billing.ConnectResource) map[string]any {
	return map[string]any{
		"id":                resource.ID,
		"object":            billing.ObjectShippingRate,
		"active":            resource.Status == "active",
		"created":           unix(resource.CreatedAt),
		"delivery_estimate": nil,
		"display_name":      emptyToNil(resource.Description),
		"fixed_amount":      map[string]any{"amount": resource.Amount, "currency": strings.ToLower(resource.Currency)},
		"livemode":          false,
		"metadata":          nonNilMap(resource.Metadata),
		"tax_behavior":      stringDefault(resource.Data["tax_behavior"], "unspecified"),
		"tax_code":          nil,
		"type":              "fixed_amount",
	}
}

func stripeFeature(resource billing.ConnectResource) map[string]any {
	return map[string]any{
		"id":         resource.ID,
		"object":     billing.ObjectFeature,
		"active":     resource.Status == "active",
		"livemode":   false,
		"lookup_key": resource.Data["lookup_key"],
		"metadata":   nonNilMap(resource.Metadata),
		"name":       resource.Description,
	}
}
