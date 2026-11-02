// The Milvus adapter converts application records into SDK columns and vector hits.
// Memory collections have user/Agent fields; other collections use the KB schema.
package vector

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/chengpeng-cp/nexus-agent/internal/infra"
)

// MilvusStore adapts one reusable SDK client to Store operations.
type MilvusStore struct {
	cli client.Client
}

// NewMilvusStore wraps an initialized client without creating collections.
func NewMilvusStore(cli client.Client) *MilvusStore { return &MilvusStore{cli: cli} }

// EnsureCollection returns immediately for an existing collection. Otherwise it
// selects the memory or KB schema, creates a cosine HNSW index, and loads the
// collection. It does not validate or migrate the schema of an existing collection.
func (s *MilvusStore) EnsureCollection(ctx context.Context, collection string, dim int) error {
	has, err := s.cli.HasCollection(ctx, collection)
	if err != nil {
		return fmt.Errorf("has collection: %w", err)
	}
	if has {
		return nil
	}

	var schema *entity.Schema
	if collection == "memories" {
		schema = infra.NewMemorySchema(collection, dim)
	} else {
		schema = infra.NewKBSchema(collection, dim)
	}
	if err := s.cli.CreateCollection(ctx, schema, 2); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	idx, err := infra.NewHNSWIndex(dim)
	if err != nil {
		return fmt.Errorf("hnsw index: %w", err)
	}

	if err := s.cli.CreateIndex(ctx, collection, vectorField(collection), idx, false); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return s.cli.LoadCollection(ctx, collection, false)
}

// vectorField chooses vector for the shared memory collection and dense for KBs.
func vectorField(collection string) string {
	if collection == "memories" {
		return "vector"
	}
	return KBDenseField
}

// Upsert constructs schema-specific scalar/vector columns, calls SDK Insert,
// and flushes the collection. This implementation does not issue a native upsert
// or delete older rows first; callers must account for that write behavior.
func (s *MilvusStore) Upsert(ctx context.Context, collection string, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(records))
	vectors := make([][]float32, 0, len(records))
	metas := make([]map[string]any, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.ID)
		vectors = append(vectors, r.Vector)
		metas = append(metas, r.Metadata)
	}

	cols := []entity.Column{entity.NewColumnInt64("id", ids)}

	if collection == "memories" {
		uids := i64FromMeta(metas, "user_id")
		aids := i64FromMeta(metas, "agent_id")
		cols = append(cols, entity.NewColumnInt64("user_id", uids))
		cols = append(cols, entity.NewColumnInt64("agent_id", aids))
	} else {
		cols = append(cols,
			entity.NewColumnInt64("doc_id", i64FromMeta(metas, "doc_id")),
			entity.NewColumnInt32("seq", i32FromMeta(metas, "seq")),
			entity.NewColumnInt32("page", i32FromMeta(metas, "page")),
			entity.NewColumnVarChar("content", strFromMeta(metas, "content")),
		)
	}
	cols = append(cols, entity.NewColumnFloatVector(vectorField(collection), len(records[0].Vector), vectors))

	if _, err := s.cli.Insert(ctx, collection, "", cols...); err != nil {
		return fmt.Errorf("insert vectors: %w", err)
	}
	return s.cli.Flush(ctx, collection, false)
}

const KBDenseField = "dense"

// Search issues one cosine nearest-neighbor query with bounded consistency and
// HNSW ef=64. It returns IDs and scores only; malformed ID columns are skipped
// and document or memory content is resolved separately by the application.
func (s *MilvusStore) Search(ctx context.Context, collection string, vector []float32, topK int) ([]SearchHit, error) {
	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return nil, fmt.Errorf("search param: %w", err)
	}

	results, err := s.cli.Search(ctx, collection, nil, "",
		[]string{"id"}, []entity.Vector{entity.FloatVector(vector)},
		vectorField(collection), entity.COSINE, topK, sp,
		client.WithSearchQueryConsistencyLevel(entity.ClBounded),
	)
	if err != nil {
		return nil, fmt.Errorf("milvus search: %w", err)
	}

	hits := make([]SearchHit, 0, topK)
	for _, r := range results {
		col, ok := r.IDs.(*entity.ColumnInt64)
		if !ok {
			continue
		}
		for i := 0; i < col.Len() && i < len(r.Scores); i++ {
			id, err := col.ValueByIdx(i)
			if err != nil {
				continue
			}
			hits = append(hits, SearchHit{
				Record: Record{ID: id},
				Score:  r.Scores[i],
			})
		}
	}
	return hits, nil
}

// Delete removes the supplied primary keys through an id-in expression.
// An empty ID set is a no-op and no SQL rows are changed by this operation.
func (s *MilvusStore) Delete(ctx context.Context, collection string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	expr := fmt.Sprintf("id in %s", fmtInt64Slice(ids))
	return s.cli.Delete(ctx, collection, "", expr)
}

// DropCollection first checks existence so an already-missing collection is a no-op.
func (s *MilvusStore) DropCollection(ctx context.Context, collection string) error {
	has, err := s.cli.HasCollection(ctx, collection)
	if err != nil || !has {
		return err
	}
	return s.cli.DropCollection(ctx, collection)
}

// Count reads the collection aggregate count and returns zero for an empty result.
func (s *MilvusStore) Count(ctx context.Context, collection string) (int64, error) {
	rows, err := s.cli.Query(ctx, collection, nil, "", []string{"count(*)"})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	return rows[0].(*entity.ColumnInt64).ValueByIdx(0)
}

// i64FromMeta extracts int64 metadata columns, leaving missing or differently typed values as zero.
func i64FromMeta(metas []map[string]any, key string) []int64 {
	out := make([]int64, len(metas))
	for i, m := range metas {
		if v, ok := m[key].(int64); ok {
			out[i] = v
		}
	}
	return out
}

// i32FromMeta converts int and int64 metadata to the int32 scalar columns required by KB schemas.
func i32FromMeta(metas []map[string]any, key string) []int32 {
	out := make([]int32, len(metas))
	for i, m := range metas {
		switch v := m[key].(type) {
		case int:
			out[i] = int32(v)
		case int64:
			out[i] = int32(v)
		}
	}
	return out
}

// strFromMeta extracts string metadata, retaining an empty value for missing fields.
func strFromMeta(metas []map[string]any, key string) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		if v, ok := m[key].(string); ok {
			out[i] = v
		}
	}
	return out
}

// fmtInt64Slice serializes trusted integer IDs into the list syntax used by Milvus filters.
func fmtInt64Slice(ids []int64) string {
	b := "["
	for i, id := range ids {
		if i > 0 {
			b += ","
		}
		b += fmt.Sprintf("%d", id)
	}
	return b + "]"
}
