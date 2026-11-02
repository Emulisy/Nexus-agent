// Integration endpoints expose administrator operations for MCP servers and A2A agents.
// MCP synchronization persists remote tool definitions; A2A registration stores
// an agent-card snapshot and delegates requests through the remote client.
package api

import (
	"net/http"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/service"
)

// ListMCPServers returns registered servers and their cached availability metadata.
func (h *Handler) ListMCPServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svcs.MCP.List(ctx)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, list)
}

// CreateMCPServer persists a bound server definition using service defaults.
func (h *Handler) CreateMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	srv := &model.MCPServer{}
	if err := decodeJSON(w, r, srv); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.MCP.Create(ctx, srv); err != nil {
		Fail(w, err)
		return
	}
	OK(w, srv)
}

// UpdateMCPServer takes the server ID from the route and saves the bound configuration.
func (h *Handler) UpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	srv := &model.MCPServer{}
	if err := decodeJSON(w, r, srv); err != nil {
		Fail(w, err)
		return
	}
	srv.ID = id
	if err := h.svcs.MCP.Update(ctx, srv); err != nil {
		Fail(w, err)
		return
	}
	OK(w, srv)
}

// DeleteMCPServer delegates deletion of the server and its catalog tools.
func (h *Handler) DeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.MCP.Delete(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// SyncMCPServer discovers remote tools and returns the newly persisted catalog entries.
func (h *Handler) SyncMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	tools, err := h.svcs.MCP.SyncTools(ctx, id)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, tools)
}

// CheckMCPServer pings the configured server and returns connected only after success.
func (h *Handler) CheckMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.MCP.HealthCheck(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"status": "connected"})
}

// CallMCPToolReq identifies a remote tool by its server-side name and structured arguments.
type CallMCPToolReq struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// CallMCPTool invokes the named remote tool directly and returns its textual output.
func (h *Handler) CallMCPTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	var req CallMCPToolReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	out, err := h.svcs.MCP.CallTool(ctx, id, req.Name, req.Args)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"output": out})
}

// ListA2AAgents returns remote-agent registrations and their latest card snapshots.
func (h *Handler) ListA2AAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svcs.A2A.List(ctx)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, list)
}

// RegisterA2AAgent fetches a card through the service before returning the stored registration.
func (h *Handler) RegisterA2AAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req service.A2ARegisterInput
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	ag, err := h.svcs.A2A.Register(ctx, &req)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, ag)
}

// DeleteA2AAgent removes the selected remote registration without calling the remote agent.
func (h *Handler) DeleteA2AAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.A2A.Delete(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// CheckA2AAgent refreshes the remote card and persists the current availability state.
func (h *Handler) CheckA2AAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.A2A.HealthCheck(ctx, id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"status": "connected"})
}

// SendA2ATaskReq carries the text sent to the selected remote A2A agent.
type SendA2ATaskReq struct {
	Text string `json:"text"`
}

// SendA2ATask waits for message/send and returns the remote task state and extracted answer text.
func (h *Handler) SendA2ATask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	var req SendA2ATaskReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	res, err := h.svcs.A2A.SendTask(ctx, id, req.Text)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, res)
}
