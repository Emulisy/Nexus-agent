// Package runtime executes the ReAct model/tool loop and publishes transport-neutral
// progress events. The service layer resolves credentials and assembles memory and
// retrieval context before calling a runner; persistence and HTTP streaming stay
// outside this package.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chengpeng-cp/nexus-agent/internal/agent"
	agenttrace "github.com/chengpeng-cp/nexus-agent/internal/agent/trace"
	"github.com/chengpeng-cp/nexus-agent/internal/llm"
	"github.com/chengpeng-cp/nexus-agent/internal/logging"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
	"github.com/chengpeng-cp/nexus-agent/internal/tool"
)

// Runner starts one execution and returns an event stream owned by the implementation.
// Consumers must keep draining the channel until it closes; an error returned from
// Run means execution could not be started.
type Runner interface {
	Run(ctx context.Context, in *RunInput) (<-chan agent.Event, error)
}

// RunInput holds the resolved configuration and conversation for a single run.
// The runner appends tool exchanges to Messages, so callers should not share one
// input between concurrent executions.
type RunInput struct {
	// SessionID identifies the conversation that owns this run.
	SessionID string
	// AgentName labels decision records and error logs.
	AgentName string
	// Messages already includes the system prompt and selected conversation history.
	Messages []llm.Message
	// ToolsJSON contains persisted agent bindings used to list model-visible tools.
	ToolsJSON string
	// Primary is the resolved model and provider configuration.
	Primary *llm.ModelDescriptor
	// Fallback is optional and is considered by the gateway if opening a call fails.
	Fallback *llm.ModelDescriptor
	// MaxIterations caps model/tool rounds; non-positive values use ten rounds.
	MaxIterations int
	// Refs carries citation metadata emitted before answer generation.
	Refs []map[string]any
	// Trace optionally collects thought, action, and final-answer records.
	Trace *agenttrace.AgentTraceCollector
}

// ReactRunner alternates streamed model calls with sequential tool execution.
// It bounds total runtime and each model step; tools also apply their own executor
// timeouts under the total run context.
type ReactRunner struct {
	gateway     *llm.Gateway
	exec        tool.Executor
	stepTimeout time.Duration
	runTimeout  time.Duration
}

// NewReactRunner wires a gateway, executor, and execution deadlines. Model selection
// and iteration limits are supplied separately for each RunInput.
func NewReactRunner(gateway *llm.Gateway, exec tool.Executor, stepTimeout, runTimeout time.Duration) *ReactRunner {
	return &ReactRunner{gateway: gateway, exec: exec, stepTimeout: stepTimeout, runTimeout: runTimeout}
}

// Run resolves the allowed tool definitions before starting the goroutine, so
// invalid bindings fail synchronously rather than becoming an error event later.
// The returned channel is buffered and is closed when the loop exits.
func (r *ReactRunner) Run(ctx context.Context, in *RunInput) (<-chan agent.Event, error) {
	toolDefs, err := r.exec.List(in.ToolsJSON)
	if err != nil {
		return nil, err
	}
	if in.MaxIterations <= 0 {
		in.MaxIterations = 10
	}

	events := make(chan agent.Event, 128)
	go r.loop(ctx, in, toolDefs, events)
	return events, nil
}

