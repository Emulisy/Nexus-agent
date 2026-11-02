// Model administration endpoints use pointer-based request types for partial updates.
// Omitted fields are distinguishable from explicit zero values. Provider listing
// uses a masked-key view rather than exposing stored or decrypted credentials.
package api

import (
	"net/http"

	"github.com/Emulisy/SupaAgent/internal/model"
)

// ---- Provider ----

// ListProviders returns administrator-facing provider views with masked API keys.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svcs.Models.ListProviders(ctx)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, list)
}

// CreateProvider binds supplied fields and delegates defaults and key encryption.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req := &model.ProviderUpsert{}
	if err := decodeJSON(w, r, req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.CreateProvider(ctx, req); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// UpdateProvider combines the route ID with partial fields; omitted or empty keys are preserved.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	req := &model.ProviderUpsert{}
	if err := decodeJSON(w, r, req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.UpdateProvider(ctx, id, req); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// DeleteProvider delegates removal; database constraints govern dependent model records.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.DeleteProvider(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ---- ModelConfig ----

// ListModels lists configurations with provider associations and an optional type filter.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svcs.Models.ListModels(ctx, string(r.URL.Query().Get("type")))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, list)
}

// ListChatModels exposes enabled chat/provider pairs to signed-in users for the model selector.
func (h *Handler) ListChatModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svcs.Models.ListChatModels(ctx)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, list)
}

// CreateModel delegates required-field checks and default values to ModelService.
func (h *Handler) CreateModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req := &model.ModelUpsert{}
	if err := decodeJSON(w, r, req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.CreateModel(ctx, req); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// UpdateModel preserves omitted fields through ModelUpsert pointer values.
func (h *Handler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	req := &model.ModelUpsert{}
	if err := decodeJSON(w, r, req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.UpdateModel(ctx, id, req); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// DeleteModel removes the model identified by the route parameter.
func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Models.DeleteModel(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}
