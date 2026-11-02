package tool

import (
	"sort"
	"sync"
)

// registry protects handler registration and lookup with an RWMutex so service
// initialization or integration refreshes can coexist with tool dispatch.
type registry struct {
	mu       sync.RWMutex
	handlers map[string]BuiltinHandler
}

// NewRegistry creates an empty handler table; built-ins and integration proxies
// are registered by application wiring.
func NewRegistry() Registry {
	return &registry{handlers: map[string]BuiltinHandler{}}
}

// Register installs or replaces the implementation for a code. Replacement lets
// an integration refresh update its proxy without changing stored tool bindings.
func (r *registry) Register(code string, h BuiltinHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[code] = h
}

// Get returns a handler snapshot; execution happens after the read lock is released.
func (r *registry) Get(code string) (BuiltinHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[code]
	return h, ok
}

// Codes returns a sorted copy of registered names for deterministic enumeration
// without exposing the mutable handler map.
func (r *registry) Codes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	codes := make([]string, 0, len(r.handlers))
	for c := range r.handlers {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}
