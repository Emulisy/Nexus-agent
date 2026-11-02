// Command worker consumes document ETL jobs independently of the HTTP server.
// It uses the same database, vector store, encryption key, and model resolution
// as the API process. Redis stream consumption ends when the signal handler
// cancels the worker context.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/infra"
	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/logging"
	"github.com/Emulisy/SupaAgent/internal/rag/engine"
	"github.com/Emulisy/SupaAgent/internal/repo"
	"github.com/Emulisy/SupaAgent/internal/security"
	"github.com/Emulisy/SupaAgent/internal/service"
	"github.com/Emulisy/SupaAgent/internal/vector"
	"github.com/Emulisy/SupaAgent/internal/worker"
)

// main reports worker initialization or processing errors and exits unsuccessfully.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker exited: %v\n", err)
		os.Exit(1)
	}
}

// run creates storage clients and a RAG engine, then starts stream consumption.
// MinIO construction is optional at this layer; jobs that need stored object
// content still require a usable object client. Shutdown cancellation is passed
// to the worker rather than managed through an HTTP server.
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

	gateway := llm.NewGateway(&cfg.LLM)
	store := vector.NewMilvusStore(mcli)
	repos := repo.New(db)

	// Use the server master key to decrypt model credentials.
	encryptor, err := security.NewEncryptor(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("encryptor: %w", err)
	}

	// Reuse service model resolution for document embedding and reranking.
	svcs, err := service.New(service.Deps{
		Cfg: cfg, Repos: repos, Gateway: gateway, RDB: rdb, Vector: store, Encryptor: encryptor,
	})
	if err != nil {
		return fmt.Errorf("services: %w", err)
	}
	ragEngine := engine.New(gateway, store, repos.KB, &cfg.RAG, cfg.Milvus.Dim)
	ragEngine.SetModels(svcs.DefaultEmbedding, svcs.DefaultRerank)

	w := worker.New(rdb, repos, ragEngine, nil, "", &cfg.Worker)
	// Object storage is optional when document content is already available inline.
	if mc, err := infra.NewMinIO(ctx, &cfg.MinIO); err == nil {
		w = worker.New(rdb, repos, ragEngine, mc, cfg.MinIO.Bucket, &cfg.Worker)
	}

	logging.LogInfo("SupaAgent worker started",
		"concurrency", fmt.Sprint(cfg.Worker.Concurrency), "stream", worker.StreamDocETL)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		cancel()
	}()
	return w.Run(ctx)
}
