// Memory management endpoints operate on the signed-in user's stored memories.
// Deletion and archival are distinct operations; manual extraction uses an
// existing owned session rather than accepting arbitrary conversation text.
package api

import (
	"net/http"

	"github.com/Emulisy/SupaAgent/internal/service"
)

// ListMemories returns the caller's memory records, including archived records.
func (h *Handler) ListMemories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, total, err := h.svcs.Memory.List(ctx, userID(r), parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// DeleteMemory delegates ownership checking and record/vector removal to the service.
func (h *Handler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Memory.Delete(ctx, userID(r), id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ArchiveMemory marks an owned memory inactive for recall while retaining its stored record.
func (h *Handler) ArchiveMemory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Memory.Archive(ctx, userID(r), id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ExtractMemory requests immediate extraction from the specified owned session.
func (h *Handler) ExtractMemory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req service.ExtractInput
	if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.Memory.ExtractNow(ctx, userID(r), &req); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}
