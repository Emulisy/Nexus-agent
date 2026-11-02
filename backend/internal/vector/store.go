// Package vector isolates retrieval and memory logic from the Milvus SDK.
// The interface uses SQL-compatible integer IDs so vector hits can be joined back
// to authoritative application records without duplicating repository behavior.
package vector

import "context"

// Record combines an application row ID with its embedding and schema-dependent metadata.
// The Milvus adapter extracts only fields supported by the selected collection.
type Record struct {
	// ID matches the originating SQL chunk or memory primary key.
	ID     int64     `json:"id"`
	Vector []float32 `json:"vector"`
	// Metadata supplies the scalar fields expected by the concrete collection schema.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SearchHit adds the backend similarity score to a matching ID. The current
// Milvus search returns IDs and scores; callers load text and ownership from SQL.
type SearchHit struct {
	Record
	// Score is cosine similarity in the current adapter; larger values rank closer.
	Score float32 `json:"score"`
}

// Store manages collection lifecycle, vector writes, and nearest-neighbor search.
// Collection dimensions must match the embedding model. Callers should not infer
// write idempotency solely from the Upsert name; it depends on the concrete adapter.
type Store interface {
	EnsureCollection(ctx context.Context, collection string, dim int) error

	Upsert(ctx context.Context, collection string, records []Record) error

	Search(ctx context.Context, collection string, vector []float32, topK int) ([]SearchHit, error)

	Delete(ctx context.Context, collection string, ids []int64) error

	DropCollection(ctx context.Context, collection string) error

	Count(ctx context.Context, collection string) (int64, error)
}
