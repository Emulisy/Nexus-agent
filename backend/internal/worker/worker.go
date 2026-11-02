// Package worker consumes uploaded-document indexing jobs from Redis Streams.
// The API publishes only document IDs; workers fetch metadata from PostgreSQL and
// source bytes from MinIO, then call the shared RAG indexing pipeline.
//
// Consumer groups retain unacknowledged jobs for recovery. Processing failures are
// marked failed, copied to a dead-letter stream, and acknowledged immediately; the
// reclaim loop targets stalled/unacknowledged delivery rather than a retry counter.
package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/rag/engine"
	"github.com/chengpeng-cp/nexus-agent/internal/rag/loader"
	"github.com/chengpeng-cp/nexus-agent/internal/repo"
)

const (
	// StreamDocETL carries document IDs awaiting parsing and indexing.
	StreamDocETL = "stream:doc-etl"
	// DLQDocETL retains failed delivery IDs and error text for diagnosis.
	DLQDocETL = "stream:doc-etl:dlq"

	GroupDocETL = "g-docetl"

	// ConsumerName is shared by the current worker processes and goroutines.
	ConsumerName = "worker-1"
)

// Publisher implements the service job-publishing boundary without coupling
// request handlers to Redis Stream commands.
type Publisher struct {
	rdb *redis.Client
}

// NewPublisher binds job publication to the application Redis client.
func NewPublisher(rdb *redis.Client) *Publisher { return &Publisher{rdb: rdb} }

// PublishDocETL appends a document ID after its metadata and original object have
// been stored by the upload service. Indexing is performed by a separate worker.
func (p *Publisher) PublishDocETL(ctx context.Context, docID int64) error {
	return p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamDocETL,
		Values: map[string]any{"doc_id": strconv.FormatInt(docID, 10)},
	}).Err()
}

// Worker owns the queue client, document repositories, object storage, and indexer.
// Document jobs require a usable MinIO client because content is fetched by key.
type Worker struct {
	rdb    *redis.Client
	repos  *repo.Repos
	engine *engine.Engine
	minio  *minio.Client
	bucket string
	cfg    *config.Worker
}

// New wires one worker; Run starts its consumer and reclaim goroutines.
func New(rdb *redis.Client, repos *repo.Repos, eng *engine.Engine, mc *minio.Client, bucket string, cfg *config.Worker) *Worker {
	return &Worker{rdb: rdb, repos: repos, engine: eng, minio: mc, bucket: bucket, cfg: cfg}
}

// Run ensures the consumer group exists, launches configured concurrent consumers,
// and starts periodic pending-message reclamation. Non-positive configuration uses
// four consumers and a thirty-second reclaim interval. It blocks until cancellation.
func (w *Worker) Run(ctx context.Context) error {
	concurrency := w.cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	claimInterval := w.cfg.ClaimInterval
	if claimInterval <= 0 {
		claimInterval = 30 * time.Second
	}

	// Create the stream along with the group when starting with an empty Redis
	// instance. An existing group is expected and must not prevent worker startup.
	if err := w.rdb.XGroupCreateMkStream(ctx, StreamDocETL, GroupDocETL, "0").Err(); err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create group %s: %w", GroupDocETL, err)
	}

	for i := 0; i < concurrency; i++ {
		go w.consumeLoop(ctx, StreamDocETL, GroupDocETL, w.handleDocETL)
	}

	go w.claimLoop(ctx, StreamDocETL, GroupDocETL, w.handleDocETL, claimInterval)

	<-ctx.Done()
	return nil
}

// consumeLoop reads new group deliveries one at a time and sends them through the
// common process/acknowledgment path. Redis errors are logged with a short delay
// to avoid a tight failure loop.
func (w *Worker) consumeLoop(ctx context.Context, stream, group string, handler func(ctx context.Context, id int64) error) {
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: ConsumerName,
			Streams:  []string{stream, ">"},
			Count:    1,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if err != redis.Nil && ctx.Err() == nil {
				log.Printf("[worker] read %s: %v", stream, err)
				time.Sleep(time.Second)
			}
			continue
		}
		for _, s := range res {
			for _, msg := range s.Messages {
				w.process(ctx, stream, group, msg, handler)
			}
		}
	}
}

