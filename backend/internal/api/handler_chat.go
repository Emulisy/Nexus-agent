// Chat handlers bridge application events to SSE and expose session history.
// Input failures use JSON responses; successful requests stream events until
// completion or client cancellation.
package api

import (
	"io"
	"net/http"

	"github.com/chengpeng-cp/nexus-agent/internal/service"
)

// ChatReq selects the session, agent, and optional model for a single turn.
// An empty session ID creates a session; agent ID zero selects the default
// assistant. Model overrides are applied to this turn without saving agent changes.
type ChatReq struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id"`
	Content   string `json:"content"`
	// Zero keeps the agent model; a positive ID overrides it for this request.
	ModelConfigID int64 `json:"model_config_id"`
}

// Chat handles POST /api/v1/chat/stream and forwards service events as SSE.
// The handler stays alive while streaming and uses the request cancellation context.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ChatReq
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}

	events, err := h.svcs.Chat.Stream(ctx, userID(r), &service.ChatInput{
		SessionID:     req.SessionID,
		AgentID:       req.AgentID,
		Content:       req.Content,
		ModelConfigID: req.ModelConfigID,
	})
	if err != nil {
		Fail(w, err)
		return
	}

	serveChatEvents(w, r, events)
}

// writeSSE writes one named event with a single JSON data line and a blank separator.
// The caller supplies serialized data and handles transport write errors.
func writeSSE(w io.Writer, event string, data []byte) (int, error) {
	return io.WriteString(w, "event: "+event+"\ndata: "+string(data)+"\n\n")
}

// ListSessions returns the authenticated user's active sessions and total count.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := parsePagination(r)
	list, total, err := h.svcs.Repos.Session.ListSessions(ctx, userID(r), q.Offset(), q.Limit())
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// ListMessages verifies session ownership before returning up to 200 recent messages.
// Messages are ordered chronologically and include stored trace and reference metadata.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sid := r.PathValue("id")
	session, err := h.svcs.Repos.Session.GetSession(ctx, sid)
	if err != nil {
		Fail(w, err)
		return
	}
	if session.UserID != userID(r) {
		forbidden(w)
		return
	}
	msgs, err := h.svcs.Repos.Session.ListMessages(ctx, sid, 200)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, msgs)
}

// RenameSession verifies ownership and passes the requested title to RetitleSession.
// That repository operation only changes a title while it still has the default value.
func (h *Handler) RenameSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sid := r.PathValue("id")
	session, err := h.svcs.Repos.Session.GetSession(ctx, sid)
	if err != nil {
		Fail(w, err)
		return
	}
	if session.UserID != userID(r) {
		forbidden(w)
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Repos.Session.RetitleSession(ctx, sid, req.Title); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// DeleteSession rejects sessions owned by another user before deleting the row.
// Related database cleanup follows the foreign-key behavior defined by migrations.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sid := r.PathValue("id")
	session, err := h.svcs.Repos.Session.GetSession(ctx, sid)
	if err != nil {
		Fail(w, err)
		return
	}
	if session.UserID != userID(r) {
		forbidden(w)
		return
	}
	if err := h.svcs.Repos.Session.DeleteSession(ctx, sid); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}
