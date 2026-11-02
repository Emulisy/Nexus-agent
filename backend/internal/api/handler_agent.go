// Agent endpoints pass authenticated ownership context to AgentService.
// Path IDs take precedence over body IDs for updates. Tool listing reads the
// catalog used when configuring agent bindings.
package api

import (
	"net/http"

	"github.com/chengpeng-cp/nexus-agent/internal/model"
)

// ListAgents returns shared agents and agents owned by the caller, with pagination.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, total, err := h.svcs.Agent.List(ctx, userID(r), parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// GetAgent returns an accessible agent or the service's not-found error.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	ag, err := h.svcs.Agent.Get(ctx, userID(r), id)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, ag)
}

// CreateAgent binds an agent definition; the service assigns its owner and resolves defaults.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ag := &model.Agent{}
	if err := decodeJSON(w, r, ag); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Agent.Create(ctx, userID(r), ag); err != nil {
		Fail(w, err)
		return
	}
	OK(w, ag)
}

// UpdateAgent uses the route ID and delegates editable-field merging and ownership checks.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	ag := &model.Agent{}
	if err := decodeJSON(w, r, ag); err != nil {
		Fail(w, err)
		return
	}
	ag.ID = id
	if err := h.svcs.Agent.Update(ctx, userID(r), ag); err != nil {
		Fail(w, err)
		return
	}
	OK(w, ag)
}

// DeleteAgent requests an owner-filtered deletion and returns an empty success result.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Agent.Delete(ctx, userID(r), id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ListTools lists catalog records, optionally filtered by type, for agent configuration.
func (h *Handler) ListTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, total, err := h.svcs.Repos.Tool.List(ctx, string(r.URL.Query().Get("type")), parsePagination(r).Offset(), parsePagination(r).Limit())
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}
