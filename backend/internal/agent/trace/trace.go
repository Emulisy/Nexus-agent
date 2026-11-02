// Agent decision records are persisted with assistant message metadata for timeline rendering.
// They are application records used by the chat interface.
package agenttrace

import (
	"sync"
	"time"
)

const (
	EventPlan    = "plan"
	EventThought = "thought"
	EventAction  = "action"
	EventObserve = "observe"
	EventDelegat = "delegate"
	EventMerge   = "merge"
	EventAnswer  = "answer"
)

// TraceStep describes a decision or action with a sequence number, optional tool
// arguments/result, elapsed milliseconds, status, and creation timestamp.
type TraceStep struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Agent   string         `json:"agent"`
	Content string         `json:"content"`
	Tool    string         `json:"tool,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Result  string         `json:"result,omitempty"`
	Elapsed int64          `json:"elapsed_ms"`
	Status  string         `json:"status,omitempty"`
	Time    time.Time      `json:"time"`
}

// AgentTraceCollector serializes Add, Done, and Snapshot through a mutex.
// Returned step pointers and nested Args maps must not be mutated concurrently
// with snapshotting; callers normally complete a step before appending the next one.
type AgentTraceCollector struct {
	mu    sync.Mutex
	steps []TraceStep
}

// NewAgentTrace creates an empty collector for a single Agent execution.
func NewAgentTrace() *AgentTraceCollector {
	return &AgentTraceCollector{}
}

// Add appends a timestamped step and returns a pointer used to attach tool details
// and completion data. Sequence numbers start at one in append order.
func (t *AgentTraceCollector) Add(typ, agentName, content string) *TraceStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	step := TraceStep{
		Seq:     len(t.steps) + 1,
		Type:    typ,
		Agent:   agentName,
		Content: content,
		Time:    time.Now(),
	}
	t.steps = append(t.steps, step)
	return &t.steps[len(t.steps)-1]
}

// Done records the result, final status, and elapsed time on a previously added step.
func (t *AgentTraceCollector) Done(step *TraceStep, result string, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	step.Result = result
	step.Status = status
	step.Elapsed = time.Since(step.Time).Milliseconds()
}

// Snapshot copies the step slice for serialization. Nested maps remain shared,
// so the copy is not a deep snapshot of concurrently mutable argument values.
func (t *AgentTraceCollector) Snapshot() []TraceStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TraceStep, len(t.steps))
	copy(out, t.steps)
	return out
}
