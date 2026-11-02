// Command server assembles the API process and its external dependencies.
// Configuration, storage clients, model resolution, and tool execution are shared
// by the HTTP services. Engines that depend on those services are attached after
// construction. Database migrations and the document worker run as separate commands.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Emulisy/SupaAgent/internal/a2a"
	"github.com/Emulisy/SupaAgent/internal/api"
	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/infra"
	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/logging"
	"github.com/Emulisy/SupaAgent/internal/memory/long_term"
	"github.com/Emulisy/SupaAgent/internal/rag/engine"
	"github.com/Emulisy/SupaAgent/internal/repo"
	"github.com/Emulisy/SupaAgent/internal/security"
	"github.com/Emulisy/SupaAgent/internal/service"
	"github.com/Emulisy/SupaAgent/internal/tool"
	"github.com/Emulisy/SupaAgent/internal/vector"
	"github.com/Emulisy/SupaAgent/internal/worker"
	"github.com/Emulisy/SupaAgent/internal/workflow"
	"github.com/minio/minio-go/v7"
)

// main reports startup failures to stderr and exits with a nonzero status.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "server exited: %v\n", err)
		os.Exit(1)
	}
}

// run constructs dependencies before opening the HTTP listener.
// Storage and service initialization failures stop startup; the code
// sandbox is optional. A termination signal cancels the application context and
// gives the HTTP server the configured shutdown interval to drain requests.
func run() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	logging.Init(cfg.Log.Level, cfg.Log.Format)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := infra.NewPG(ctx, &cfg.PG)
	if err != nil {
		return fmt.Errorf("pg: %w", err)
	}
	rdb, err := infra.NewRedis(ctx, &cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	mcli, err := infra.NewMilvus(ctx, &cfg.Milvus)
	if err != nil {
		return fmt.Errorf("milvus: %w", err)
	}
	mio, err := infra.NewMinIO(ctx, &cfg.MinIO)
	if err != nil {
		return fmt.Errorf("minio: %w", err)
	}
	if err := mio.MakeBucket(ctx, cfg.MinIO.Bucket, minio.MakeBucketOptions{}); err != nil {
		if exists, _ := mio.BucketExists(ctx, cfg.MinIO.Bucket); !exists {
			return fmt.Errorf("minio bucket: %w", err)
		}
	}

	encryptor, err := security.NewEncryptor(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("encryptor: %w", err)
	}
	jwtMgr := security.NewJWTManager(cfg.JWT.Secret, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)

	gateway := llm.NewGateway(&cfg.LLM)
	store := vector.NewMilvusStore(mcli)

	repos := repo.New(db)
	reg := tool.NewRegistry()
	tool.RegisterSimple(reg)
	tool.RegisterWeb(reg, tool.SearchConfig{SearxngURL: cfg.Search.SearxngURL})
	tool.RegisterFiles(reg, tool.FileOpsConfig{BaseDir: "./data/files", MaxSize: 10 << 20})
	tool.RegisterDBQuery(reg, db)
	if cfg.Sandbox.Enabled {
		// Continue serving other tools when the code sandbox is unavailable.
		if sandbox, err := tool.NewSandbox(tool.SandboxConfig{
			Image:   cfg.Sandbox.Image,
			Timeout: cfg.Sandbox.Timeout,
		}); err != nil {
			logging.LogWarn("sandbox init failed, code_exec disabled", "err", err.Error())
		} else {
			tool.RegisterCodeExec(reg, sandbox)
		}
	}
	executor := tool.NewExecutor(repos.Tool, reg, tool.ExecutorConfig{DefaultTimeout: 30 * time.Second})

	svcs, err := service.New(service.Deps{
		Cfg: cfg, Repos: repos, Gateway: gateway,
		Encryptor: encryptor, JWT: jwtMgr, RDB: rdb,
		Vector: store, Executor: executor, Registry: reg,
		Streams: worker.NewPublisher(rdb),
	})
	if err != nil {
		return fmt.Errorf("services: %w", err)
	}

	ragEngine := engine.New(gateway, store, repos.KB, &cfg.RAG, cfg.Milvus.Dim)
	ragEngine.SetModels(svcs.DefaultEmbedding, svcs.DefaultRerank)
	svcs.SetRAGEngine(ragEngine)

	svcs.Workflow.SetEngine(workflow.New(workflow.Deps{
		Gateway:      gateway,
		Tools:        executor,
		RAG:          ragEngine,
		ResolveModel: svcs.ResolveModelByAlias,
		GetKB:        svcs.Workflow.GetWorkflowLookup(),
		SubRunner:    svcs.Workflow.SubflowRunner(),
	}))

	memoryMgr := long_term.NewManager(gateway, svcs.DefaultChat, svcs.DefaultEmbedding, repos.Memory, store)
	svcs.Chat.SetLongTermMemory(memoryMgr)
	svcs.Memory.SetManager(memoryMgr)

	svcs.KB.SetMinIO(mio, cfg.MinIO.Bucket)
	tool.RegisterKBSearch(reg, kbSearchFunc(svcs, ragEngine))

	h := &http.Server{
		Addr:              "127.0.0.1:" + cfg.HTTP.Port,
		Handler:           api.NewRouter(api.New(svcs), jwtMgr),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No write timeout: chat streams and synchronous workflows may be long-lived.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	if cfg.A2A.Enabled {
		startA2AServer(cfg, svcs)
	}

	logging.LogInfo("SupaAgent server started", "port", cfg.HTTP.Port, "env", cfg.Env)

	serverErr := make(chan error, 1)
	go func() { serverErr <- h.ListenAndServe() }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	select {
	case <-quit:
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	}
	logging.LogInfo("shutting down...")

	shutdownCtx, c := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer c()
	cancel()
	return h.Shutdown(shutdownCtx)
}

// startA2AServer exposes the local chat service on a separate net/http listener.
// The advertised card disables streaming because the handler collects chat deltas
// into one response. Listener failures are logged by its background goroutine.
func startA2AServer(cfg *config.Config, svcs *service.Services) {
	card := &a2a.AgentCard{
		Name:        cfg.A2A.Name,
		Description: cfg.A2A.Desc,
		URL:         fmt.Sprintf("http://127.0.0.1:%s/a2a", cfg.A2A.Port),
		Version:     "2.0.0",
		Protocol:    "0.2.9",
		Capabilities: struct {
			Streaming bool `json:"streaming"`
		}{Streaming: false},
		Skills: []a2a.AgentSkill{{Name: "chat", Description: "General conversation and tool calling"}},
	}
	handler := func(ctx context.Context, text string) (string, error) {
		return svcs.A2A.InvokeLocal(ctx, 0, text)
	}
	mux := http.NewServeMux()
	a2a.NewServer(card, handler).RegisterRoutes(mux)
	go func() {
		logging.LogInfo("a2a server listening", "port", cfg.A2A.Port)
		if err := http.ListenAndServe("127.0.0.1:"+cfg.A2A.Port, mux); err != nil {
			logging.LogError("a2a server exited", "err", err.Error())
		}
	}()
}

// kbSearchFunc adapts RAG retrieval to the model-facing kb_search tool.
// It requires an authenticated identity in context and loads the knowledge base
// through an ownership-filtered query. Result count follows the RAG configuration;
// the callback's topK argument is not applied here.
func kbSearchFunc(svcs *service.Services, eng *engine.Engine) tool.KBSearchFunc {
	return func(ctx context.Context, kbID int64, query string, topK int) (string, error) {
		userID, ok := security.UserIDFromContext(ctx)
		if !ok {
			return "", fmt.Errorf("authenticated user context is required")
		}
		kb, err := svcs.Repos.KB.GetKBForUser(ctx, kbID, userID)
		if err != nil {
			return "", err
		}
		refs, err := eng.Retrieve(ctx, kb, query)
		if err != nil {
			return "", err
		}
		var out string
		for _, r := range refs {
			out += fmt.Sprintf("[%s p%d] %s\n", r.Filename, r.Page, r.Content)
		}
		return out, nil
	}
}
