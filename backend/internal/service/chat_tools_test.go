package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"

	"github.com/chengpeng-cp/nexus-agent/internal/agent"
	"github.com/chengpeng-cp/nexus-agent/internal/agent/host"
	"github.com/chengpeng-cp/nexus-agent/internal/agent/runtime"
	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/llm"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/tool"
)

type calculatorCatalog struct{}

func (calculatorCatalog) GetByCode(context.Context, string) (*model.Tool, error) {
	return &model.Tool{Code: "calculator", TimeoutMs: 1000}, nil
}

func (calculatorCatalog) ListByCodes(context.Context, []string) ([]model.Tool, error) {
	return nil, nil
}

func (calculatorCatalog) ListByIDs(_ context.Context, ids []int64) ([]model.Tool, error) {
	if len(ids) != 1 || ids[0] != 1 {
		return nil, fmt.Errorf("unexpected tool bindings: %v", ids)
	}
	return []model.Tool{{ID: 1, Code: "calculator", Description: "Evaluate an expression",
		Parameters: datatypes.JSON(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)}}, nil
}

// Exercise host routing, numeric tool bindings, wire schemas, real calculation,
// and the follow-up model round. Both no-expert and no-match paths used to drop tools.
func TestHostDirectResponseUsesBoundTools(t *testing.T) {
	for _, routeMiss := range []bool{false, true} {
		t.Run(fmt.Sprintf("routeMiss=%v", routeMiss), func(t *testing.T) {
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools []struct {
						Function struct{ Name string } `json:"function"`
					} `json:"tools"`
					Messages []struct {
						Role       string `json:"role"`
						Content    string `json:"content"`
						ToolCallID string `json:"tool_call_id"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					http.Error(w, "invalid request", 400)
					return
				}
				if len(req.Tools) != 1 || req.Tools[0].Function.Name != "calculator" {
					t.Error("host did not advertise its bound calculator")
					http.Error(w, "missing tools", 400)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if requests.Add(1) == 1 {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_calc\",\"type\":\"function\",\"function\":{\"name\":\"calculator\",\"arguments\":\"{\\\"expression\\\":\\\"6*7\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				last := req.Messages[len(req.Messages)-1]
				if last.Role != "tool" || last.ToolCallID != "call_calc" || last.Content != "42" {
					t.Errorf("incorrect tool observation: %+v", last)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"42\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer upstream.Close()

			reg := tool.NewRegistry()
			tool.RegisterSimple(reg)
			cfg := &config.Config{}
			cfg.Agent.StepTimeout, cfg.Agent.RunTimeout = time.Second*5, time.Second*10
			cfg.LLM.MaxConnsPerProvider, cfg.LLM.CircuitErrorRate = 1, 1
			svcs := &Services{Deps: Deps{
				Cfg: cfg, Gateway: llm.NewGateway(&cfg.LLM),
				Executor: tool.NewExecutor(calculatorCatalog{}, reg, tool.ExecutorConfig{DefaultTimeout: time.Second}),
			}}
			var experts []host.Expert
			if routeMiss {
				experts = []host.Expert{{Name: "Other expert"}}
			}
			router := host.New(experts, func(context.Context, *llm.ModelDescriptor, *llm.ModelDescriptor, string) (*llm.ChatResponse, *llm.ModelDescriptor, error) {
				return &llm.ChatResponse{Content: "0"}, nil, nil
			}, NewChatService(svcs).hostDirectRunner())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			events, err := router.Run(ctx, &runtime.RunInput{
				AgentName: "Host", ToolsJSON: `[{"tool_id":1}]`, MaxIterations: 3,
				Primary:  &llm.ModelDescriptor{ProviderCode: "test", ModelName: "test-model", BaseURL: upstream.URL},
				Messages: []llm.Message{{Role: llm.RoleUser, Content: "Calculate 6*7 with the calculator"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var called, result, done bool
			for ev := range events {
				switch ev.Type {
				case agent.TypeToolCall:
					called = ev.Data["name"] == "calculator"
				case agent.TypeToolResult:
					result = ev.Data["status"] == "ok" && ev.Data["result"] == "42"
				case agent.TypeDone:
					done = true
				case agent.TypeError:
					t.Errorf("run failed: %v", ev.Data)
				}
			}
			if !called || !result || !done || requests.Load() != 2 {
				t.Fatalf("incomplete tool round trip: call=%v result=%v done=%v requests=%d", called, result, done, requests.Load())
			}
		})
	}
}
