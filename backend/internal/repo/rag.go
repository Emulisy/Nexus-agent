// RAG persistence manages KB/document metadata and indexed text chunks.
// Owner-filtered KB methods are used by request-facing services, while ingestion
// uses direct IDs after job lookup. Vector operations are outside this repository;
// chunk IDs link PostgreSQL content to the corresponding Milvus entries.
package repo

import (
	"context"
	"math"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/rag/engine"
)

// KBRepo implements both management persistence and the retrieval engine's chunk repository.
type KBRepo struct {
	db *gorm.DB
}

// ---- KnowledgeBase ----

// CreateKB inserts metadata before the service derives the vector collection name from its generated ID.
func (r *KBRepo) CreateKB(ctx context.Context, kb *model.KnowledgeBase) error {
	return r.db.WithContext(ctx).Create(kb).Error
}

// UpdateKB saves complete KB metadata; creation uses it to store the derived collection name.
func (r *KBRepo) UpdateKB(ctx context.Context, kb *model.KnowledgeBase) error {
	return r.db.WithContext(ctx).Save(kb).Error
}

// DeleteKB deletes metadata; callers coordinate external vector cleanup before this operation.
func (r *KBRepo) DeleteKB(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.KnowledgeBase{}, id).Error
}

// GetKB loads a KB without ownership filtering for internal ingestion/processing paths.
func (r *KBRepo) GetKB(ctx context.Context, id int64) (*model.KnowledgeBase, error) {
	var kb model.KnowledgeBase
	if err := r.db.WithContext(ctx).First(&kb, id).Error; err != nil {
		return nil, err
	}
	return &kb, nil
}

// GetKBForUser requires both the KB ID and owning user ID, returning not-found for inaccessible rows.
func (r *KBRepo) GetKBForUser(ctx context.Context, id, userID int64) (*model.KnowledgeBase, error) {
	var kb model.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&kb).Error; err != nil {
		return nil, err
	}
	return &kb, nil
}

// ListKBs lists active KBs; a positive user ID adds ownership filtering, while zero leaves the query unscoped.
func (r *KBRepo) ListKBs(ctx context.Context, userID int64, offset, limit int) ([]model.KnowledgeBase, int64, error) {
	var (
		list  []model.KnowledgeBase
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.KnowledgeBase{}).Where("status = 'active'")
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListKBsByIDsForUser restricts agent-bound KB IDs to active records owned by the current user.
func (r *KBRepo) ListKBsByIDsForUser(ctx context.Context, ids []int64, userID int64) ([]model.KnowledgeBase, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.KnowledgeBase
	err := r.db.WithContext(ctx).Where("id IN ? AND user_id = ? AND status = 'active'", ids, userID).Find(&list).Error
	return list, err
}

// ---- Document ----

// CreateDoc inserts pending document metadata and provides the ID needed for its object-storage key.
func (r *KBRepo) CreateDoc(ctx context.Context, d *model.Document) error {
	return r.db.WithContext(ctx).Create(d).Error
}

// UpdateDoc saves document metadata, including the ETL stage, counts, and processing error.
func (r *KBRepo) UpdateDoc(ctx context.Context, d *model.Document) error {
	return r.db.WithContext(ctx).Save(d).Error
}

// GetDoc loads document metadata by ID; request-facing callers check its parent KB ownership separately.
func (r *KBRepo) GetDoc(ctx context.Context, id int64) (*model.Document, error) {
	var d model.Document
	if err := r.db.WithContext(ctx).First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDocs returns a KB's documents newest first, with a separate total-count query.
func (r *KBRepo) ListDocs(ctx context.Context, kbID int64, offset, limit int) ([]model.Document, int64, error) {
	var (
		list  []model.Document
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Document{}).Where("kb_id = ?", kbID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// DeleteDoc deletes the document row; external index cleanup remains the service/engine's responsibility.
func (r *KBRepo) DeleteDoc(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Document{}, id).Error
}

// BatchCreate inserts chunks in batches of 200 within one database transaction.
// Generated IDs are written into the supplied slice for vector indexing. A batch
// failure rolls back all chunk inserts from this call.
func (r *KBRepo) BatchCreate(ctx context.Context, chunks []model.DocumentChunk) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.CreateInBatches(chunks, 200).Error
	})
}

