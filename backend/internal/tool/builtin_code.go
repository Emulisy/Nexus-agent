package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// RegisterCodeExec installs code_exec with a Docker-backed execution boundary.
// Results are returned directly as tool observations.
func RegisterCodeExec(r Registry, sandbox *Sandbox) {
	r.Register("code_exec", func(ctx context.Context, args map[string]any, raw string) (string, error) {
		language, _ := args["language"].(string)
		code, _ := args["code"].(string)
		if language == "" || code == "" {
			return "", fmt.Errorf("language and code are required")
		}
		// A registered handler with no sandbox must fail rather than run code locally.
		if sandbox == nil {
			return "", fmt.Errorf("sandbox disabled (enable in config)")
		}

		result, err := sandbox.Run(ctx, language, code)
		if err != nil {
			return "", err
		}

		out, _ := json.Marshal(map[string]any{
			"exit_code": result.ExitCode,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"duration":  result.Duration.Milliseconds(),
		})
		return string(out), nil
	})
}

// KBSearchFunc is a service-provided retrieval callback. It keeps knowledge-base
// lookup and user access checks outside the generic tool package.
type KBSearchFunc func(ctx context.Context, kbID int64, query string, topK int) (string, error)

// RegisterKBSearch exposes knowledge-base retrieval to an agent tool round.
// The callback receives a numeric KB ID, query, and requested top-k after basic
// argument checks; service wiring determines how access and retrieval are handled.
func RegisterKBSearch(r Registry, fn KBSearchFunc) {
	r.Register("kb_search", func(ctx context.Context, args map[string]any, _ string) (string, error) {
		var kbID int64
		switch v := args["kb_id"].(type) {
		case float64:
			kbID = int64(v)
		case json.Number:
			kbID, _ = v.Int64()
		default:
			return "", fmt.Errorf("kb_id must be a number")
		}
		query, _ := args["query"].(string)
		topK := 5
		if v, ok := args["top_k"].(float64); ok && int(v) > 0 {
			topK = int(v)
		}
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		return fn(ctx, kbID, query, topK)
	})
}
