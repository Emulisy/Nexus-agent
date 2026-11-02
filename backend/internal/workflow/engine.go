// The workflow interpreter carries one state map through node execution. Completed
// node outputs are stored under node keys; approval nodes return a waiting result
// so services can persist state and decide when to resume.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/expr-lang/expr"

	"github.com/chengpeng-cp/nexus-agent/internal/llm"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
	"github.com/chengpeng-cp/nexus-agent/internal/rag/engine"
	"github.com/chengpeng-cp/nexus-agent/internal/tool"
)

// SubRunner executes a referenced workflow through service wiring, keeping the
// interpreter independent of workflow repositories and run lifecycle handling.
type SubRunner func(ctx context.Context, workflowID int64, input map[string]any) (map[string]any, error)

// Deps provides model calls, tools, retrieval, and service-owned lookup functions.
// Resolvers keep credentials and user access decisions outside the interpreter.
type Deps struct {
	Gateway *llm.Gateway
	Tools   tool.Executor
	RAG     *engine.Engine

	// ResolveModel returns primary/fallback descriptors for an alias, using the
	// platform default for an empty alias.
	ResolveModel func(ctx context.Context, alias string) (*llm.ModelDescriptor, *llm.ModelDescriptor, error)

	// GetKB supplies the knowledge-base record used by a kb node.
	GetKB func(ctx context.Context, kbID int64) (*model.KnowledgeBase, error)

	SubRunner SubRunner
}

// Engine interprets validated DSL nodes using injected application dependencies.
type Engine struct {
	deps Deps
}

// New binds execution dependencies; ParseDSL/Validate should run before Run.
func New(deps Deps) *Engine { return &Engine{deps: deps} }

// ExecResult contains the run status and current state snapshot. Node execution
// failures are represented by failed status plus Error; malformed graph lookups
// can instead be returned as the Go error alongside any collected step records.
type ExecResult struct {
	Status string
	Output map[string]any
	// WaitKey identifies the approval node when Status is waiting_approval.
	WaitKey string
	Error   string
}

// Run copies input entries into a new state map and begins at the DSL entry node.
// It returns per-node outcome records for persistence by the calling service.
func (e *Engine) Run(ctx context.Context, d *DSL, input map[string]any) (*ExecResult, map[string]model.WorkflowStepRun, error) {
	state := map[string]any{}
	for k, v := range input {
		state[k] = v
	}
	return e.drive(ctx, d, d.StartKey(), state)
}

// Resume continues after the first human node in declaration order using the
// provided saved state. It follows that node approval branch rather than executing
// the human node again, which would immediately pause the run. It does not select
// a persisted WaitKey independently of the definition.
func (e *Engine) Resume(ctx context.Context, d *DSL, state map[string]any) (*ExecResult, map[string]model.WorkflowStepRun, error) {
	var waitKey string
	for i := range d.Nodes {
		if d.Nodes[i].Type == "human" {
			waitKey = d.Nodes[i].Key
			break
		}
	}
	if waitKey == "" {
		return nil, nil, errcode.ErrWorkflowDSL.WithMsg("no human node to resume")
	}

	// Starting at the approval node would immediately pause again.
	return e.drive(ctx, d, d.Next(waitKey, true), state)
}

