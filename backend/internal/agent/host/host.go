// Package host routes a request to one specialist selected by a model. Each
// specialist has its own system prompt, model, and tool bindings. The host forwards
// the chosen runner stream and adds delegation metadata; it does not execute
// specialists in parallel or generate a separate synthesis response.
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Emulisy/SupaAgent/internal/agent"
	"github.com/Emulisy/SupaAgent/internal/agent/runtime"
	"github.com/Emulisy/SupaAgent/internal/llm"
	"github.com/Emulisy/SupaAgent/internal/pkg/errcode"
)

// Expert is a service-resolved specialist configuration. Runner receives a copy
// of the host input with these model, prompt, tool, and iteration settings.
type Expert struct {
	Name string
	// Description is included in the routing prompt and should identify the work
	// this specialist can handle.
	Description string
	// System replaces existing system messages in the delegated conversation.
	System    string
	ToolsJSON string
	Primary   *llm.ModelDescriptor
	Fallback  *llm.ModelDescriptor
	MaxIter   int
	Runner    runtime.Runner
}

// RouterChat performs the non-streaming classification call with the host model.
// It is injected so routing does not depend on service-layer model resolution.
type RouterChat func(ctx context.Context, primary, fallback *llm.ModelDescriptor, prompt string) (*llm.ChatResponse, *llm.ModelDescriptor, error)

// DirectRunner handles requests that do not match a specialist, using the host
// conversation and its configured tool bindings.
type DirectRunner func(ctx context.Context, in *runtime.RunInput) (<-chan agent.Event, error)

// HostRouter implements runtime.Runner and selects at most one expert per run.
type HostRouter struct {
	experts      []Expert
	chat         RouterChat
	directRunner DirectRunner
}

// New wires the specialist list, routing call, and no-match execution path.
func New(experts []Expert, chat RouterChat, direct DirectRunner) *HostRouter {
	return &HostRouter{experts: experts, chat: chat, directRunner: direct}
}

// Run starts routing asynchronously and closes the returned channel after the
// selected execution completes or routing fails.
func (h *HostRouter) Run(ctx context.Context, in *runtime.RunInput) (<-chan agent.Event, error) {
	events := make(chan agent.Event, 128)
	go h.loop(ctx, in, events)
	return events, nil
}

func (h *HostRouter) loop(ctx context.Context, in *runtime.RunInput, events chan<- agent.Event) {
	defer close(events)

	// Route from the most recent user message rather than accumulated assistant
	// text or tool observations.
	question := lastUserMessage(in.Messages)
	if question == "" {
		send(events, agent.EvtError(errcode.ErrInvalidParam.Code, "empty question"))
		return
	}

	expert, err := h.route(ctx, in, question)
	if err != nil {
		send(events, agent.EvtError(errcode.From(err).Code, errcode.From(err).Message))
		return
	}
	// No suitable specialist means the host uses its own model and tools.
	if expert == nil {
		h.forwardDirect(ctx, in, events)
		return
	}

	send(events, agent.EvtAgentEvent("delegate", expert.Name, expert.Description))
	send(events, agent.Evt(agent.TypeMeta, map[string]any{"expert": expert.Name}))

	// Copy per-run configuration before replacing specialist settings. Message
	// assembly below creates a separate slice for the delegated conversation.
	expertInput := *in
	expertInput.AgentName = expert.Name
	expertInput.ToolsJSON = expert.ToolsJSON
	expertInput.Primary = expert.Primary
	expertInput.Fallback = expert.Fallback
	if expert.MaxIter > 0 {
		expertInput.MaxIterations = expert.MaxIter
	}
	expertInput.Messages = buildExpertMessages(expert.System, in.Messages)

	expertEvents, err := expert.Runner.Run(ctx, &expertInput)
	if err != nil {
		send(events, agent.EvtError(errcode.From(err).Code, errcode.From(err).Message))
		return
	}
	var final strings.Builder
	// Forward the specialist events unchanged. The collected text is only used for
	// the merge event summary, not for a second model call.
	for ev := range expertEvents {
		if ev.Type == agent.TypeDelta {
			final.WriteString(str(ev.Data["content"]))
		}
		send(events, ev)
	}

	send(events, agent.EvtAgentEvent("merge", expert.Name, truncateRunes(final.String(), 120)))
}

// route asks the host model for a specialist index. Zero, invalid output, or an
// empty specialist list selects the direct-answer path.
func (h *HostRouter) route(ctx context.Context, in *runtime.RunInput, question string) (*Expert, error) {
	if len(h.experts) == 0 {
		return nil, nil
	}

	var b strings.Builder
	for i, e := range h.experts {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, e.Name, e.Description)
	}
	prompt := fmt.Sprintf(
		"You are a task router. Choose the most suitable expert from the list below for the user question.\n"+
			"Output only the expert number (for example, 2). Output 0 if none are suitable.\n\nExperts:\n%s\nUser question: %s",
		b.String(), question)

	resp, _, err := h.chatForRoute(ctx, in, prompt)
	if err != nil {
		return nil, err
	}

	idx := parseRouteIndex(resp.Content, len(h.experts))
	if idx == 0 {
		return nil, nil
	}
	return &h.experts[idx-1], nil
}

// chatForRoute uses the host primary/fallback descriptors for classification.
func (h *HostRouter) chatForRoute(ctx context.Context, in *runtime.RunInput, prompt string) (*llm.ChatResponse, *llm.ModelDescriptor, error) {
	if h.chat == nil {
		return nil, nil, errcode.ErrNotImplement.WithMsg("host router chat function not configured")
	}
	return h.chat(ctx, in.Primary, in.Fallback, prompt)
}

// forwardDirect labels the no-match path and forwards its runner events.
func (h *HostRouter) forwardDirect(ctx context.Context, in *runtime.RunInput, events chan<- agent.Event) {
	if h.directRunner == nil {
		send(events, agent.EvtError(errcode.ErrNotImplement.Code, "host direct runner not configured"))
		return
	}
	send(events, agent.EvtAgentEvent("delegate", "host", "Host direct response"))
	evts, err := h.directRunner(ctx, in)
	if err != nil {
		send(events, agent.EvtError(errcode.From(err).Code, errcode.From(err).Message))
		return
	}
	for ev := range evts {
		send(events, ev)
	}
}

// buildExpertMessages replaces every existing system message with the expert
// prompt, retaining user, assistant, and tool messages. System-level memory or RAG
// context assembled for the host is therefore not retained by this function.
func buildExpertMessages(system string, msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs)+1)
	if system != "" {
		out = append(out, llm.Message{Role: llm.RoleSystem, Content: system})
	}

	for _, m := range msgs {
		if m.Role == llm.RoleSystem {
			continue
		}
		out = append(out, m)
	}
	return out
}

// lastUserMessage searches backward so routing always uses the current request.
func lastUserMessage(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

// parseRouteIndex accepts a JSON integer first, then a single digit embedded in
// free-form model output. An invalid or out-of-range index means no match.
func parseRouteIndex(content string, max int) int {
	var n int
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &n); err == nil {
		if n >= 0 && n <= max {
			return n
		}
	}

	for _, r := range content {
		if r >= '0' && r <= '9' {
			v := int(r - '0')
			if v <= max {
				return v
			}
			return 0
		}
	}
	return 0
}

// send is best effort: any event can be dropped if the consumer buffer is full.
func send(events chan<- agent.Event, e agent.Event) {
	select {
	case events <- e:
	default:
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// truncateRunes limits delegation summaries without splitting UTF-8 characters.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