func (r *ReactRunner) loop(ctx context.Context, in *RunInput, toolDefs []llm.ToolDef, events chan<- agent.Event) {
	defer close(events)

	// The overall deadline includes model generation and all tool invocations.
	runCtx, cancel := context.WithTimeout(ctx, r.runTimeout)
	defer cancel()

	// Publish citations before the first token so consumers can display the sources
	// while the answer is still being generated.
	if len(in.Refs) > 0 {
		emit(events, agent.Evt(agent.TypeRefs, map[string]any{"items": in.Refs}))
	}

	promptTokens, completionTokens := 0, 0
	var finalContent, finishReason, usedModel string

	// Each round either completes with a direct answer or extends the conversation
	// with assistant tool calls and their observations.
	for step := 0; step < in.MaxIterations; step++ {
		if err := runCtx.Err(); err != nil {
			emit(events, agent.EvtError(errcode.ErrModelTimeout.Code, "Agent run timed out"))
			return
		}

		// The step deadline covers opening and consuming this model stream. It is
		// canceled before tools run; those receive the remaining overall run context.
		stepCtx, stepCancel := context.WithTimeout(runCtx, r.stepTimeout)
		stream, _, err := r.gateway.ChatStream(stepCtx, in.Primary, in.Fallback, &llm.ChatRequest{
			Messages: in.Messages,
			Tools:    toolDefs,
			Stream:   true,
		})
		if err != nil {
			stepCancel()

			logging.LogError("agent llm call failed", "agent", in.AgentName,
				"model", in.Primary.ModelName, "step", fmt.Sprintf("%d", step), "err", err.Error())
			be := llm.ModelError(err)
			emit(events, agent.EvtError(be.Code, be.Message))
			return
		}

		// Accumulate this round while forwarding text immediately. Providers return
		// assembled tool calls and usage in their final stream event.
		var content, reasoning string
		var calls []llm.ToolCall
		for ev := range stream {
			// A failure after stream opening is still a failed model round. Do not execute
			// partially assembled calls or report already-streamed text as a final answer.
			if ev.Error != nil {
				stepCancel()
				logging.LogError("agent llm stream failed", "agent", in.AgentName, "err", ev.Error.Error())
				be := llm.ModelError(ev.Error)
				emit(events, agent.EvtError(be.Code, be.Message))
				return
			}
			if ev.Content != "" {
				content += ev.Content
				emit(events, agent.EvtDelta(ev.Content))
			}
			if ev.ReasoningContent != "" {
				reasoning += ev.ReasoningContent
				emit(events, agent.EvtReasoning(ev.ReasoningContent))
			}
			if ev.ToolCalls != nil {
				calls = ev.ToolCalls
			}
			if ev.Usage != nil {
				promptTokens += ev.Usage.PromptTokens
				completionTokens += ev.Usage.CompletionTokens
			}
			if ev.FinishReason != "" {
				finishReason = ev.FinishReason
			}
		}
		// Check the deadline before canceling the step ourselves so an expired model
		// stream is distinguished from normal cancellation after successful consumption.
		stepErr := stepCtx.Err()
		stepCancel()
		if stepErr != nil {
			emit(events, agent.EvtError(errcode.ErrModelTimeout.Code, errcode.ErrModelTimeout.Message))
			return
		}

		// A round without tool calls is terminal. The model label currently uses the
		// primary descriptor even if the gateway opened a fallback stream.
		if len(calls) == 0 {
			finalContent = content
			usedModel = in.Primary.ModelName
			break
		}

		if in.Trace != nil && content != "" {
			in.Trace.Add(agenttrace.EventThought, in.AgentName, content)
		}

		// Preserve the assistant tool-call message before adding observations so the
		// next provider request can match each tool result to its call ID.
		in.Messages = append(in.Messages, llm.Message{
			Role: llm.RoleAssistant, Content: content, ReasoningContent: reasoning, ToolCalls: calls,
		})
		// Execute calls in order. A tool failure is returned as an observation so the
		// model can decide how to proceed on its next round.
		for _, tc := range calls {
			emit(events, agent.EvtToolCall(tc.ID, tc.Name, tc.ArgsJSON))
			var step *agenttrace.TraceStep
			if in.Trace != nil {
				step = in.Trace.Add(agenttrace.EventAction, in.AgentName, tc.Name)
				step.Tool = tc.Name
			}

			if in.Trace != nil && step != nil {
				_ = parseArgsForTrace(step, tc.ArgsJSON)
			}

			// Use the overall context here; the executor chooses each tool timeout.
			res := r.exec.Invoke(runCtx, in.ToolsJSON, tc.Name, tc.ArgsJSON)
			emit(events, agent.EvtToolResult(tc.ID, res.Output, res.Elapsed, res.Status))

			if in.Trace != nil && step != nil {
				in.Trace.Done(step, res.Output, res.Status)
			}

			in.Messages = append(in.Messages, llm.Message{
				Role: llm.RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: res.Output,
			})
		}
	}

	// Without a non-empty final answer, report exhaustion instead of emitting a
	// successful completion. This also covers streams that produced no answer text.
	if finalContent == "" {
		emit(events, agent.EvtError(errcode.ErrMaxIteration.Code, errcode.ErrMaxIteration.Message))
		return
	}

	if in.Trace != nil {
		step := in.Trace.Add(agenttrace.EventAnswer, in.AgentName, "")
		in.Trace.Done(step, finalContent, "ok")
	}

	// Model descriptors do not include prices, so usage retains the unknown-cost
	// value. Token counts are the sums supplied by completed provider streams.
	emit(events, agent.EvtUsage(promptTokens, completionTokens, "0.0000", usedModel))
	emit(events, agent.EvtDone(finishReason))
}

// emit uses a best-effort, non-blocking send to avoid retaining a runner when a
// consumer stops draining. A full buffer can drop any event, including usage or a
// terminal event; downstream consumers must handle a stream without termination.
func emit(events chan<- agent.Event, e agent.Event) {
	select {
	case events <- e:
	default:
	}
}

// parseArgsForTrace stores decoded arguments for the decision timeline. Malformed
// JSON is not a tracing failure for the run; the executor handles invocation errors.
func parseArgsForTrace(step *agenttrace.TraceStep, argsJSON string) error {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return err
	}
	step.Args = m
	return nil
}