// drive runs nodes until the graph ends, a node fails, an approval is encountered,
// or the traversal bound is reached. Outputs become state[nodeKey] only after a
// non-waiting successful step. The bounded loop limits unexpected traversal even
// if a definition bypassed normal validation.
func (e *Engine) drive(ctx context.Context, d *DSL, start string, state map[string]any) (*ExecResult, map[string]model.WorkflowStepRun, error) {
	steps := map[string]model.WorkflowStepRun{}
	cur := start

	for i := 0; i < len(d.Nodes)*2+8 && cur != ""; i++ {
		n := e.node(d, cur)
		if n == nil {
			return nil, steps, errcode.ErrWorkflowDSL.WithMsg("node %q not found", cur)
		}
		res, cond, wait, err := e.executeNode(ctx, d, n, state)

		steps[cur] = e.snapshot(n, res, err)
		if err != nil {
			return &ExecResult{Status: model.RunStatusFailed, Error: err.Error(), Output: state}, steps, nil
		}
		// The service persists the returned state and step snapshot before requesting
		// approval. The interpreter does not block a goroutine waiting for a user.
		if wait {
			return &ExecResult{Status: model.RunStatusWaiting, WaitKey: cur, Output: state}, steps, nil
		}
		state[cur] = res
		cur = d.Next(cur, cond)
	}
	return &ExecResult{Status: model.RunStatusSucceeded, Output: state}, steps, nil
}

// node returns the definition entry for a key without copying the entire node.
func (e *Engine) node(d *DSL, key string) *Node {
	for i := range d.Nodes {
		if d.Nodes[i].Key == key {
			return &d.Nodes[i]
		}
	}
	return nil
}

// executeNode returns a result, condition routing value, approval-wait flag, and
// error. Node-specific executors share this tuple so graph traversal stays separate
// from each operation implementation.
func (e *Engine) executeNode(ctx context.Context, d *DSL, n *Node, state map[string]any) (val any, cond bool, wait bool, err error) {
	switch n.Type {
	case "llm":
		return e.runLLM(ctx, n, state)
	case "tool":
		return e.runTool(ctx, n, state)
	case "kb":
		return e.runKB(ctx, n, state)
	case "condition":
		ok, e2 := evalExpr(n.Expr, state)
		if e2 != nil {
			return nil, false, false, e2
		}
		return nil, ok, false, nil
	case "parallel":
		return e.runParallel(ctx, d, n, state)
	// Return the rendered approval prompt as step output; drive exposes a waiting
	// result for service-side persistence and approval handling.
	case "human":
		return RenderTemplate(n.Prompt, state), false, true, nil
	case "subflow":
		return e.runSubflow(ctx, n, state)
	default:
		return nil, false, false, errcode.ErrWorkflowDSL.WithMsg("unsupported node type: %s", n.Type)
	}
}

// runLLM resolves a model alias, renders state into the prompt, and performs one
// non-streaming model call. A positive TimeoutS adds a node-local deadline; results
// are stored only after the complete response is available.
func (e *Engine) runLLM(ctx context.Context, n *Node, state map[string]any) (any, bool, bool, error) {
	if e.deps.ResolveModel == nil {
		return nil, false, false, errcode.ErrNotImplement.WithMsg("model resolver not configured")
	}
	primary, fallback, err := e.deps.ResolveModel(ctx, n.Model)
	if err != nil {
		return nil, false, false, err
	}

	prompt := RenderTemplate(n.Prompt, state)
	if n.TimeoutS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(n.TimeoutS)*time.Second)
		defer cancel()
	}
	resp, _, err := e.deps.Gateway.Chat(ctx, primary, fallback, &llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, false, false, err
	}
	return resp.Content, false, false, nil
}

// runTool renders string argument values and reuses the shared tool executor.
// It passes an empty agent binding list because workflow nodes identify a tool
// directly; a non-ok tool status becomes a failed workflow node.
func (e *Engine) runTool(ctx context.Context, n *Node, state map[string]any) (any, bool, bool, error) {
	args := map[string]any{}
	for k, v := range n.Args {
		if s, ok := v.(string); ok {
			args[k] = RenderTemplate(s, state)
		} else {
			args[k] = v
		}
	}
	raw, _ := json.Marshal(args)
	res := e.deps.Tools.Invoke(ctx, "[]", n.ToolCode, string(raw))
	if res.Status != "ok" {
		return nil, false, false, errcode.ErrToolFailed.WithMsg("tool %s: %s", n.ToolCode, res.Output)
	}
	return res.Output, false, false, nil
}

