package llm

import "context"

// Provider adapts the operations understood by the gateway. Implementations own
// HTTP protocol conversion, stream channel closure, and transient-error
// classification; the gateway owns retries, fallback, and provider-local guards.
type Provider interface {
	// Chat returns a complete response or a synchronous request error.
	Chat(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (*ChatResponse, error)

	// ChatStream returns after the response opens. The implementation closes the
	// returned channel after consuming the response body.
	ChatStream(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (<-chan StreamEvent, error)

	// Embed preserves input order in the returned vector batch.
	Embed(ctx context.Context, desc *ModelDescriptor, texts []string) ([][]float32, error)

	// Rerank returns original candidate indexes with relevance scores.
	Rerank(ctx context.Context, desc *ModelDescriptor, query string, docs []string, topN int) ([]RerankResult, error)
}

// RerankResult identifies a source document by its original zero-based index.
// Scores are provider relevance values and need not match vector similarity.
type RerankResult struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
}

// NewProvider returns the OpenAI-compatible adapter shared by supported provider
// codes. The descriptor supplies each actual base URL, model name, and credential.
func NewProvider() Provider {
	return &openaiCompatibleProvider{}
}
