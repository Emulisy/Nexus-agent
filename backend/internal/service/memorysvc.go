// Memory management provides listing, archival, deletion, and manual extraction.
// The long-term manager owns extraction/recall algorithms; this service checks
// record/session ownership and coordinates PostgreSQL metadata with vector cleanup.
package service

import (
	"context"

	"github.com/chengpeng-cp/nexus-agent/internal/memory/long_term"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/pagination"
)

// memoryCollection must match the vector collection used by long_term.Manager.
const memoryCollection = "memories"

// MemoryService manages stored memories and optionally delegates extraction to an attached manager.
type MemoryService struct {
	svcs    *Services
	manager *long_term.Manager
}

// NewMemoryService binds storage resources; manual extraction remains unavailable until SetManager.
func NewMemoryService(svcs *Services) *MemoryService {
	return &MemoryService{svcs: svcs}
}

// SetManager attaches the long-term manager used by explicit extraction requests.
func (s *MemoryService) SetManager(m *long_term.Manager) { s.manager = m }

// List returns all caller-owned memory records, including archived entries, with pagination.
func (s *MemoryService) List(ctx context.Context, userID int64, q pagination.Query) ([]model.Memory, int64, error) {
	list, total, err := s.svcs.Deps.Repos.Memory.ListByUser(ctx, userID, q.Offset(), q.Limit())
	if err != nil {
		return nil, 0, errcode.ErrInternal.WithCause(err)
	}
	return list, total, nil
}

// Delete verifies record ownership and attempts to remove its vector before metadata.
// Vector deletion is best effort; PostgreSQL deletion failures are returned as
// internal errors rather than restoring a previously removed vector.
func (s *MemoryService) Delete(ctx context.Context, userID, id int64) error {
	m, err := s.svcs.Deps.Repos.Memory.Get(ctx, id)
	if err != nil {
		return errcode.ErrMemoryNotFound
	}
	if m.UserID != userID {
		return errcode.ErrForbidden
	}
	if s.svcs.Deps.Vector != nil {
		_ = s.svcs.Deps.Vector.Delete(ctx, memoryCollection, []int64{id})
	}
	if err := s.svcs.Deps.Repos.Memory.Delete(ctx, id); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}

// Archive verifies ownership and changes the metadata status to archived.
// The record and embedding remain stored, but active-memory lookups exclude it.
func (s *MemoryService) Archive(ctx context.Context, userID, id int64) error {
	m, err := s.svcs.Deps.Repos.Memory.Get(ctx, id)
	if err != nil {
		return errcode.ErrMemoryNotFound
	}
	if m.UserID != userID {
		return errcode.ErrForbidden
	}
	if err := s.svcs.Deps.Repos.Memory.Archive(ctx, id); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}

// ExtractInput references an existing conversation whose messages supply the extraction input.
type ExtractInput struct {
	SessionID string `json:"session_id"`
}

// ExtractNow requires an attached manager and a session owned by the caller.
// It passes up to 20 recent user/assistant messages and the session's agent scope
// to extraction. Empty history and extraction failures are returned to the request.
func (s *MemoryService) ExtractNow(ctx context.Context, userID int64, in *ExtractInput) error {
	if s.manager == nil {
		return errcode.ErrNotImplement.WithMsg("Long-term memory is disabled because no extraction model is configured")
	}
	if in.SessionID == "" {
		return errcode.ErrInvalidParam.WithMsg("session_id is required")
	}
	repo := s.svcs.Deps.Repos.Session
	session, err := repo.GetSession(ctx, in.SessionID)
	if err != nil {
		return errcode.ErrSessionNotFnd.WithCause(err)
	}
	if session.UserID != userID {
		return errcode.ErrForbidden
	}

	msgs, err := repo.ListMessages(ctx, session.ID, 20)
	if err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	if len(msgs) == 0 {
		return errcode.ErrInvalidParam.WithMsg("The session has no messages available for memory extraction")
	}
	if err := s.manager.ExtractAndStore(ctx, userID, session.AgentID, historyToLLM(msgs)); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	return nil
}