// runKB retrieves the referenced knowledge base through GetKB, runs the shared
// hybrid retrieval engine, and returns its formatted context as this node output.
func (e *Engine) runKB(ctx context.Context, n *Node, state map[string]any) (any, bool, bool, error) {
	if e.deps.RAG == nil || e.deps.GetKB == nil {
		return nil, false, false, errcode.ErrNotImplement.WithMsg("rag engine not configured")
	}
	kb, err := e.deps.GetKB(ctx, n.KbID)
	if err != nil {
		return nil, false, false, errcode.ErrKBNotFound.WithCause(err)
	}
	query := RenderTemplate(n.Query, state)
	refs, err := e.deps.RAG.Retrieve(ctx, kb, query)
	if err != nil {
		return nil, false, false, err
	}
	return engine.BuildContext(refs), false, false, nil
}

// runParallel executes the named branch nodes once each in concurrent goroutines.
// All branches read the shared state without updating it; the parent joins their
// results into a map keyed by branch node. It does not independently traverse the
// outgoing edges of each branch node.
func (e *Engine) runParallel(ctx context.Context, d *DSL, n *Node, state map[string]any) (any, bool, bool, error) {
	type job struct {
		key string
		n   *Node
	}
	jobs := make([]job, 0, len(n.Branches))
	for _, key := range n.Branches {
		if bn := e.node(d, key); bn != nil {
			jobs = append(jobs, job{key: key, n: bn})
		}
	}
	if len(jobs) == 0 {
		return map[string]any{}, false, false, nil
	}

	type result struct {
		key string
		val any
		err error
	}
	// One buffered result per job lets every branch finish even before the join
	// loop starts receiving. Only the join goroutine writes the output map.
	ch := make(chan result, len(jobs))
	for _, j := range jobs {
		go func(j job) {
			val, _, _, err := e.executeNode(ctx, d, j.n, state)
			ch <- result{key: j.key, val: val, err: err}
		}(j)
	}

	out := map[string]any{}
	var firstErr error
	for range jobs {
		r := <-ch
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		out[r.key] = r.val
	}
	if firstErr != nil {
		return nil, false, false, firstErr
	}
	return out, false, false, nil
}

// runSubflow delegates the current state to the injected nested-workflow runner
// and exposes its returned output as one node result.
func (e *Engine) runSubflow(ctx context.Context, n *Node, state map[string]any) (any, bool, bool, error) {
	if e.deps.SubRunner == nil {
		return nil, false, false, errcode.ErrNotImplement.WithMsg("subflow runner not configured")
	}
	out, err := e.deps.SubRunner(ctx, n.Workflow, state)
	if err != nil {
		return nil, false, false, err
	}
	return out, false, false, nil
}

// evalExpr evaluates a boolean expression against current state with built-ins
// disabled. An empty expression is true, allowing an unconditional condition path.
func evalExpr(expression string, state map[string]any) (bool, error) {
	if expression == "" {
		return true, nil
	}
	program, err := expr.Compile(expression, expr.Env(state), expr.AsBool(), expr.DisableAllBuiltins())
	if err != nil {
		return false, errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("expr compile: %w", err))
	}
	out, err := expr.Run(program, state)
	if err != nil {
		return false, errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("expr run: %w", err))
	}
	ok, _ := out.(bool)
	return ok, nil
}

// snapshot creates a persistence record for the observed node outcome. Conditions
// have no result payload; other successful outputs are encoded as JSON. The service
// owns storing these records alongside the workflow run.
func (e *Engine) snapshot(n *Node, val any, err error) model.WorkflowStepRun {
	now := time.Now()
	s := model.WorkflowStepRun{
		NodeKey:   n.Key,
		NodeType:  n.Type,
		StartedAt: &now,
	}
	if err != nil {
		s.Status = model.RunStatusFailed
		s.Error = err.Error()
	} else {
		s.Status = model.RunStatusSucceeded
		if n.Type != "condition" {
			b, _ := json.Marshal(val)
			s.Output = b
		}
	}
	return s
}