// claimLoop reassigns messages idle for at least three minutes to this consumer
// and processes them through the same handler. This recovers work abandoned before
// ACK; a long-running live job can also exceed that idle threshold.
func (w *Worker) claimLoop(ctx context.Context, stream, group string, handler func(ctx context.Context, id int64) error, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, _, err := w.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   stream,
				Group:    group,
				Consumer: ConsumerName,
				MinIdle:  3 * time.Minute,
				Start:    "0-0",
				Count:    10,
			}).Result()
			if err != nil {
				continue
			}
			for _, msg := range res {
				w.process(ctx, stream, group, msg, handler)
			}
		}
	}
}

// process validates the document ID, invokes indexing, and acknowledges successful
// or invalid messages. A handler failure takes the dead-letter path rather than
// being retried inline.
func (w *Worker) process(ctx context.Context, stream, group string, msg redis.XMessage, handler func(ctx context.Context, id int64) error) {
	docID, _ := strconv.ParseInt(fmt.Sprint(msg.Values["doc_id"]), 10, 64)
	if docID <= 0 {
		_ = w.rdb.XAck(ctx, stream, group, msg.ID).Err()
		return
	}
	if err := handler(ctx, docID); err != nil {
		log.Printf("[worker] doc-etl %d failed: %v", docID, err)
		w.deadLetter(ctx, stream, msg.ID, docID, err)
		return
	}
	_ = w.rdb.XAck(ctx, stream, group, msg.ID).Err()
}

// deadLetter marks the document failed, records a bounded database error, appends
// the original queue ID plus full error to the DLQ, then acknowledges the delivery.
// These operations are best effort rather than one Redis/database transaction.
func (w *Worker) deadLetter(ctx context.Context, stream, msgID string, docID int64, cause error) {
	doc, err := w.repos.KB.GetDoc(ctx, docID)
	if err != nil {
		return
	}
	doc.Status = model.DocStatusFailed
	doc.Error = truncateErr(cause)
	_ = w.repos.KB.UpdateDoc(ctx, doc)

	_ = w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: DLQDocETL,
		Values: map[string]any{
			"doc_id": strconv.FormatInt(docID, 10),
			"msg_id": msgID,
			"error":  cause.Error(),
		},
	}).Err()
	_ = w.rdb.XAck(ctx, stream, GroupDocETL, msgID).Err()
}

// handleDocETL loads document/KB metadata, reads and parses the source object,
// advances status, and delegates chunk/vector indexing. Already-ready documents
// are skipped so redelivery after a successful index does not duplicate that work.
func (w *Worker) handleDocETL(ctx context.Context, docID int64) error {
	repos := w.repos

	doc, err := repos.KB.GetDoc(ctx, docID)
	if err != nil {
		return fmt.Errorf("get doc %d: %w", docID, err)
	}
	if doc.Status == model.DocStatusReady {
		return nil
	}
	kb, err := repos.KB.GetKB(ctx, doc.KBID)
	if err != nil {
		return fmt.Errorf("get kb %d: %w", doc.KBID, err)
	}

	// The worker owns document status persistence; the loader only returns pages.
	doc.Status = model.DocStatusParsing
	_ = repos.KB.UpdateDoc(ctx, doc)

	data, err := w.getObject(ctx, doc.ObjectKey)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}

	// Select parsing by the original filename extension, retaining source pages for
	// chunk citation metadata.
	loaded, err := loader.Load(doc.Filename, data)
	if err != nil {
		return fmt.Errorf("parse %s: %w", doc.Filename, err)
	}
	doc.Status = model.DocStatusChunking
	_ = repos.KB.UpdateDoc(ctx, doc)

	// Indexing persists chunk IDs before requesting embeddings. A failure returns
	// through process to the failed-document and dead-letter handling path.
	if err := w.engine.IndexDocument(ctx, kb, doc, loaded); err != nil {
		return fmt.Errorf("index: %w", err)
	}

	doc.Status = model.DocStatusReady
	doc.Error = ""
	if err := repos.KB.UpdateDoc(ctx, doc); err != nil {
		return fmt.Errorf("update doc: %w", err)
	}
	log.Printf("[worker] doc-etl %d ready: chunks=%d tokens=%d", doc.ID, doc.ChunkCount, doc.TokenCount)
	return nil
}

// getObject downloads the original uploaded bytes by the stored object key.
// Object storage must be configured before a document job can succeed.
func (w *Worker) getObject(ctx context.Context, key string) ([]byte, error) {
	obj, err := w.minio.GetObject(ctx, w.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// isBusyGroup distinguishes the expected existing-group response from Redis errors.
func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

// truncateErr bounds the diagnostic stored in the document row to five hundred bytes.
func truncateErr(err error) string {
	s := err.Error()
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
