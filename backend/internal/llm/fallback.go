package llm

import (
	"sync"
	"time"
)

// CircuitBreaker keeps a fixed-size circular window of final call outcomes for
// one provider. State is local to this process; an open circuit rejects requests
// until its cooldown expires, then resumes collecting a fresh window.
type CircuitBreaker struct {
	mu     sync.Mutex
	window []bool
	// filled prevents zero-value slots from being treated as observed failures.
	filled  int
	pos     int
	size    int
	errRate float64
	openDur time.Duration
	// openUntil is the earliest time a request may pass; zero means initially closed.
	openUntil time.Time
}

// NewCircuitBreaker configures the sample window, failure-rate threshold, and
// cooldown duration. Non-positive window sizes use twenty outcomes.
func NewCircuitBreaker(windowSize int, errRate float64, openDuration time.Duration) *CircuitBreaker {
	if windowSize <= 0 {
		windowSize = 20
	}
	return &CircuitBreaker{
		window:    make([]bool, windowSize),
		size:      windowSize,
		errRate:   errRate,
		openDur:   openDuration,
		openUntil: time.Time{},
	}
}

// Allow checks only the cooldown deadline. It does not reserve a single probe
// request or perform a distributed half-open transition.
func (c *CircuitBreaker) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().After(c.openUntil)
}

// Record replaces the oldest sample and evaluates the error rate only after a
// full window has been collected. Opening resets the samples so a later recovery
// period is evaluated against new outcomes rather than stale failures.
func (c *CircuitBreaker) Record(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.window[c.pos] = success
	c.pos = (c.pos + 1) % c.size
	if c.filled < c.size {
		c.filled++
	}

	var fails int
	for _, ok := range c.window {
		if !ok {
			fails++
		}
	}
	if c.filled == c.size && float64(fails)/float64(c.size) >= c.errRate {
		c.openUntil = time.Now().Add(c.openDur)

		// Reset the observed window after opening; filled remains the authority for
		// whether enough new samples exist to evaluate another failure rate.
		for i := range c.window {
			c.window[i] = true
		}
		c.filled = 0
	}
}
