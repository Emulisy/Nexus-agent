// Model management separates persisted configuration from runtime descriptors.
// Provider keys are encrypted before persistence and decrypted only for masked
// views or gateway descriptors. Partial updates merge pointer-valued request
// fields into existing records to preserve omitted credentials and status values.
package service

import (
	"context"

	"github.com/chengpeng-cp/nexus-agent/internal/llm"
	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
	"github.com/chengpeng-cp/nexus-agent/internal/repo"
)

// Model-type strings mirror the persisted model types used by repository lookups.
const (
	modelTypeChat   = "chat"
	modelTypeEmbed  = "embedding"
	modelTypeRerank = "rerank"
)

// ModelService manages provider/model records and delegates credential encryption through a small interface.
type ModelService struct {
	repos     *repo.Repos
	encryptor Encryptor
}

// Encryptor supplies reversible credential protection without coupling model management to one implementation.
type Encryptor interface {
	Encrypt(plain string) (string, error)
	Decrypt(cipher string) (string, error)
}

// NewModelService binds configuration repositories and the credential encryptor.
func NewModelService(repos *repo.Repos, enc Encryptor) *ModelService {
	return &ModelService{repos: repos, encryptor: enc}
}

// ListProviders builds views containing only masked credentials.
// An unreadable stored key yields an empty mask so one decryption failure does
// not prevent the rest of the provider list from loading.
func (s *ModelService) ListProviders(ctx context.Context) ([]model.ProviderView, error) {
	list, err := s.repos.LLM.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.ProviderView, 0, len(list))
	for i := range list {
		p := &list[i]
		// Treat an unreadable key as unconfigured so the provider list remains available.
		masked := ""
		if plain, e := s.encryptor.Decrypt(p.APIKey); e == nil {
			masked = model.MaskAPIKey(plain)
		}
		out = append(out, model.ProviderView{
			ID: p.ID, Code: p.Code, Name: p.Name, BaseURL: p.BaseURL,
			APIKeyMasked: masked, Status: p.Status,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		})
	}
	return out, nil
}

