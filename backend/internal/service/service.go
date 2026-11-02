// Package service coordinates application use cases across storage and runtime components.
// Services perform ownership checks, translate persistence errors, and compose
// model resolution, retrieval, memory, and execution. HTTP handlers depend on
// this layer instead of assembling those dependencies per request.
package service

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
	"github.com/Emulisy/SupaAgent/internal/rag/engine"
	"github.com/Emulisy/SupaAgent/internal/repo"
	"github.com/Emulisy/SupaAgent/internal/security"
	"github.com/Emulisy/SupaAgent/internal/tool"
	"github.com/Emulisy/SupaAgent/internal/vector"
)

// Deps contains resources created by the process entry point.
// Optional publishers and executors may be absent in the document worker,
// which constructs services only to reuse model resolution.
type Deps struct {
	Cfg       *config.Config
	Repos     *repo.Repos
	Gateway   *llm.Gateway
	Encryptor *security.Encryptor
	JWT       *security.JWTManager
	RDB       *redis.Client
	Vector    vector.Store
	Executor  tool.Executor
	Registry  tool.Registry
	Streams   StreamPublisher
}

// StreamPublisher isolates document job submission from the Redis worker package.
// Publication is separate from the preceding database and object-store writes;
// the interface does not provide a transaction spanning those systems.
type StreamPublisher interface {
	PublishDocETL(ctx context.Context, docID int64) error
}

// Services shares infrastructure and exposes the application service instances.
// Model descriptors are resolved from current persisted settings rather than
// cached on this container. RAG/workflow engines are attached after construction
// to break their dependency on service-provided lookup callbacks.
type Services struct {
	Deps

	ragEngine *engine.Engine

	Auth     *AuthService
	Models   *ModelService
	Chat     *ChatService
	Agent    *AgentService
	KB       *KBService
	Workflow *WorkflowService
	MCP      *MCPService
	A2A      *A2AService
	Memory   *MemoryService
}

// New constructs the service graph after synchronizing the startup model catalog.
// Catalog changes run under the repository advisory lock and use encrypted provider
// credentials. Engines and storage callbacks that depend on the services are attached
// separately after construction; a startup synchronization failure is returned.
func New(d Deps) (*Services, error) {
	s := &Services{Deps: d}

	if err := s.bootstrapDefaultModels(context.Background()); err != nil {
		return nil, err
	}

	s.Auth = NewAuthService(d.Repos, d.JWT, d.RDB)
	s.Models = NewModelService(d.Repos, d.Encryptor)
	s.Chat = NewChatService(s)
	s.Agent = NewAgentService(d.Repos, d.Cfg)
	s.KB = NewKBService(s)
	s.Workflow = NewWorkflowService(s)
	s.MCP = NewMCPService(s)
	s.A2A = NewA2AService(s)
	s.Memory = NewMemoryService(s)
	return s, nil
}

// DefaultChat resolves current model and provider settings on every call.
// Disabled or invalid configurations stop being used immediately; failed resolution
// returns nil rather than a cached startup descriptor. Callers must handle that case.
func (s *Services) DefaultChat() *llm.ModelDescriptor {
	if d, err := s.resolveByName(s.Deps.Cfg.LLM.DefaultChat, modelTypeChat); err == nil {
		return d
	}
	return nil
}

// DefaultEmbedding resolves the configured embedding model from the live catalog.
// It returns nil when no eligible configuration is available, so callers cannot rely
// on a descriptor cached before an administrator changed or disabled a provider.
func (s *Services) DefaultEmbedding() *llm.ModelDescriptor {
	if d, err := s.resolveByName(s.Deps.Cfg.LLM.DefaultEmbedding, modelTypeEmbed); err == nil {
		return d
	}
	return nil
}

// DefaultRerank resolves the optional bge-reranker alias from the live catalog.
// A missing or unavailable model returns nil, allowing retrieval to keep its fused
// ranking instead of requiring a reranking provider.
func (s *Services) DefaultRerank() *llm.ModelDescriptor {
	d, _ := s.resolveByName("bge-reranker", modelTypeRerank)
	return d
}

// ResolveModelByAlias supplies model resolution to workflow nodes.
// An empty alias uses DefaultChat; named aliases are resolved from current
// configuration. This callback does not choose a separate fallback descriptor.
func (s *Services) ResolveModelByAlias(ctx context.Context, alias string) (*llm.ModelDescriptor, *llm.ModelDescriptor, error) {
	if alias == "" {
		if d := s.DefaultChat(); d != nil {
			return d, nil, nil
		}
		return nil, nil, errcode.ErrProviderNotFound.WithMsg("No default chat model is configured")
	}
	m, err := s.resolveByName(alias, modelTypeChat)
	if err != nil {
		return nil, nil, err
	}
	return m, nil, nil
}

// SetRAGEngine attaches retrieval after the engine has been constructed with repository dependencies.
func (s *Services) SetRAGEngine(e *engine.Engine) { s.ragEngine = e }
