package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
)

// Gateway manages provider adapters and provider-local resilience controls.
// Services pass resolved ModelDescriptors, including decrypted credentials; the
// gateway does not query model tables or own credential storage.
//
// Concurrency limits, retry attempts, and circuit history are shared by requests
// with the same provider code within this process.
type Gateway struct {
	mu         sync.RWMutex
	providers  map[string]Provider
	breakers   map[string]*CircuitBreaker
	semaphores map[string]*Semaphore
	retryCfg   RetryConfig
	cfg        *config.LLM
}

// NewGateway initializes lazy provider caches and the default retry policy.
func NewGateway(cfg *config.LLM) *Gateway {
	return &Gateway{
		providers:  map[string]Provider{},
		breakers:   map[string]*CircuitBreaker{},
		semaphores: map[string]*Semaphore{},
		retryCfg:   DefaultRetryConfig(),
		cfg:        cfg,
	}
}

// providerFor creates an adapter, breaker, and semaphore once per provider code.
// The lock covers initialization so concurrent first requests share the same state.
func (g *Gateway) providerFor(code string) (Provider, *CircuitBreaker, *Semaphore) {
	g.mu.Lock()
	defer g.mu.Unlock()

	p, ok := g.providers[code]
	if !ok {
		p = NewProvider()
		g.providers[code] = p
	}
	b, ok := g.breakers[code]
	if !ok {
		b = NewCircuitBreaker(20, g.cfg.CircuitErrorRate, g.cfg.CircuitOpen)
		g.breakers[code] = b
	}
	s, ok := g.semaphores[code]
	if !ok {
		s = NewSemaphore(g.cfg.MaxConnsPerProvider)
		g.semaphores[code] = s
	}
	return p, b, s
}

// runWithGuard rejects an open circuit, then applies retry and concurrency guards
// to the provider operation. It records one final outcome after all retry attempts.
// For streaming calls, this operation ends when the HTTP stream has been opened,
// so the semaphore does not cover subsequent stream consumption.
func (g *Gateway) runWithGuard(ctx context.Context, desc *ModelDescriptor, fn func(p Provider) error) error {
	p, breaker, sem := g.providerFor(desc.ProviderCode)

	// Reject without contacting the provider; the outer chat call may choose a
	// different provider as fallback.
	if !breaker.Allow() {
		return ErrCircuitOpen
	}

	// Acquire a slot for each attempt, releasing it before any retry backoff.
	err := RetryDo(ctx, g.retryCfg, func(ctx context.Context) error {
		if aErr := sem.Acquire(ctx); aErr != nil {
			return aErr
		}
		defer sem.Release()
		return fn(p)
	})

	breaker.Record(err == nil)
	return err
}

// Chat performs a non-streaming call and returns the descriptor that succeeded.
// After a primary failure, it attempts an optional fallback only when its provider
// code differs, avoiding another request through the same provider circuit.
func (g *Gateway) Chat(ctx context.Context, primary, fallback *ModelDescriptor, req *ChatRequest) (*ChatResponse, *ModelDescriptor, error) {

	resp, used, err := g.chatOnce(ctx, primary, req)
	if err == nil {
		return resp, used, nil
	}

	if fallback != nil && fallback.ProviderCode != primary.ProviderCode {
		resp2, used2, err2 := g.chatOnce(ctx, fallback, req)
		if err2 == nil {
			return resp2, used2, nil
		}
		err = fmt.Errorf("primary: %w; fallback: %w", err, err2)
	}
	return nil, nil, ModelError(err)
}

// chatOnce executes one model through provider guards. Retry is handled inside runWithGuard rather than by this method.
func (g *Gateway) chatOnce(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (*ChatResponse, *ModelDescriptor, error) {
	var resp *ChatResponse
	err := g.runWithGuard(ctx, desc, func(p Provider) error {
		var e error
		resp, e = p.Chat(ctx, desc, req)
		return e
	})
	if err != nil {
		return nil, nil, err
	}
	return resp, desc, nil
}

// ChatStream opens an event stream and returns the descriptor that opened it.
// Fallback applies only when opening the HTTP request fails; errors after a
// successful response cannot be replayed or switched to another provider here.
func (g *Gateway) ChatStream(ctx context.Context, primary, fallback *ModelDescriptor, req *ChatRequest) (<-chan StreamEvent, *ModelDescriptor, error) {

	events, err := g.streamOnce(ctx, primary, req)
	if err == nil {
		return events, primary, nil
	}

	if fallback != nil && fallback.ProviderCode != primary.ProviderCode {
		events2, err2 := g.streamOnce(ctx, fallback, req)
		if err2 == nil {
			return events2, fallback, nil
		}
		err = fmt.Errorf("primary: %w; fallback: %w", err, err2)
	}
	return nil, nil, ModelError(err)
}

// streamOnce applies guards while opening a provider stream. Later stream events
// are consumed outside those guards and do not change circuit history.
func (g *Gateway) streamOnce(ctx context.Context, desc *ModelDescriptor, req *ChatRequest) (<-chan StreamEvent, error) {
	var events <-chan StreamEvent
	err := g.runWithGuard(ctx, desc, func(p Provider) error {
		var e error
		events, e = p.ChatStream(ctx, desc, req)
		return e
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

// Embed sends a batch through the same provider guards used for chat. It has no
// fallback descriptor; the caller decides whether embedding failure is recoverable.
func (g *Gateway) Embed(ctx context.Context, desc *ModelDescriptor, texts []string) ([][]float32, error) {

	var vectors [][]float32
	err := g.runWithGuard(ctx, desc, func(p Provider) error {
		var e error
		vectors, e = p.Embed(ctx, desc, texts)
		return e
	})
	return vectors, err
}

// Rerank scores candidate documents through the configured provider. Retrieval
// callers can retain their fused order when this optional stage fails.
func (g *Gateway) Rerank(ctx context.Context, desc *ModelDescriptor, query string, docs []string, topN int) ([]RerankResult, error) {

	var results []RerankResult
	err := g.runWithGuard(ctx, desc, func(p Provider) error {
		var e error
		results, e = p.Rerank(ctx, desc, query, docs, topN)
		return e
	})
	return results, err
}

// ErrCircuitOpen indicates the provider cooldown has not expired.
var ErrCircuitOpen = fmt.Errorf("provider circuit open")

// ModelError preserves classified provider failures and translates transport or
// deadline errors without turning model failures into generic internal errors.
func ModelError(err error) *errcode.Error {
	var public *errcode.Error
	if errors.As(err, &public) {
		return public
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errcode.ErrModelTimeout.WithCause(err)
	}
	return errcode.ErrModelUnavail.WithCause(err)
}
