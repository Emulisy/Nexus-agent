package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chengpeng-cp/nexus-agent/internal/llm"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

// ToolRepo resolves stored descriptions, parameter schemas, and per-tool timeout
// metadata. Bindings may identify records by database ID or stable tool code.
type ToolRepo interface {
	GetByCode(ctx context.Context, code string) (*model.Tool, error)
	ListByCodes(ctx context.Context, codes []string) ([]model.Tool, error)
	ListByIDs(ctx context.Context, ids []int64) ([]model.Tool, error)
}

// ExecutorConfig supplies the fallback context deadline for handler execution.
type ExecutorConfig struct {
	DefaultTimeout time.Duration
}

// ProdExecutor joins database definitions with registered handlers and common
// timeout, argument decoding, and result formatting. It does not perform
// JSON Schema validation, binding authorization, or interactive tool approval.
type ProdExecutor struct {
	repo     ToolRepo
	registry Registry
	cfg      ExecutorConfig
}

// NewExecutor wires tool metadata, handler lookup, and a default timeout.
func NewExecutor(repo ToolRepo, reg Registry, cfg ExecutorConfig) *ProdExecutor {
	return &ProdExecutor{repo: repo, registry: reg, cfg: cfg}
}

// List resolves agent binding JSON into definitions advertised to the model.
// Bindings with codes are fetched by code; ID-only bindings are fetched by ID.
// The combined records are deduplicated by code. Binding Config values are parsed
// for format compatibility but are not merged into invocation arguments here.
func (e *ProdExecutor) List(agentToolsJSON string) ([]llm.ToolDef, error) {
	bindings, err := ParseAgentBindings(agentToolsJSON)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}

	ctx := context.Background()

	// Split lookup paths so both persisted formats can resolve without assuming
	// that a frontend-created binding already includes its stable tool code.
	ids := make([]int64, 0, len(bindings))
	codes := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if b.ToolCode != "" {
			codes = append(codes, b.ToolCode)
		} else {
			ids = append(ids, b.ToolID)
		}
	}
	tools, err := e.repo.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		byIDs, err := e.repo.ListByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		tools = append(tools, byIDs...)
	}

	defs := make([]llm.ToolDef, 0, len(tools))
	seen := map[string]bool{}
	for _, t := range tools {
		if seen[t.Code] {
			continue
		}
		seen[t.Code] = true
		// Invalid stored schema JSON falls back to a generic object schema instead
		// of preventing all tool definitions from being listed.
		var params map[string]any
		if jsonErr := json.Unmarshal(t.Parameters, &params); jsonErr != nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		defs = append(defs, llm.ToolDef{
			Name:        t.Code,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return defs, nil
}

// Invoke selects the stored timeout override when available,
// looks up a registered handler, and decodes argument JSON before execution.
// Ordinary handler failures are returned as observations so the model can adapt.
//
// agentToolsJSON is not currently consulted during invocation: schema advertising
// and registry dispatch are separate operations. The deadline is context-based,
// so a handler must honor cancellation or enforce its own execution boundary.
func (e *ProdExecutor) Invoke(ctx context.Context, agentToolsJSON, name, argsJSON string) ToolResult {
	start := time.Now()
	res := ToolResult{ToolName: name}

	// A tool-row timeout overrides the default. Missing metadata does not prevent
	// calling a handler that is already present in the registry.
	timeout := e.cfg.DefaultTimeout
	if t, err := e.repo.GetByCode(ctx, name); err == nil && t != nil && t.TimeoutMs > 0 {
		timeout = time.Duration(t.TimeoutMs) * time.Millisecond
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			res.Output = fmt.Sprintf(`{"error":"tool panic: %v"}`, r)
			res.Status = "error"
			res.Elapsed = time.Since(start).Milliseconds()
		}
	}()

	// The registry is the actual execution dispatch table; no handler means a
	// structured unknown-tool observation.
	handler, ok := e.registry.Get(name)
	if !ok {
		res.Output = fmt.Sprintf(`{"error":"unknown tool: %s"}`, name)
		res.Status = "error"
		res.Elapsed = time.Since(start).Milliseconds()
		return res
	}

	// Decode syntax here; semantic argument checks remain the handler responsibility.
	// An empty argument string is accepted as an empty/nil argument map.
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil && argsJSON != "" {
		res.Output = fmt.Sprintf(`{"error":"invalid tool args json: %v"}`, err)
		res.Status = "error"
		res.Elapsed = time.Since(start).Milliseconds()
		return res
	}

	// Pass the bounded context through to local handlers or remote protocol proxies.
	out, err := handler(runCtx, args, argsJSON)
	switch {
	case err == nil:
		res.Status = "ok"
	case runCtx.Err() != nil:
		res.Status = "timeout"
		err = errcode.ErrToolTimeout.WithCause(err)
	default:
		res.Status = "error"
	}
	if err != nil {
		if res.Status == "" {
			res.Status = "error"
		}

		// Preserve any explanatory output returned with an error; synthesize an error
		// object only when the handler supplied no observation text.
		if out == "" {
			out = fmt.Sprintf(`{"error": %q}`, err.Error())
		}
	}
	res.Output = out
	res.Elapsed = time.Since(start).Milliseconds()
	return res
}

// AgentToolBinding is the persisted agents.tools entry. ToolCode is optional
// for ID-only frontend bindings, and Config preserves per-binding JSON values.
type AgentToolBinding struct {
	ToolID   int64          `json:"tool_id"`
	ToolCode string         `json:"tool_code,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
}

// ParseAgentBindings treats empty, null, and [] as no tools. Other input must be
// a JSON array of bindings; malformed JSON becomes an application parameter error.
func ParseAgentBindings(raw string) ([]AgentToolBinding, error) {
	if raw == "" || raw == "[]" || raw == "null" {
		return nil, nil
	}
	var out []AgentToolBinding
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errcode.ErrInvalidParam.WithCause(fmt.Errorf("parse agent tools: %w", err))
	}
	return out, nil
}
