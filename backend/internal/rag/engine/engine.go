// Package engine coordinates document indexing and hybrid retrieval. Indexing
// persists chunks in PostgreSQL before writing embeddings to Milvus. Retrieval
// combines vector and keyword rankings, restores candidate text, optionally reranks,
// and formats references for inclusion in a model prompt.
//
// Document loading and job status belong to the worker; model and knowledge-base
// access checks belong to the service layer.
package engine

import (
	"context"
	"fmt"

	"gorm.io/datatypes"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/rag/loader"
	"github.com/Emulisy/SupaAgent/internal/rag/splitter"
	"github.com/Emulisy/SupaAgent/internal/vector"
)

// ChunkRepo supplies chunk persistence and keyword ranking without exposing the
// database client to the engine. Vector records use the IDs assigned by BatchCreate.
type ChunkRepo interface {
	// BatchCreate inserts chunks together and populates their database IDs.
	BatchCreate(ctx context.Context, chunks []model.DocumentChunk) error

	// ListByDoc returns the IDs needed to remove a document from vector storage.
	ListByDoc(ctx context.Context, docID int64) ([]model.DocumentChunk, error)

	// DeleteByDoc removes persisted chunks after their vectors have been deleted.
	DeleteByDoc(ctx context.Context, docID int64) error

	// KeywordSearch returns scored, text-bearing candidates from PostgreSQL.
	KeywordSearch(ctx context.Context, kbID int64, terms []string, limit int) ([]ScoredChunk, error)

	// ListByIDs hydrates dense hits because vector search returns IDs and scores.
	ListByIDs(ctx context.Context, ids []int64) ([]model.DocumentChunk, error)
}

// ScoredChunk is a keyword candidate with its original database record and source
// filename. Its raw score is used for ranking before reciprocal rank fusion.
type ScoredChunk struct {
	Chunk    model.DocumentChunk
	Filename string
	Score    float64
}

// Engine shares the model gateway and vector store between indexing and retrieval.
// Model callbacks resolve the currently selected embedding/rerank descriptors rather
// than keeping credentials in the indexing configuration.
type Engine struct {
	gateway *llm.Gateway
	store   vector.Store
	repo    ChunkRepo
	cfg     *config.RAG
	dim     int

	embedMdl  func() *llm.ModelDescriptor
	rerankMdl func() *llm.ModelDescriptor
}

// New wires storage, model access, retrieval parameters, and collection dimension.
// SetModels must supply the embedding resolver before indexing or retrieval.
func New(gateway *llm.Gateway, store vector.Store, repo ChunkRepo, cfg *config.RAG, dim int) *Engine {
	return &Engine{gateway: gateway, store: store, repo: repo, cfg: cfg, dim: dim}
}

// SetModels installs model descriptor callbacks. The rerank callback may return
// nil to skip that optional stage; embedding requires a usable descriptor.
func (e *Engine) SetModels(embed, rerank func() *llm.ModelDescriptor) {
	e.embedMdl = embed
	e.rerankMdl = rerank
}

// EnsureKBVectorCollection creates the knowledge-base collection and its index
// when needed, using the configured embedding dimension.
func (e *Engine) EnsureKBVectorCollection(ctx context.Context, collection string) error {
	return e.store.EnsureCollection(ctx, collection, e.dim)
}

