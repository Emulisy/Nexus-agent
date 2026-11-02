// Package agent defines the transport-neutral events shared by runners and consumers.
// The HTTP layer writes Type as the SSE event name and JSON-encodes Data. Event
// constants describe the protocol vocabulary; not every runner emits every kind.
package agent

const (
	TypeMeta       = "meta"
	TypeDelta      = "delta"
	TypeReasoning  = "reasoning"
	TypePlan       = "plan"
	TypeToolCall   = "tool_call"
	TypeToolResult = "tool_result"
	TypeAgentEvent = "agent_event"
	TypeRefs       = "refs"
	TypeUsage      = "usage"
	TypeApproval   = "approval"
	TypeDone       = "done"
	TypeError      = "error"
)

// Event pairs a protocol event name with its JSON-ready payload. Payload field
// names are consumed directly by the frontend reducer and should remain stable.
type Event struct {
	Type string
	Data map[string]any
}

// Evt constructs a named event without copying its payload map. Callers should
// finish populating the map before publishing it to another goroutine.
func Evt(typ string, kv map[string]any) Event { return Event{Type: typ, Data: kv} }

// EvtDelta carries an incremental assistant text fragment to append to the response.
func EvtDelta(s string) Event { return Event{Type: TypeDelta, Data: map[string]any{"content": s}} }

// EvtReasoning carries optional provider-returned reasoning text separately from the answer.
func EvtReasoning(s string) Event {
	return Event{Type: TypeReasoning, Data: map[string]any{"content": s}}
}

// EvtToolCall exposes a call ID, registry name, and serialized JSON arguments.
// The matching result uses the same ID for frontend correlation.
func EvtToolCall(id, name, args string) Event {
	return Event{Type: TypeToolCall, Data: map[string]any{"id": id, "name": name, "args": args}}
}

// EvtToolResult reports a tool observation, elapsed milliseconds, and execution status.
func EvtToolResult(id, result string, ms int64, status string) Event {
	return Event{Type: TypeToolResult, Data: map[string]any{
		"id": id, "result": result, "ms": ms, "status": status,
	}}
}

// EvtAgentEvent carries delegation/progress metadata independently of answer text.
func EvtAgentEvent(typ, expert, detail string) Event {
	return Event{Type: TypeAgentEvent, Data: map[string]any{
		"type": typ, "expert": expert, "detail": detail,
	}}
}

// EvtUsage reports aggregate token counts, model label, and a formatted cost string.
// The current runner uses an unknown-cost value rather than calculating model prices.
func EvtUsage(prompt, completion int, cost string, model string) Event {
	return Event{Type: TypeUsage, Data: map[string]any{
		"prompt_tokens": prompt, "completion_tokens": completion, "cost": cost, "model": model,
	}}
}

// EvtDone marks successful runner completion with the upstream finish reason.
// The surrounding service may still persist response records before closing the stream.
func EvtDone(reason string) Event {
	return Event{Type: TypeDone, Data: map[string]any{"finish_reason": reason}}
}

// EvtError carries an application error code and its public English message.
func EvtError(code int, msg string) Event {
	return Event{Type: TypeError, Data: map[string]any{"code": code, "message": msg}}
}
