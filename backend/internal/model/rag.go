// RAG models separate KB ownership, original-document metadata, and searchable chunks.
// Original bytes live in object storage, text/metadata live in PostgreSQL, and
// chunk embeddings use matching IDs in the KB's Milvus collection.
package model

import (
	"time"

	"gorm.io/datatypes"
)

// KnowledgeBase binds ownership, chunk settings, embedding-model metadata, and a vector-collection name.
type KnowledgeBase struct {
	ID          int64  `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:128" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	UserID      int64  `gorm:"index" json:"user_id"`
	// EmbeddingConfigID is a required model reference; current engine callbacks resolve the platform embedding default.
	EmbeddingConfigID int64          `json:"embedding_config_id"`
	ChunkSize         int            `gorm:"default:512" json:"chunk_size"`
	ChunkOverlap      int            `gorm:"default:64" json:"chunk_overlap"`
	ParserConfig      datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"parser_config"`
	DocCount          int            `json:"doc_count"`
	// MilvusCollection is derived as kb_<id> after the KB receives its generated database ID.
	MilvusCollection string    `gorm:"size:128" json:"milvus_collection"`
	Status           string    `gorm:"size:20;default:active" json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Document tracks uploaded source metadata and ingestion progress independently of its parsed chunks.
type Document struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	KBID     int64  `gorm:"index:idx_documents_kb,priority:1" json:"kb_id"`
	Filename string `gorm:"size:256" json:"filename"`
	// FileType contains the normalized extension without its leading dot.
	FileType string `gorm:"size:16" json:"file_type"`
	// ObjectKey locates original bytes in the configured bucket for asynchronous ingestion.
	ObjectKey string `gorm:"size:512" json:"object_key"`
	FileSize  int64  `json:"file_size"`
	SourceURL string `gorm:"size:512" json:"source_url"`
	// Status records the current ETL stage so upload can return before indexing finishes.
	Status string `gorm:"size:20;index:idx_documents_kb,priority:2;default:pending" json:"status"`
	// Error holds the last ingestion failure description for the management interface.
	Error      string `gorm:"size:512" json:"error"`
	ChunkCount int    `json:"chunk_count"`
	TokenCount int    `json:"token_count"`
	// EnableGraph is persisted metadata; the current ingestion path does not build a graph index.
	EnableGraph bool           `json:"enable_graph"`
	Metadata    datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Document status values follow pending, parsing, chunking, embedding, then ready or failed.
const (
	DocStatusPending   = "pending"
	DocStatusParsing   = "parsing"
	DocStatusChunking  = "chunking"
	DocStatusEmbedding = "embedding"
	DocStatusReady     = "ready"
	DocStatusFailed    = "failed"
)

// DocumentChunk stores ordered searchable text; its ID also identifies the corresponding vector entry.
type DocumentChunk struct {
	ID         int64 `gorm:"primaryKey" json:"id"`
	KBID       int64 `gorm:"index" json:"kb_id"`
	DocumentID int64 `gorm:"index:idx_chunks_doc,priority:1" json:"document_id"`
	// Seq preserves source chunk ordering for document inspection.
	Seq        int    `json:"seq"`
	Content    string `gorm:"type:text" json:"content"`
	TokenCount int    `json:"token_count"`
	// Meta stores source-location details such as page and heading for citations.
	Meta     datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"meta"`
	VectorID string         `gorm:"size:64" json:"vector_id"`
	// Status 1 permits chunk retrieval by keyword search and dense-result text lookup.
	Status    int16     `gorm:"default:1" json:"status"`
	CreatedAt time.Time `gorm:"index:idx_chunks_doc,priority:2" json:"created_at"`
}
