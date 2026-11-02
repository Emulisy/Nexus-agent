// Knowledge-base services coordinate metadata, vector collections, and document jobs.
// Ownership is checked through the parent knowledge base before document access.
// Database, object-store, vector, and stream operations are separate writes;
// failures after an earlier write can leave a partially completed operation.
package service

import (
	"bytes"
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/infra"
	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/pkg/pagination"
)

// MaxDocSize bounds an uploaded document to 20 MiB before object storage or job publication.
const MaxDocSize = 20 << 20

// allowedDocTypes is the filename-extension allowlist applied to document uploads.
var allowedDocTypes = map[string]bool{
	".pdf": true, ".docx": true, ".md": true, ".markdown": true,
	".txt": true, ".html": true, ".htm": true,
}

// KBService shares RAG defaults and receives object storage separately after construction.
type KBService struct {
	svcs   *Services
	cfg    *config.RAG
	minio  *minio.Client
	bucket string
}

// NewKBService binds the service container and the configured RAG chunk defaults.
func NewKBService(svcs *Services) *KBService {
	return &KBService{svcs: svcs, cfg: &svcs.Deps.Cfg.RAG}
}

// SetMinIO attaches the object client and bucket used for uploaded document content.
func (s *KBService) SetMinIO(cli *minio.Client, bucket string) {
	s.minio = cli
	s.bucket = bucket
}

// CreateKB assigns ownership, resolves a missing embedding-model foreign key,
// and fills nonpositive chunk settings from global defaults. It persists the KB
// before deriving kb_<id>, then creates the vector collection if an engine is attached.
func (s *KBService) CreateKB(ctx context.Context, userID int64, kb *model.KnowledgeBase) error {
	if kb.Name == "" {
		return errcode.ErrInvalidParam.WithMsg("name is required")
	}
	kb.UserID = userID
	// Resolve a default embedding model before inserting the non-null foreign key.
	if kb.EmbeddingConfigID <= 0 {
		m, err := s.svcs.Deps.Repos.LLM.DefaultModel(ctx, s.svcs.Deps.Cfg.LLM.DefaultEmbedding, modelTypeEmbed)
		if err != nil {
			return errcode.ErrInvalidParam.WithMsg("No embedding model is available. Enable one in model management first.").WithCause(err)
		}
		kb.EmbeddingConfigID = m.ID
	}
	if kb.ChunkSize <= 0 {
		kb.ChunkSize = s.cfg.ChunkSize
	}
	if kb.ChunkOverlap <= 0 {
		kb.ChunkOverlap = s.cfg.ChunkOverlap
	}
	if err := s.svcs.Deps.Repos.KB.CreateKB(ctx, kb); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	// Persist first because the collection name depends on the generated ID.
	kb.MilvusCollection = fmt.Sprintf("kb_%d", kb.ID)
	if err := s.svcs.Deps.Repos.KB.UpdateKB(ctx, kb); err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	if s.svcs.ragEngine != nil {
		if err := s.svcs.ragEngine.EnsureKBVectorCollection(ctx, kb.MilvusCollection); err != nil {
			return errcode.ErrInternal.WithCause(err)
		}
	}
	return nil
}

// ListKBs delegates the current user and pagination to the active-knowledge-base query.
func (s *KBService) ListKBs(ctx context.Context, userID int64, q pagination.Query) ([]model.KnowledgeBase, int64, error) {
	return s.svcs.Deps.Repos.KB.ListKBs(ctx, userID, q.Offset(), q.Limit())
}

// GetKB uses an owner-filtered lookup and translates missing/inaccessible results to KB-not-found.
func (s *KBService) GetKB(ctx context.Context, userID, id int64) (*model.KnowledgeBase, error) {
	kb, err := s.svcs.Deps.Repos.KB.GetKBForUser(ctx, id, userID)
	if err != nil {
		return nil, errcode.ErrKBNotFound.WithCause(err)
	}
	return kb, nil
}

// DeleteKB checks ownership, loads up to 10,000 documents, and attempts cleanup.
// Document/vector cleanup failures are ignored here before deleting the KB row;
// the final database deletion result is returned to the caller.
func (s *KBService) DeleteKB(ctx context.Context, userID, id int64) error {
	kb, err := s.GetKB(ctx, userID, id)
	if err != nil {
		return err
	}
	docs, _, err := s.svcs.Deps.Repos.KB.ListDocs(ctx, id, 0, 10000)
	if err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	for _, d := range docs {
		_ = s.svcs.ragEngine.DeleteDocument(ctx, kb.MilvusCollection, d.ID)
		_ = s.svcs.Deps.Repos.KB.DeleteDoc(ctx, d.ID)
	}
	if s.svcs.Deps.Vector != nil {
		_ = s.svcs.Deps.Vector.DropCollection(ctx, kb.MilvusCollection)
	}
	return s.svcs.Deps.Repos.KB.DeleteKB(ctx, id)
}

