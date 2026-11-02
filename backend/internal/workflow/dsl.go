// Package workflow parses YAML orchestration definitions and interprets their
// node graph. Definitions describe model, tool, retrieval, conditional, parallel,
// approval, and nested-workflow steps; services provide access-aware dependencies
// and persist run state when execution pauses or finishes.
package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

// DSL is the serializable workflow definition. Input names and descriptive fields
// are metadata; node execution reads values from the supplied state map. When Edges
// is omitted, validation links adjacent nodes in declaration order.
type DSL struct {
	Name  string   `yaml:"name" json:"name"`
	Desc  string   `yaml:"desc" json:"desc"`
	Input []string `yaml:"input" json:"input"`
	Nodes []Node   `yaml:"nodes" json:"nodes"`
	// Edges accepts explicit from/to mappings or a linear sequence of node keys.
	Edges EdgeList `yaml:"edges" json:"edges"`
	// OutKey preserves the optional output-node setting in stored definitions; the
	// current engine returns the state map rather than selecting this field.
	OutKey string `yaml:"out" json:"out"`
}

// Node stores parameters shared by the supported node kinds. Each executor reads
// only the fields relevant to its Type; Key identifies its result in workflow state.
type Node struct {
	Key  string `yaml:"key" json:"key"`
	Type string `yaml:"type" json:"type"`
	// Prompt supports literal {{stateKey}} replacement for llm and human nodes.
	Prompt string `yaml:"prompt" json:"prompt"`
	// Model is a service-resolved alias; an empty alias selects the default model.
	Model    string `yaml:"model" json:"model"`
	KbID     int64  `yaml:"kb_id" json:"kb_id"`
	Query    string `yaml:"query" json:"query"`
	ToolCode string `yaml:"tool_code" json:"tool_code"`
	// Args provides tool inputs; string values support state-template replacement.
	Args map[string]any `yaml:"args" json:"args"`
	// Expr is a boolean condition evaluated with workflow state as its environment.
	Expr string `yaml:"expr" json:"expr"`
	// Then and Else are explicit condition targets, preferred over outgoing edges.
	Then string `yaml:"then" json:"then"`
	Else string `yaml:"else" json:"else"`
	// Branches names nodes executed once each by a parallel node.
	Branches []string `yaml:"branches" json:"branches"`
	// Workflow identifies the nested workflow passed to the injected subrunner.
	Workflow int64 `yaml:"workflow" json:"workflow"`
	// TimeoutS applies an additional deadline to an llm node when positive.
	TimeoutS int `yaml:"timeout_s" json:"timeout_s"`
}

// Edge connects node keys. Label is retained in the serialized definition; current
// condition routing uses Node.Then/Else before falling back to the first edge.
type Edge struct {
	From  string `yaml:"from" json:"from"`
	To    string `yaml:"to" json:"to"`
	Label string `yaml:"label" json:"label"`
}

// EdgeList accepts either explicit mappings such as [{from: a, to: b}] or the
// linear form [a, b, c], which becomes adjacent edges a->b and b->c. Custom YAML
// unmarshaling keeps both representations compatible with the same graph type.
type EdgeList []Edge

// UnmarshalYAML decodes mapping entries directly, otherwise interprets entries
// as ordered node keys. An empty or single-key linear list creates no edges.
func (l *EdgeList) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode && len(value.Content) > 0 &&
		value.Content[0].Kind == yaml.MappingNode {
		var es []Edge
		if err := value.Decode(&es); err != nil {
			return err
		}
		*l = es
		return nil
	}

	var keys []string
	if err := value.Decode(&keys); err != nil {
		return err
	}
	es := make([]Edge, 0, max(len(keys)-1, 0))
	for i := 0; i+1 < len(keys); i++ {
		es = append(es, Edge{From: keys[i], To: keys[i+1]})
	}
	*l = es
	return nil
}

