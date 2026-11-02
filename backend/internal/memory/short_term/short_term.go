// Package memory builds a bounded recent conversation for model input. Short-term
// state is derived from supplied messages for each request; it is not a separate
// Redis cache and does not summarize history with another model call.
package memory

import (
	"strings"

	"github.com/Emulisy/SupaAgent/internal/llm"
)

// ShortTermConfig selects a message window and an approximate token allowance.
// The allowance is used to shorten tool observations, not as an exact tokenizer
// or a hard cap on every kind of message.
type ShortTermConfig struct {
	// Window retains the most recent N messages before any tool-result compression.
	Window int
	// TokenBudget is an estimate based on message text plus fixed metadata overhead.
	TokenBudget int
}

// DefaultShortTermConfig retains twenty messages with an 8,192-token estimate.
func DefaultShortTermConfig() ShortTermConfig {
	return ShortTermConfig{Window: 20, TokenBudget: 8192}
}

// ShortTerm applies the configured window and compression policy to one message list.
type ShortTerm struct {
	cfg ShortTermConfig
}

// NewShortTerm uses the default configuration when Window is non-positive.
func NewShortTerm(cfg ShortTermConfig) *ShortTerm {
	if cfg.Window <= 0 {
		cfg = DefaultShortTermConfig()
	}
	return &ShortTerm{cfg: cfg}
}

// Apply copies the retained recent window, estimates its size, and shortens older
// tool observations while over budget. Non-tool messages are left intact; callers
// prepend their current system prompt after this history-only pass.
func (s *ShortTerm) Apply(msgs []llm.Message) []llm.Message {
	if len(msgs) > s.cfg.Window {
		msgs = msgs[len(msgs)-s.cfg.Window:]
	}

	// Copy the slice before replacing message content so the supplied history is
	// not modified by tool-result compression.
	out := make([]llm.Message, 0, len(msgs))
	out = append(out, msgs...)

	budget := s.cfg.TokenBudget
	for i := range out {
		budget -= estimateMsgTokens(out[i])
	}
	// Prefer shortening tool results because they can dominate context length and
	// are less useful than recent user intent once the next model step has used them.
	for budget < 0 {
		idx := s.findOldestToolMsg(out)
		if idx < 0 {
			break
		}
		out[idx].Content = truncateForBudget(out[idx].Content, 200)
		budget += estimateMsgTokens(out[idx]) - 40
	}
	return out
}

// findOldestToolMsg chooses the earliest tool observation without a compression
// marker. It leaves user, assistant, and system messages unchanged.
func (s *ShortTerm) findOldestToolMsg(msgs []llm.Message) int {
	for i, m := range msgs {
		if m.Role == llm.RoleTool && !strings.HasSuffix(m.Content, "…[compressed]") {
			return i
		}
	}
	return -1
}

// estimateMsgTokens uses half the rune count plus eight tokens of metadata
// overhead. It intentionally does not depend on a specific model tokenizer.
func estimateMsgTokens(m llm.Message) int {
	n := 8
	n += len([]rune(m.Content)) / 2
	n += len([]rune(m.ReasoningContent)) / 2
	return n
}

// truncateForBudget preserves a Unicode prefix and appends the marker used by
// findOldestToolMsg. Content already within maxRunes is returned unchanged.
func truncateForBudget(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…[compressed]"
}