// UploadDoc validates size/type and ownership, then creates a pending document.
// The generated ID determines the object key. Object upload and stream publication
// follow metadata writes; either can fail without rolling back the document row.
// An absent publisher leaves the document pending rather than ingesting it inline.
func (s *KBService) UploadDoc(ctx context.Context, userID, kbID int64, filename string, data []byte) (*model.Document, error) {
	if len(data) > MaxDocSize {
		return nil, errcode.ErrFileTooLarge
	}
	if !allowedDocTypes[extOf(filename)] {
		return nil, errcode.ErrFileType
	}
	kb, err := s.GetKB(ctx, userID, kbID)
	if err != nil {
		return nil, err
	}

	// Create the pending record first to obtain an ID for the object key.
	doc := &model.Document{
		KBID:     kb.ID,
		Filename: filename,
		FileType: extOf(filename)[1:],
		FileSize: int64(len(data)),
		Status:   model.DocStatusPending,
	}
	if err := s.svcs.Deps.Repos.KB.CreateDoc(ctx, doc); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	objectKey := infra.ObjectKey(kb.ID, doc.ID, filename)
	doc.ObjectKey = objectKey
	if err := s.svcs.Deps.Repos.KB.UpdateDoc(ctx, doc); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}

	if s.minio != nil {
		_, err := s.minio.PutObject(ctx, s.bucket, objectKey,
			bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: mimeOf(filename)})
		if err != nil {
			return nil, errcode.ErrInternal.WithCause(fmt.Errorf("put object: %w", err))
		}
	}

	if s.svcs.Deps.Streams != nil {
		if err := s.svcs.Deps.Streams.PublishDocETL(ctx, doc.ID); err != nil {
			return nil, errcode.ErrInternal.WithCause(err)
		}
	}
	return doc, nil
}

// ListDocs verifies access to the parent KB before returning its document page.
func (s *KBService) ListDocs(ctx context.Context, userID, kbID int64, q pagination.Query) ([]model.Document, int64, error) {
	if _, err := s.GetKB(ctx, userID, kbID); err != nil {
		return nil, 0, err
	}
	return s.svcs.Deps.Repos.KB.ListDocs(ctx, kbID, q.Offset(), q.Limit())
}

// DeleteDoc verifies both KB ownership and the document-to-KB relationship.
// When RAG is attached it removes indexed content first, then deletes the document
// record. This path does not delete the original object from MinIO.
func (s *KBService) DeleteDoc(ctx context.Context, userID, kbID, docID int64) error {
	kb, err := s.GetKB(ctx, userID, kbID)
	if err != nil {
		return err
	}
	doc, err := s.svcs.Deps.Repos.KB.GetDoc(ctx, docID)
	if err != nil || doc.KBID != kb.ID {
		return errcode.ErrDocNotFound
	}
	if s.svcs.ragEngine != nil {
		if err := s.svcs.ragEngine.DeleteDocument(ctx, kb.MilvusCollection, docID); err != nil {
			return errcode.ErrInternal.WithCause(err)
		}
	}
	return s.svcs.Deps.Repos.KB.DeleteDoc(ctx, docID)
}

// ListChunks loads the document, checks its parent KB ownership, and returns chunks in sequence order.
func (s *KBService) ListChunks(ctx context.Context, userID, docID int64, q pagination.Query) ([]model.DocumentChunk, int64, error) {
	doc, err := s.svcs.Deps.Repos.KB.GetDoc(ctx, docID)
	if err != nil {
		return nil, 0, errcode.ErrDocNotFound
	}
	if _, err := s.GetKB(ctx, userID, doc.KBID); err != nil {
		return nil, 0, err
	}
	return s.svcs.Deps.Repos.KB.ListChunks(ctx, docID, q.Offset(), q.Limit())
}

// TestRetrieve checks ownership and executes retrieval without answer generation.
// It returns a reduced reference view for inspection; an unattached RAG engine
// produces the service's not-implemented error.
func (s *KBService) TestRetrieve(ctx context.Context, userID, kbID int64, query string) ([]RetrievalRef, error) {
	kb, err := s.GetKB(ctx, userID, kbID)
	if err != nil {
		return nil, err
	}
	if s.svcs.ragEngine == nil {
		return nil, errcode.ErrNotImplement.WithMsg("RAG engine is not configured")
	}
	refs, err := s.svcs.ragEngine.Retrieve(ctx, kb, query)
	if err != nil {
		return nil, errcode.From(err)
	}
	out := make([]RetrievalRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, RetrievalRef{
			ChunkID: r.ChunkID, Filename: r.Filename, Page: r.Page,
			Content: r.Content, Score: r.Score,
		})
	}
	return out, nil
}

// RetrievalRef exposes chunk text, source location, and score from a retrieval-only request.
type RetrievalRef struct {
	ChunkID  int64   `json:"chunk_id"`
	Filename string  `json:"filename"`
	Page     int     `json:"page"`
	Content  string  `json:"content"`
	Score    float32 `json:"score"`
}

// extOf returns the lowercase suffix beginning at the final dot, or an empty string when none exists.
func extOf(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return lower(name[i:])
		}
	}
	return ""
}

// lower normalizes ASCII uppercase bytes used by the supported extension allowlist.
func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// mimeOf selects the object Content-Type for supported document formats, defaulting to plain text.
func mimeOf(name string) string {
	switch extOf(name) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".html", ".htm":
		return "text/html"
	default:
		return "text/plain"
	}
}