// IndexDocument splits loaded pages, persists chunks to obtain stable IDs, and
// batches embeddings into the knowledge-base vector collection. It updates the
// passed document count fields but leaves status transitions and saving that
// document to the worker. Database and vector writes are not one transaction.
func (e *Engine) IndexDocument(ctx context.Context, kb *model.KnowledgeBase, doc *model.Document, loaded *loader.LoadedDoc) error {
	// Knowledge-base settings override global split defaults when positive.
	chunkSize, chunkOverlap := kb.ChunkSize, kb.ChunkOverlap
	if chunkSize <= 0 {
		chunkSize = e.cfg.ChunkSize
	}
	if chunkOverlap <= 0 {
		chunkOverlap = e.cfg.ChunkOverlap
	}
	sp := splitter.New(chunkSize, chunkOverlap)

	// Retain page metadata alongside text so references can point back to the
	// source page after either dense or keyword retrieval.
	var chunks []model.DocumentChunk
	for _, page := range loaded.Pages {
		for _, c := range sp.Split(page.Content, page.Page) {
			chunks = append(chunks, model.DocumentChunk{
				KBID:       kb.ID,
				DocumentID: doc.ID,
				Seq:        c.Seq,
				Content:    c.Content,
				Meta:       datatypes.JSON(fmt.Sprintf(`{"page":%d}`, c.Page)),
			})
		}
	}
	if len(chunks) == 0 {
		return fmt.Errorf("document produced no chunks: %s", doc.Filename)
	}
	// Persist first: Milvus primary keys must be the generated PostgreSQL chunk
	// IDs, not positional indexes within this document.
	if err := e.repo.BatchCreate(ctx, chunks); err != nil {
		return fmt.Errorf("save chunks: %w", err)
	}

	if err := e.embedChunks(ctx, kb.MilvusCollection, chunks); err != nil {
		return fmt.Errorf("embed chunks: %w", err)
	}

	doc.ChunkCount = len(chunks)
	totalTokens := 0
	for _, c := range chunks {
		totalTokens += c.TokenCount
	}
	doc.TokenCount = totalTokens
	return nil
}

// embedChunks limits upstream embedding batch size and writes vectors with
// chunk identity and citation metadata. A failed batch stops this indexing attempt;
// previously stored chunks or batches are not rolled back here.
func (e *Engine) embedChunks(ctx context.Context, collection string, chunks []model.DocumentChunk) error {
	// Use a bounded default batch when no positive batch size is configured.
	batch := e.cfg.BatchEmbed
	if batch <= 0 {
		batch = 64
	}
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		batchChunks := chunks[i:end]

		texts := make([]string, 0, len(batchChunks))
		for _, c := range batchChunks {
			texts = append(texts, c.Content)
		}
		vecs, err := e.gateway.Embed(ctx, e.embedMdl(), texts)
		if err != nil {
			return err
		}

		// Pair vectors with the same input ordering used by the embedding request.
		records := make([]vector.Record, 0, len(batchChunks))
		for j, c := range batchChunks {
			c.TokenCount = llm.EstimateTokens(c.Content)
			records = append(records, vector.Record{
				ID:     c.ID,
				Vector: vecs[j],
				Metadata: map[string]any{
					"doc_id":  c.DocumentID,
					"seq":     int32(c.Seq),
					"page":    int32(chunkPage(c)),
					"content": c.Content,
				},
			})
		}
		if err := e.store.Upsert(ctx, collection, records); err != nil {
			return err
		}
	}
	return nil
}

// DeleteDocument deletes vectors before chunk rows so their IDs remain available
// for cleanup. A vector-delete failure stops before the database deletion.
func (e *Engine) DeleteDocument(ctx context.Context, collection string, docID int64) error {
	chunks, err := e.repo.ListByDoc(ctx, docID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(chunks))
	for _, c := range chunks {
		ids = append(ids, c.ID)
	}
	if len(ids) > 0 {
		if err := e.store.Delete(ctx, collection, ids); err != nil {
			return fmt.Errorf("delete vectors: %w", err)
		}
	}
	return e.repo.DeleteByDoc(ctx, docID)
}

// Ref is a retrieved citation with document identity, source position, text, and
// its final relevance score. Score is an RRF value unless reranking succeeded.
type Ref struct {
	ChunkID    int64   `json:"chunk_id"`
	DocumentID int64   `json:"document_id"`
	Filename   string  `json:"filename"`
	Page       int     `json:"page"`
	Content    string  `json:"content"`
	Score      float32 `json:"score"`
}

