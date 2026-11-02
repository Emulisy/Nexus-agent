// Milvus connection and schema helpers define the vector layout used by the store.
// SQL records remain authoritative for ownership and document/memory text.
package infra

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/Emulisy/SupaAgent/internal/config"
)

// NewMilvus connects through the SDK using the configured gRPC address.
// Collection creation and loading are separate operations in the vector store.
func NewMilvus(ctx context.Context, cfg *config.Milvus) (client.Client, error) {
	cli, err := client.NewClient(ctx, client.Config{
		Address: cfg.Addr,
	})
	if err != nil {
		return nil, fmt.Errorf("new milvus client: %w", err)
	}
	return cli, nil
}

// KBVectorField is the dense field name shared by KB collection creation and search.
const KBVectorField = "dense"

// NewKBSchema uses SQL chunk IDs as primary keys and keeps document, sequence,
// page, and text metadata beside the dense vector. dim must match the embeddings
// written into this collection.
func NewKBSchema(collection string, dim int) *entity.Schema {
	return entity.NewSchema().WithName(collection).WithDescription("nexus-agent knowledge base").
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeInt64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("doc_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("seq").WithDataType(entity.FieldTypeInt32)).
		WithField(entity.NewField().WithName("content").WithDataType(entity.FieldTypeVarChar).WithMaxLength(8192)).
		WithField(entity.NewField().WithName("page").WithDataType(entity.FieldTypeInt32)).
		WithField(entity.NewField().WithName(KBVectorField).
			WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dim)))
}

// NewMemorySchema separates user/Agent-scoped memory vectors from KB collections.
// Its primary keys refer to SQL memory rows; the vector field is named vector.
func NewMemorySchema(collection string, dim int) *entity.Schema {
	return entity.NewSchema().WithName(collection).WithDescription("nexus-agent long-term memory").
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeInt64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("user_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("agent_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("vector").
			WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dim)))
}

// NewHNSWIndex creates a cosine HNSW index with M=16 and efConstruction=200.
// The vector dimension belongs to the collection schema rather than these index settings.
func NewHNSWIndex(dim int) (entity.Index, error) {
	return entity.NewIndexHNSW(entity.COSINE, 16, 200)
}
