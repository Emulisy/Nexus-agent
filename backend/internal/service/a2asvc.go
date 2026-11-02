// A2A services store remote-agent registrations and send synchronous tasks.
// Registration/health checks fetch Agent Cards; task results are reduced to state
// and extracted text. Inbound local execution reuses ChatService rather than
// implementing a separate model or tool pipeline.
package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/chengpeng-cp/nexus-agent/internal/a2a"
	"github.com/chengpeng-cp/nexus-agent/internal/agent"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
)

// A2AService combines the remote registry with a reusable A2A HTTP client.
type A2AService struct {
	svcs *Services
	cli  *a2a.Client
}

// NewA2AService creates the remote protocol client while sharing application persistence.
func NewA2AService(svcs *Services) *A2AService {
	return &A2AService{svcs: svcs, cli: a2a.NewClient()}
}

// A2ARegisterInput supplies the remote base URL and an optional local display name.
type A2ARegisterInput struct {
	Name    string `json:"name"` // Defaults to the agent card name.
	BaseURL string `json:"base_url"`
}

// Register fetches a reachable Agent Card before saving a connected registration.
// An omitted display name uses card.Name. The stored card is a snapshot and
// LastHealthAt records this successful fetch.
func (s *A2AService) Register(ctx context.Context, in *A2ARegisterInput) (*model.A2AAgent, error) {
	if in.BaseURL == "" {
		return nil, errcode.ErrInvalidParam.WithMsg("base_url is required")
	}

	card, err := s.cli.FetchCard(ctx, in.BaseURL)
	if err != nil {
		return nil, err
	}

	name := in.Name
	if name == "" {
		name = card.Name
	}
	cardJSON, _ := json.Marshal(card)
	ag := &model.A2AAgent{
		Name:    name,
		BaseURL: in.BaseURL,
		Card:    cardJSON,
		Status:  "connected",
	}
	now := time.Now()
	ag.LastHealthAt = &now
	if err := s.svcs.Deps.Repos.A2A.Create(ctx, ag); err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return ag, nil
}

// List returns registered remote agents and translates repository failures to internal errors.
func (s *A2AService) List(ctx context.Context) ([]model.A2AAgent, error) {
	list, err := s.svcs.Deps.Repos.A2A.List(ctx)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	return list, nil
}

// Delete verifies the registration exists before removing it from the local registry.
func (s *A2AService) Delete(ctx context.Context, id int64) error {
	if _, err := s.get(ctx, id); err != nil {
		return err
	}
	return s.svcs.Deps.Repos.A2A.Delete(ctx, id)
}

// get normalizes repository lookup failures to the remote-agent card error used by this service.
func (s *A2AService) get(ctx context.Context, id int64) (*model.A2AAgent, error) {
	ag, err := s.svcs.Deps.Repos.A2A.Get(ctx, id)
	if err != nil {
		return nil, errcode.ErrA2ACardFetch.WithMsg("Remote Agent not found: %d", id)
	}
	return ag, nil
}

// HealthCheck refetches the Agent Card and updates status and check time.
// On failure, persisting the error state is best effort and the fetch error is
// returned. On success, the newly fetched card replaces the cached snapshot.
func (s *A2AService) HealthCheck(ctx context.Context, id int64) error {
	ag, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	card, err := s.cli.FetchCard(ctx, ag.BaseURL)
	if err != nil {
		ag.Status = "error"
		now := time.Now()
		ag.LastHealthAt = &now
		_ = s.svcs.Deps.Repos.A2A.Update(ctx, ag)
		return err
	}
	cardJSON, _ := json.Marshal(card)
	ag.Card = cardJSON
	ag.Status = "connected"
	now := time.Now()
	ag.LastHealthAt = &now
	return s.svcs.Deps.Repos.A2A.Update(ctx, ag)
}

// SendTaskResult carries the remote task ID/state and the text extracted from its artifacts.
type SendTaskResult struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"` // completed/failed/...
	Text   string `json:"text"`
}

// SendTask rejects empty text, loads the remote registration, and calls message/send.
// It waits for the returned task and exposes its current state; the service
// does not subscribe to a separate remote event stream.
func (s *A2AService) SendTask(ctx context.Context, id int64, text string) (*SendTaskResult, error) {
	if text == "" {
		return nil, errcode.ErrInvalidParam.WithMsg("Task content is required")
	}
	ag, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	task, err := s.cli.SendMessage(ctx, ag.BaseURL, text)
	if err != nil {
		return nil, err
	}
	return &SendTaskResult{
		TaskID: task.ID,
		State:  task.State,
		Text:   a2a.ExtractText(task),
	}, nil
}

// InvokeLocal runs ChatService under the configured system identity and collects deltas.
// Agent ID zero selects the default assistant and an empty session ID creates a
// new conversation. Other event types are ignored except error, which aborts
// collection and becomes an A2A task failure.
func (s *A2AService) InvokeLocal(ctx context.Context, agentID int64, text string) (string, error) {
	events, err := s.svcs.Chat.Stream(ctx, systemUserID, &ChatInput{
		AgentID: agentID,
		Content: text,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range events {
		if ev.Type == agent.TypeDelta {
			if s, ok := ev.Data["content"].(string); ok {
				b.WriteString(s)
			}
		}
		if ev.Type == agent.TypeError {
			return "", errcode.ErrA2ATaskFail.WithMsg("Local execution failed: %v", ev.Data["message"])
		}
	}
	return b.String(), nil
}

// systemUserID is the identity supplied to the standard chat/persistence path for inbound A2A calls.
const systemUserID int64 = 0