// CreateProvider requires code, name, and base URL, encrypts the optional key,
// and defaults status to enabled unless an explicit status was supplied.
func (s *ModelService) CreateProvider(ctx context.Context, req *model.ProviderUpsert) error {
	if req.Code == nil || req.Name == nil || req.BaseURL == nil {
		return errcode.ErrMissingParam
	}
	enc, err := s.encryptor.Encrypt(derefStr(req.APIKey))
	if err != nil {
		return errcode.ErrInternal.WithCause(err)
	}
	p := &model.ModelProvider{
		Code:    *req.Code,
		Name:    *req.Name,
		BaseURL: *req.BaseURL,
		APIKey:  enc,
		Status:  1,
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	return s.repos.LLM.CreateProvider(ctx, p)
}

// UpdateProvider loads the stored provider and merges only supplied fields.
// An empty API key is treated like an omitted key because edit forms do not
// receive the original secret. Other pointer fields can explicitly write zero values.
func (s *ModelService) UpdateProvider(ctx context.Context, id int64, req *model.ProviderUpsert) error {
	p, err := s.repos.LLM.GetProvider(ctx, id)
	if err != nil {
		return errcode.ErrProviderNotFound.WithCause(err)
	}
	if req.Code != nil {
		p.Code = *req.Code
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.BaseURL != nil {
		p.BaseURL = *req.BaseURL
	}
	// An omitted or empty key preserves the existing credential.
	if req.APIKey != nil && *req.APIKey != "" {
		enc, err := s.encryptor.Encrypt(*req.APIKey)
		if err != nil {
			return errcode.ErrInternal.WithCause(err)
		}
		p.APIKey = enc
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	return s.repos.LLM.UpdateProvider(ctx, p)
}

// DeleteProvider delegates removal; dependency constraints are enforced by PostgreSQL.
func (s *ModelService) DeleteProvider(ctx context.Context, id int64) error {
	return s.repos.LLM.DeleteProvider(ctx, id)
}

// ListModels returns persisted models and their associated provider, optionally filtered by type.
func (s *ModelService) ListModels(ctx context.Context, typ string) ([]model.ModelConfig, error) {
	return s.repos.LLM.ListModels(ctx, typ)
}

// ListChatModels returns only enabled chat models with enabled providers.
// The selector view includes labels and provider names without provider secrets;
// an empty alias falls back to the upstream model name.
func (s *ModelService) ListChatModels(ctx context.Context) ([]ChatModelItem, error) {
	all, err := s.repos.LLM.ListModels(ctx, modelTypeChat)
	if err != nil {
		return nil, err
	}
	out := make([]ChatModelItem, 0, len(all))
	for i := range all {
		m := &all[i]
		if m.Status != 1 || m.Provider == nil || m.Provider.Status != 1 {
			continue
		}
		key, err := s.encryptor.Decrypt(m.Provider.APIKey)
		if err != nil || (key == "" && m.Provider.Code != "ollama") {
			continue
		}
		label := m.Alias
		if label == "" {
			label = m.ModelName
		}
		out = append(out, ChatModelItem{
			ID: m.ID, Label: label, ModelName: m.ModelName,
			Provider: m.Provider.Name, IsDefault: m.IsDefault,
		})
	}
	return out, nil
}

// ChatModelItem is the reduced model view consumed by the chat model selector.
type ChatModelItem struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	ModelName string `json:"model_name"`
	Provider  string `json:"provider"`
	IsDefault bool   `json:"is_default"`
}

// CreateModel requires a provider, upstream name, and type, then applies defaults.
// Optional pointer fields override those defaults even when their value is zero;
// capabilities are normalized to a JSON array before insertion.
func (s *ModelService) CreateModel(ctx context.Context, req *model.ModelUpsert) error {
	if req.ProviderID == nil || req.ModelName == nil || req.Type == nil {
		return errcode.ErrMissingParam
	}
	m := &model.ModelConfig{
		ProviderID:    *req.ProviderID,
		ModelName:     *req.ModelName,
		Alias:         derefStr(req.Alias),
		Type:          *req.Type,
		ContextWindow: 8192,
		MaxOutput:     4096,
		Priority:      100,
		Capabilities:  model.CapabilitiesJSON(derefCaps(req.Capabilities)),
		Status:        1,
	}
	if req.ContextWindow != nil {
		m.ContextWindow = *req.ContextWindow
	}
	if req.MaxOutput != nil {
		m.MaxOutput = *req.MaxOutput
	}
	if req.InputPrice != nil {
		m.InputPrice = *req.InputPrice
	}
	if req.OutputPrice != nil {
		m.OutputPrice = *req.OutputPrice
	}
	if req.Priority != nil {
		m.Priority = *req.Priority
	}
	if req.IsDefault != nil {
		m.IsDefault = *req.IsDefault
	}
	if req.Status != nil {
		m.Status = *req.Status
	}
	return s.repos.LLM.CreateModel(ctx, m)
}

// UpdateModel merges supplied configuration fields into the loaded model, preserving omitted values.
func (s *ModelService) UpdateModel(ctx context.Context, id int64, req *model.ModelUpsert) error {
	m, err := s.repos.LLM.GetModel(ctx, id)
	if err != nil {
		return errcode.ErrProviderNotFound.WithCause(err)
	}
	if req.ProviderID != nil {
		m.ProviderID = *req.ProviderID
	}
	if req.ModelName != nil {
		m.ModelName = *req.ModelName
	}
	if req.Alias != nil {
		m.Alias = *req.Alias
	}
	if req.Type != nil {
		m.Type = *req.Type
	}
	if req.ContextWindow != nil {
		m.ContextWindow = *req.ContextWindow
	}
	if req.MaxOutput != nil {
		m.MaxOutput = *req.MaxOutput
	}
	if req.InputPrice != nil {
		m.InputPrice = *req.InputPrice
	}
	if req.OutputPrice != nil {
		m.OutputPrice = *req.OutputPrice
	}
	if req.Priority != nil {
		m.Priority = *req.Priority
	}
	if req.Capabilities != nil {
		m.Capabilities = model.CapabilitiesJSON(*req.Capabilities)
	}
	if req.IsDefault != nil {
		m.IsDefault = *req.IsDefault
	}
	if req.Status != nil {
		m.Status = *req.Status
	}
	return s.repos.LLM.UpdateModel(ctx, m)
}

// DeleteModel delegates deletion of the identified model configuration.
func (s *ModelService) DeleteModel(ctx context.Context, id int64) error {
	return s.repos.LLM.DeleteModel(ctx, id)
}

// derefStr supplies an empty default for omitted string fields during creation.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// derefCaps preserves nil for omitted capability lists so creation can normalize them consistently.
func derefCaps(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

// ResolveModel builds request-local primary and optional fallback descriptors.
// ID zero uses the platform chat default and does not resolve a fallback. A
// primary lookup/decryption failure is fatal; an unavailable fallback is ignored
// so a valid primary remains usable.
func (s *Services) ResolveModel(ctx context.Context, modelConfigID, fallbackID int64) (*llm.ModelDescriptor, *llm.ModelDescriptor, error) {
	if modelConfigID == 0 {
		if d := s.DefaultChat(); d != nil {
			return d, nil, nil
		}
		return nil, nil, errcode.ErrProviderNotFound.WithMsg("No default chat model is configured")
	}
	m, err := s.Deps.Repos.LLM.GetModel(ctx, modelConfigID)
	if err != nil {
		return nil, nil, errcode.ErrProviderNotFound.WithCause(err)
	}
	primary, err := s.descriptorOf(m, 0, 0)
	if err != nil {
		return nil, nil, err
	}

	var fallback *llm.ModelDescriptor
	if fallbackID > 0 {
		if fb, err := s.Deps.Repos.LLM.GetModel(ctx, fallbackID); err == nil {
			fallback, _ = s.descriptorOf(fb, 0, 0)
		}
	}
	return primary, fallback, nil
}

// resolveByName resolves a configured alias with repository default-selection fallback.
// It uses a background context because default-model callbacks have no request
// context parameter, then decrypts the selected provider credentials.
func (s *Services) resolveByName(alias, typ string) (*llm.ModelDescriptor, error) {
	m, err := s.Deps.Repos.LLM.DefaultModel(context.Background(), alias, typ)
	if err != nil {
		return nil, errcode.ErrProviderNotFound.WithMsg("Default model %q is not configured", alias).WithCause(err)
	}
	return s.descriptorOf(m, 0, 0)
}

// descriptorOf requires a preloaded enabled provider and enabled model.
// It decrypts the stored key and rejects keyless cloud configurations while
// allowing keyless Ollama. The resulting descriptor uses sampling defaults
// 0.7/0.9 for nonpositive inputs; chat can then apply agent-specific overrides.
func (s *Services) descriptorOf(m *model.ModelConfig, temp, topP float32) (*llm.ModelDescriptor, error) {
	if m.Provider == nil {
		return nil, errcode.ErrProviderNotFound.WithMsg("Model %q has no associated provider", m.Alias)
	}
	if m.Status != 1 || m.Provider.Status != 1 {
		return nil, errcode.ErrProviderNotFound.WithMsg("The model or its provider is disabled")
	}
	apiKey, err := s.Deps.Encryptor.Decrypt(m.Provider.APIKey)
	if err != nil {
		return nil, errcode.ErrInternal.WithCause(err)
	}
	if apiKey == "" && m.Provider.Code != "ollama" {
		return nil, errcode.ErrProviderNotFound.WithMsg("Provider %q has no API key configured", m.Provider.Name)
	}
	if temp <= 0 {
		temp = 0.7
	}
	if topP <= 0 {
		topP = 0.9
	}
	return &llm.ModelDescriptor{
		ProviderCode: m.Provider.Code,
		BaseURL:      m.Provider.BaseURL,
		APIKey:       apiKey,
		ModelName:    m.ModelName,
		Temperature:  temp,
		TopP:         topP,
		MaxTokens:    m.MaxOutput,
	}, nil
}