// ListByDoc returns all document chunks in sequence order for ingestion cleanup and inspection.
func (r *KBRepo) ListByDoc(ctx context.Context, docID int64) ([]model.DocumentChunk, error) {
	var list []model.DocumentChunk
	err := r.db.WithContext(ctx).Where("document_id = ?", docID).Order("seq ASC").Find(&list).Error
	return list, err
}

// DeleteByDoc removes the stored chunks for a document without touching its external vectors.
func (r *KBRepo) DeleteByDoc(ctx context.Context, docID int64) error {
	return r.db.WithContext(ctx).Delete(&model.DocumentChunk{}, "document_id = ?", docID).Error
}

// keywordCandidateLimit bounds the SQL candidate set scored in memory by keyword retrieval.
const keywordCandidateLimit = 500

// ListByIDs loads enabled chunks after dense retrieval; an empty input avoids an unnecessary IN query.
func (r *KBRepo) ListByIDs(ctx context.Context, ids []int64) ([]model.DocumentChunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.DocumentChunk
	err := r.db.WithContext(ctx).Where("id IN ? AND status = 1", ids).Find(&list).Error
	return list, err
}

// KeywordSearch uses OR-combined ILIKE terms to select a bounded candidate set.
// It scores candidates with term frequency and an approximate inverse frequency
// computed from that same pool, then returns the highest-scoring chunks with
// source filenames. This is not a full-corpus BM25 index.
func (r *KBRepo) KeywordSearch(ctx context.Context, kbID int64, terms []string, limit int) ([]engine.ScoredChunk, error) {
	if len(terms) == 0 {
		return nil, nil
	}

	var candidates []model.DocumentChunk
	q := r.db.WithContext(ctx).Where("kb_id = ? AND status = 1", kbID)
	conds := make([]string, 0, len(terms))
	args := make([]any, 0, len(terms))
	for _, t := range terms {
		conds = append(conds, "content ILIKE ?")
		args = append(args, "%"+t+"%")
	}
	if err := q.Where(strings.Join(conds, " OR "), args...).
		Limit(keywordCandidateLimit).Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	docIDs := make([]int64, 0, len(candidates))
	seen := map[int64]bool{}
	for _, c := range candidates {
		if !seen[c.DocumentID] {
			seen[c.DocumentID] = true
			docIDs = append(docIDs, c.DocumentID)
		}
	}
	var docs []model.Document
	if err := r.db.WithContext(ctx).Select("id, filename").Where("id IN ?", docIDs).Find(&docs).Error; err != nil {
		return nil, err
	}
	docNames := map[int64]string{}
	for _, d := range docs {
		docNames[d.ID] = d.Filename
	}

	n := float64(len(candidates))
	type hit struct {
		chunk model.DocumentChunk
		score float64
	}
	hits := make([]hit, 0, len(candidates))
	for _, c := range candidates {
		content := strings.ToLower(c.Content)
		var score float64
		for _, t := range terms {
			tf := float64(strings.Count(content, strings.ToLower(t)))
			if tf == 0 {
				continue
			}
			// Estimate smoothed inverse document frequency from the candidate pool.
			df := 0
			for _, c2 := range candidates {
				if strings.Contains(strings.ToLower(c2.Content), strings.ToLower(t)) {
					df++
				}
			}
			idf := math.Log(1 + n/float64(df+1))
			score += tf * idf
		}
		if score > 0 {
			hits = append(hits, hit{chunk: c, score: score})
		}
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]engine.ScoredChunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, engine.ScoredChunk{Chunk: h.chunk, Filename: docNames[h.chunk.DocumentID], Score: h.score})
	}
	return out, nil
}

// ListChunks pages one document's chunks in sequence order and returns its total count.
func (r *KBRepo) ListChunks(ctx context.Context, docID int64, offset, limit int) ([]model.DocumentChunk, int64, error) {
	var (
		list  []model.DocumentChunk
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.DocumentChunk{}).Where("document_id = ?", docID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("seq ASC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}
