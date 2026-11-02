// Package long_term maintains durable user facts through model-assisted extraction
// and reconciliation. PostgreSQL stores fact content and lifecycle state; Milvus
// provides semantic candidates. Recall filters candidate IDs against active
// records for the requested user and optional agent before returning prompt text.
package long_term

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/vector"
)

// MemoryRepo supplies durable facts and scope filtering without exposing a
// database client to the extraction pipeline.
type MemoryRepo interface {
	Create(ctx context.Context, m *model.Memory) error
	UpdateContent(ctx context.Context, id int64, content string) error
	Archive(ctx context.Context, id int64) error
	ListActive(ctx context.Context, userID int64, agentID *int64, limit int) ([]model.Memory, error)
}

// Config holds extraction/recall defaults. RecallTopK is the vector candidate
// limit; extraction message batching is selected by the calling service.
type Config struct {
	// SimilarityThreshold is retained as configuration metadata; the current
	// reconciliation path does not apply a numeric similarity cutoff.
	SimilarityThreshold float32
	RecallTopK          int
	// ExtractBatch does not schedule extraction; the caller supplies the message batch.
	ExtractBatch int
}

// DefaultConfig selects three recall candidates and the extraction defaults.
func DefaultConfig() Config {
	return Config{SimilarityThreshold: 0.88, RecallTopK: 3, ExtractBatch: 6}
}

// Manager combines model callbacks, fact persistence, and the shared memories
// vector collection. Model callbacks allow service-side configuration resolution
// without embedding provider credentials into stored memory records.
type Manager struct {
	gateway    *llm.Gateway
	extractMdl func() *llm.ModelDescriptor
	embedMdl   func() *llm.ModelDescriptor
	repo       MemoryRepo
	store      vector.Store
	collection string
	cfg        Config
}

// NewManager uses the shared memories collection and resolves extraction and
// embedding models on demand through the supplied callbacks.
func NewManager(gateway *llm.Gateway, extractMdl, embedMdl func() *llm.ModelDescriptor, repo MemoryRepo, store vector.Store) *Manager {
	return &Manager{
		gateway: gateway, extractMdl: extractMdl, embedMdl: embedMdl,
		repo: repo, store: store, collection: "memories", cfg: DefaultConfig(),
	}
}

// extractPrompt requests independent durable facts rather than a conversation
// summary. Output is parsed as a JSON array before any embeddings are requested.
const extractPrompt = `Extract facts about the user that will remain useful over time (preferences, background, constraints, and ongoing activities).
Rules: 1. Write each fact as a standalone statement. 2. Output only a JSON array, with no additional text. 3. Output [] if there are no facts to extract.
Example: ["The user prefers Go","The user is preparing for a system design interview"]`

// opsPrompt asks the model to reconcile new facts with the scoped semantic
// candidates using ADD, UPDATE, DELETE, or NOOP operations.
const opsPrompt = `Compare the new memories with the existing memories and choose an operation for each new memory:
ADD (create), UPDATE (id, replacement content), DELETE (id, remove outdated memory), or NOOP (ignore duplicates).
Output only a JSON array: [{"op":"ADD","content":"..."},{"op":"UPDATE","id":1,"content":"..."},{"op":"DELETE","id":2}]`