// ParseDSL decodes YAML and validates the graph before exposing it to execution.
// YAML and graph errors are wrapped with the workflow application error code.
func ParseDSL(text string) (*DSL, error) {
	var d DSL
	if err := yaml.Unmarshal([]byte(text), &d); err != nil {
		return nil, errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("yaml: %w", err))
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Validate requires a name, at least one typed node, unique non-empty keys, known
// explicit edge endpoints, and an acyclic traversal graph. It mutates Edges when
// supplying the omitted sequential form; it does not validate every executor input.
func (d *DSL) Validate() error {
	if d.Name == "" {
		return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("name is required"))
	}
	if len(d.Nodes) == 0 {
		return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("at least one node"))
	}

	keys := map[string]bool{}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.Key == "" {
			return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("node[%d] key required", i))
		}
		if keys[n.Key] {
			return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("duplicate node key: %s", n.Key))
		}
		keys[n.Key] = true
		if !validNodeType(n.Type) {
			return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("node %s: unknown type %q", n.Key, n.Type))
		}
	}

	// Use node order as the linear default so simple definitions need no edge list.
	if len(d.Edges) == 0 && len(d.Nodes) > 1 {
		for i := 0; i+1 < len(d.Nodes); i++ {
			d.Edges = append(d.Edges, Edge{From: d.Nodes[i].Key, To: d.Nodes[i+1].Key})
		}
	}
	for _, e := range d.Edges {
		if !keys[e.From] || !keys[e.To] {
			return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("edge %s->%s references unknown node", e.From, e.To))
		}
	}
	if err := d.checkCycle(); err != nil {
		return err
	}
	return nil
}

// checkCycle uses depth-first visitation colors over explicit edges and condition
// targets. An edge to a currently visiting node is a cycle; completed subgraphs
// are reused instead of traversed again.
func (d *DSL) checkCycle() error {
	adj := map[string][]string{}
	for _, e := range d.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.Type == "condition" {
			if n.Then != "" {
				adj[n.Key] = append(adj[n.Key], n.Then)
			}
			if n.Else != "" {
				adj[n.Key] = append(adj[n.Key], n.Else)
			}
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var dfs func(k string) error
	dfs = func(k string) error {
		color[k] = gray
		for _, next := range adj[k] {
			switch color[next] {
			case gray:
				return errcode.ErrWorkflowDSL.WithCause(fmt.Errorf("cycle detected at node %s", next))
			case white:
				if err := dfs(next); err != nil {
					return err
				}
			}
		}
		color[k] = black
		return nil
	}
	for i := range d.Nodes {
		if color[d.Nodes[i].Key] == white {
			if err := dfs(d.Nodes[i].Key); err != nil {
				return err
			}
		}
	}
	return nil
}

// Next prefers an explicit Then/Else target for a condition node. Otherwise it
// returns the first matching outgoing edge; an empty string terminates traversal.
func (d *DSL) Next(key string, condResult bool) string {
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.Key == key && n.Type == "condition" {
			target := n.Else
			if condResult {
				target = n.Then
			}
			if target != "" {
				return target
			}
		}
	}
	for _, e := range d.Edges {
		if e.From == key {
			return e.To
		}
	}
	return ""
}

// StartKey uses the first edge source, or the first node when there are no edges.
// Callers must validate first so the node list is non-empty.
func (d *DSL) StartKey() string {
	if len(d.Edges) > 0 {
		return d.Edges[0].From
	}
	return d.Nodes[0].Key
}

// validNodeType is the shared list of node kinds understood by executeNode.
func validNodeType(t string) bool {
	switch t {
	case "llm", "tool", "kb", "condition", "parallel", "human", "subflow":
		return true
	}
	return false
}

// RenderTemplate performs literal {{key}} replacement from top-level state keys.
// Unknown placeholders remain unchanged for inspection; this is not an expression
// engine and does not resolve dotted object paths.
func RenderTemplate(tpl string, state map[string]any) string {
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	out := tpl
	for k, v := range state {
		out = strings.ReplaceAll(out, "{{"+k+"}}", toStringVal(v))
	}
	return out
}

// toStringVal preserves strings, renders nil as empty text, and serializes other
// state values as YAML without a trailing newline for insertion into templates.
func toStringVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := yaml.Marshal(t)
		return strings.TrimRight(string(b), "\n")
	}
}
