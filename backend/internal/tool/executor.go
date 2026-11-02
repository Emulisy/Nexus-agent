// Package tool defines model-facing tool contracts, registers local and remote
// handlers, and dispatches invocations under execution contexts. Stored tool rows
// provide descriptions, schemas, and timeout metadata; the registry supplies the
// actual implementation associated with each code.
package tool

import (
	"context"

	"github.com/Emulisy/SupaAgent/internal/llm"
)

// Executor exposes tool definitions to a runner and converts handler results to
// model-facing observations. Listing controls which schemas are advertised; the
// current production Invoke implementation does not enforce binding membership.
type Executor interface {
	// List resolves the agent binding JSON to definitions sent with a model request.
	List(agentToolsJSON string) ([]llm.ToolDef, error)

	// Invoke dispatches one tool name with the model argument JSON and a parent
	// execution context. Failures are ordinarily returned as ToolResult output.
	Invoke(ctx context.Context, agentToolsJSON, name, argsJSON string) ToolResult
}

// ToolResult carries the observation returned to the model plus metadata for
// stream events and decision traces. Output may be successful text or an encoded
// error; callers must inspect Status rather than infer success from its presence.
type ToolResult struct {
	Output string `json:"output"`
	// Elapsed is wall-clock execution time in milliseconds.
	Elapsed int64 `json:"elapsed_ms"`
	// Status is normally ok, error, or timeout.
	Status   string `json:"status"`
	ToolName string `json:"tool"`
}

// BuiltinHandler receives decoded arguments and their original JSON. The context
// provides cancellation/deadlines; handlers doing blocking work must observe it.
type BuiltinHandler func(ctx context.Context, args map[string]any, raw string) (string, error)

// Registry maps stable tool codes to executable handlers. Registering metadata in
// the database alone does not make an implementation available to Invoke.
type Registry interface {
	Register(code string, h BuiltinHandler)
	Get(code string) (BuiltinHandler, bool)
	Codes() []string
}