// ExtractAndStore extracts fact strings, embeds them, finds existing candidates,
// and requests reconciliation operations from the model. It runs only when called;
// periodic or per-response scheduling and background timeouts belong to services.
// Writes are best effort and are not a database/vector transaction.
func (m *Manager) ExtractAndStore(ctx context.Context, userID int64, agentID *int64, msgs []llm.Message) error {
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString(msg.Role + ": " + msg.Content + "\n")
	}
	resp, _, err := m.gateway.Chat(ctx, m.extractMdl(), nil, &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: extractPrompt},
			{Role: llm.RoleUser, Content: b.String()},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return err
	}

	// A response without a valid, non-empty fact array is treated as nothing to store.
	var facts []string
	if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &facts); err != nil || len(facts) == 0 {
		return nil
	}

	factVecs, err := m.gateway.Embed(ctx, m.embedMdl(), facts)
	if err != nil {
		return fmt.Errorf("embed facts: %w", err)
	}

	// Use the first new fact to obtain semantic candidates, then match IDs against
	// active records in this user/agent scope before exposing candidate content.
	existing, err := m.store.Search(ctx, m.collection, factVecs[0], 10)
	if err != nil {
		existing = nil
	}
	candidates := make([]string, 0, len(existing))
	for _, h := range existing {
		if mm, e := m.repo.ListActive(ctx, userID, agentID, 50); e == nil {
			for _, item := range mm {
				if item.ID == h.ID {
					candidates = append(candidates, fmt.Sprintf(`{"id":%d,"content":%q}`, item.ID, item.Content))
				}
			}
		}
	}

	opsMsg := fmt.Sprintf("Existing memories: [%s]\nNew facts: %q", strings.Join(candidates, ","), facts)
	opsResp, _, err := m.gateway.Chat(ctx, m.extractMdl(), nil, &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: opsPrompt},
			{Role: llm.RoleUser, Content: opsMsg},
		},
		Temperature: 0.1,
	})
	if err != nil {
		return err
	}

	// The second model response determines persistence operations. Invalid operation
	// JSON yields an empty operation list rather than failing the chat response.
	var ops []struct {
		Op      string `json:"op"`
		ID      int64  `json:"id"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal([]byte(extractJSON(opsResp.Content)), &ops)

	// ADD associates the operation position with the corresponding fact vector.
	// UPDATE changes SQL content only; DELETE archives the SQL record so future
	// scoped recall filters it out even if its old vector remains present.
	for i, op := range ops {
		switch op.Op {
		case "ADD", "":
			if i >= len(factVecs) {
				continue
			}
			mem := &model.Memory{UserID: userID, AgentID: agentID, Content: op.Content, Scope: "user"}
			if err := m.repo.Create(ctx, mem); err != nil {
				continue
			}
			agentIDVal := int64(0)
			if agentID != nil {
				agentIDVal = *agentID
			}
			_ = m.store.Upsert(ctx, m.collection, []vector.Record{{
				ID: mem.ID, Vector: factVecs[i],
				Metadata: map[string]any{"user_id": userID, "agent_id": agentIDVal},
			}})
		case "UPDATE":
			_ = m.repo.UpdateContent(ctx, op.ID, op.Content)
		case "DELETE":
			_ = m.repo.Archive(ctx, op.ID)
		}
	}
	return nil
}

// Recall embeds the query, obtains a global vector top-k, then retains only IDs
// present in active records for the requested user/agent. Because scope filtering
// happens after vector search, fewer than RecallTopK facts may be returned.
// Hit counters and timestamps on returned values are not persisted by this method.
func (m *Manager) Recall(ctx context.Context, userID int64, agentID *int64, query string) ([]model.Memory, error) {
	vec, err := m.gateway.Embed(ctx, m.embedMdl(), []string{query})
	if err != nil {
		return nil, err
	}
	hits, err := m.store.Search(ctx, m.collection, vec[0], m.cfg.RecallTopK)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	all, err := m.repo.ListActive(ctx, userID, agentID, 200)
	if err != nil {
		return nil, err
	}
	byID := map[int64]model.Memory{}
	for _, mm := range all {
		byID[mm.ID] = mm
	}

	out := make([]model.Memory, 0, len(hits))
	now := time.Now()
	for _, h := range hits {
		if mm, ok := byID[h.ID]; ok {
			mm.HitCount++
			t := now
			mm.LastHitAt = &t
			out = append(out, mm)
		}
	}
	return out, nil
}

// extractJSON takes the outer array from model text, tolerating surrounding
// explanation or code fences. Missing brackets produce an empty array string.
func extractJSON(s string) string {
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.LastIndex(s, "]"); j > i {
			return s[i : j+1]
		}
	}
	return "[]"
}
