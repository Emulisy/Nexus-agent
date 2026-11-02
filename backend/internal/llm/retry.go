// The retry policy applies to explicitly classified transient failures. Streaming
// requests use it only while opening their response, before a stream is handed out.
package llm

import (
	"context"
	"math/rand"
	"time"
)

// RetryConfig controls attempts and exponential backoff. MaxAttempts includes
// the first call, and delays are jittered after applying the configured cap.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryConfig permits three attempts starting at 100 ms with a 2 s base
// backoff cap. The jitter adds up to one quarter of the capped delay.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}
}

// RetryDo stops immediately on success, cancellation, or a non-retryable error.
// Transient errors wait with exponential backoff before the next attempt; waiting
// also respects context cancellation. The last failure is returned on exhaustion.
func RetryDo(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if !IsRetryable(lastErr) {
			return lastErr
		}

		// Cap the exponential component before adding jitter to avoid synchronized
		// retries from concurrent requests.
		delay := cfg.BaseDelay << attempt
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		delay += time.Duration(rand.Int63n(int64(delay / 4)))
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}
