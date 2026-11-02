package llm

import (
	"context"
	"fmt"
)

// Semaphore bounds concurrent guarded operations using buffered channel slots.
// It is a concurrency limit, not a requests-per-second rate limiter.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates n slots, defaulting to sixteen for non-positive values.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		n = 16
	}
	return &Semaphore{ch: make(chan struct{}, n)}
}

// Acquire waits for one slot or returns the context error. Callers must release
// each successfully acquired slot exactly once.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("acquire semaphore: %w", ctx.Err())
	}
}

// Release returns one slot without blocking. An empty semaphore is left unchanged.
func (s *Semaphore) Release() {
	select {
	case <-s.ch:
	default:
	}
}