// Retrieve performs dense search, then keyword search, reciprocal rank fusion,
// text hydration, optional reranking, and top-k selection. Dense failures abort
// this knowledge-base retrieval; keyword and rerank failures retain the usable
// results from earlier stages.
func (e *Engine) Retrieve(ctx context.Context, kb *model.KnowledgeBase, query string) ([]Ref, error) {
	// Embed the query with the same embedding resolver used during indexing.
	queryVecs, err := e.gateway.Embed(ctx, e.embedMdl(), []string{query})
	if err != nil {
		return nil, err
	}
	denseHits, err := e.store.Search(ctx, kb.MilvusCollection, queryVecs[0], int(e.cfg.DenseTopK))
	if err != nil {
		return nil, fmt.Errorf("dense search: %w", err)
	}

	// Keyword ranking complements dense semantics with literal terms and CJK
	// bigrams. It runs independently of the vector similarity score scale.
	terms := extractTerms(query)
	kwHits, err := e.repo.KeywordSearch(ctx, kb.ID, terms, int(e.cfg.SparseTopK))
	if err != nil {
		// Continue with dense retrieval if keyword search is unavailable.
		kwHits = nil
	}

	fused := Fuse(denseHits, kwHits, e.cfg.RRFK, int(e.cfg.FusedTopN))

	// Dense search supplies IDs rather than full records. Load missing text before
	// reranking so candidates are scored against their actual content; keyword hits
	// already carry text and do not need another lookup.
	needLookup := make([]int64, 0, len(fused))
	for _, f := range fused {
		if f.Chunk.Content == "" {
			needLookup = append(needLookup, f.Chunk.ID)
		}
	}
	if len(needLookup) > 0 {
		// Hydration is best effort. A failed lookup leaves the corresponding candidate
		// without text rather than discarding all fused results.
		if found, err := e.repo.ListByIDs(ctx, needLookup); err == nil {
			byID := make(map[int64]model.DocumentChunk, len(found))
			for _, ch := range found {
				byID[ch.ID] = ch
			}
			for i := range fused {
				if ch, ok := byID[fused[i].Chunk.ID]; ok {
					fused[i].Chunk = ch
				}
			}
		}
	}

	var final []FusedChunk
	reranked := false
	// Reranking is optional. If its upstream call fails, preserve the RRF order.
	if rerankMdl := e.rerankMdl(); rerankMdl != nil && len(fused) > 0 {
		final, err = e.rerank(ctx, fused, query)
		if err != nil {
			final = fused
		} else {
			reranked = true
		}
	} else {
		final = fused
	}

	// The configured relevance threshold uses reranker scores, not the much smaller
	// RRF score scale. Retain the first three candidates when available, subject to
	// FinalTopK, then apply the threshold to later reranked candidates.
	refs := make([]Ref, 0, len(final))
	for _, f := range final {
		if reranked && e.cfg.ScoreThreshold > 0 && f.Score < e.cfg.ScoreThreshold && len(refs) >= 3 {
			break
		}
		if len(refs) >= int(e.cfg.FinalTopK) {
			break
		}
		refs = append(refs, Ref{
			ChunkID:    f.Chunk.ID,
			DocumentID: f.Chunk.DocumentID,
			Filename:   f.Filename,
			Page:       chunkPage(f.Chunk),
			Content:    f.Chunk.Content,
			Score:      f.Score,
		})
	}
	return refs, nil
}

// BuildContext gives retrieved passages stable one-based citation labels in the
// order returned. The service combines this text with the agent persona and memory
// before sending the system prompt to the model.
func BuildContext(refs []Ref) string {
	if len(refs) == 0 {
		return ""
	}
	out := "The following references were retrieved from the knowledge base. Cite them in your answer using [source number]:\n"
	for i, r := range refs {
		out += fmt.Sprintf("\n[%d] %s, page %d:\n%s\n", i+1, r.Filename, r.Page, r.Content)
	}
	return out
}

// rerank sends candidate texts to the selected model and maps returned indexes
// back to full chunk records. Invalid indexes are ignored rather than indexing
// outside the candidate slice.
func (e *Engine) rerank(ctx context.Context, fused []FusedChunk, query string) ([]FusedChunk, error) {
	docs := make([]string, 0, len(fused))
	for _, f := range fused {
		docs = append(docs, f.Chunk.Content)
	}
	results, err := e.gateway.Rerank(ctx, e.rerankMdl(), query, docs, len(docs))
	if err != nil {
		return nil, err
	}
	out := make([]FusedChunk, 0, len(results))
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(fused) {
			continue
		}
		f := fused[r.Index]
		f.Score = r.Score
		out = append(out, f)
	}
	return out, nil
}
