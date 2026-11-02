// Inbound A2A adaptation delegates text execution to an injected application callback.
// The protocol layer owns discovery and result envelopes; it does not own chat
// persistence, model selection, authentication policy, or background task storage.
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// AgentFunc synchronously executes the concatenated text parts for a request.
// Its context comes from net/http and its returned error becomes a failed task result.
type AgentFunc func(ctx context.Context, text string) (string, error)

// Server publishes one discovery card and invokes one execution callback.
// The atomic counter distinguishes locally generated task IDs within this process.
type Server struct {
	card     *AgentCard
	handler  AgentFunc
	nextTask int64
}

// NewServer binds discovery metadata and execution without starting a listener.
func NewServer(card *AgentCard, handler AgentFunc) *Server {
	return &Server{card: card, handler: handler}
}

// CardHandler serializes the configured discovery card as JSON.
func (s *Server) CardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

// RPCHandler accepts POSTed message/send and message/ping calls.
// It concatenates text parts, waits for the application callback, and emits a
// completed or failed task. Unsupported methods and invalid payloads use RPC errors.
func (s *Server) RPCHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case MethodMessageSend:
		var p SendMessageParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeRPCError(w, req.ID, -32602, "invalid params")
			return
		}
		text := ""
		for _, part := range p.Message.Parts {
			if part.Type == "text" {
				text += part.Text
			}
		}

		answer, err := s.handler(r.Context(), text)
		if err != nil {
			task := Task{
				ID:    s.newTaskID(),
				State: StateFailed,
			}
			writeRPCResult(w, req.ID, task, err.Error())
			return
		}
		task := Task{
			ID:    s.newTaskID(),
			State: StateCompleted,
			Messages: []Message{{
				Role:  RoleAgent,
				Parts: []Part{{Type: "text", Text: answer}},
			}},
			Artifacts: []Artifact{{
				Name:  "answer",
				Parts: []Part{{Type: "text", Text: answer}},
			}},
		}
		writeRPCResult(w, req.ID, task, "")

	case MethodMessageProbe:
		writeRPCResult(w, req.ID, map[string]any{"status": "alive", "time": time.Now()}, "")

	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

// newTaskID combines a timestamp and atomic process-local sequence number.
func (s *Server) newTaskID() string {
	n := atomic.AddInt64(&s.nextTask, 1)
	return fmt.Sprintf("task-%d-%d", time.Now().UnixMilli(), n)
}

// writeRPCResult preserves the request ID and task result. A callback failure
// can add an error member alongside the failed-task result in this implementation.
func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any, errMsg string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	if errMsg != "" {
		resp["error"] = map[string]any{"code": -32000, "message": errMsg}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeRPCError encodes a protocol-level failure with the original request ID
// when available; parse failures pass a null ID.
func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	})
}

// RegisterRoutes attaches discovery and /a2a handlers to the supplied mux.
// The caller owns listener startup, exposure, and shutdown.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/agent.json", s.CardHandler)
	mux.HandleFunc("/a2a", s.RPCHandler)
}
