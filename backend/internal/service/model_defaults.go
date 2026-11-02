// Startup catalog synchronization fills provider/chat presets without resetting admin choices.
// Nonempty provider environment settings can replace credentials, endpoints, or
// upstream model names on restart. Missing environment values preserve stored
// settings, and existing status/priority/default choices remain unchanged.
package service

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/repo"
)

// providerPreset holds the initial provider identity, endpoint, chat model, alias, and selection metadata.
type providerPreset struct {
	code, name, baseURL, modelName, alias string
	contextWindow, priority               int
}

// defaultProviderPresets supplies catalog entries only when the matching provider/model records do not already exist.
var defaultProviderPresets = []providerPreset{
	{"openai", "OpenAI", "https://api.openai.com/v1", "gpt-4.1-mini", "openai-default", 1047576, 10},
	{"gemini", "Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3.8-flash", "gemini-default", 1048576, 20},
	{"deepseek", "DeepSeek", "https://api.deepseek.com", "deepseek-flash", "deepseek-default", 1048576, 30},
}

// modelCatalog exposes the persistence operations shared by transaction-backed synchronization and isolated tests.
type modelCatalog interface {
	ListProviders(context.Context) ([]model.ModelProvider, error)
	CreateProvider(context.Context, *model.ModelProvider) error
	UpdateProvider(context.Context, *model.ModelProvider) error
	ListModels(context.Context, string) ([]model.ModelConfig, error)
	CreateModel(context.Context, *model.ModelConfig) error
	UpdateModel(context.Context, *model.ModelConfig) error
}

// bootstrapDefaultModels serializes startup synchronization across API and worker processes.
// The transaction-bound repository prevents partial preset updates when listing,
// encryption, or a subsequent write fails.
func (s *Services) bootstrapDefaultModels(ctx context.Context) error {
	return s.Repos.LLM.WithCatalogLock(ctx, func(catalog *repo.LLMRepo) error {
		return syncDefaultModels(ctx, catalog, s.Encryptor, os.Getenv)
	})
}

// syncDefaultModels loads the catalog, then merges each preset with current records.
// Providers match by code; chat models match by provider ID and preset alias.
// Nonempty environment overrides are normalized and applied selectively. Keys
// are re-encrypted only when changed or unreadable, avoiding new ciphertext and
// writes on otherwise identical restarts. Existing admin fields are preserved.
func syncDefaultModels(ctx context.Context, catalog modelCatalog, enc Encryptor, getenv func(string) string) error {
	providers, err := catalog.ListProviders(ctx)
	if err != nil {
		return err
	}
	models, err := catalog.ListModels(ctx, modelTypeChat)
	if err != nil {
		return err
	}
	for _, preset := range defaultProviderPresets {
		prefix := strings.ToUpper(preset.code)
		provider := model.ModelProvider{Code: preset.code, Name: preset.name, BaseURL: preset.baseURL, Status: 1}
		for _, existing := range providers {
			if existing.Code == preset.code {
				provider = existing
				break
			}
		}
		changed := false
		if endpoint := strings.TrimRight(strings.TrimSpace(getenv(prefix+"_BASE_URL")), "/"); endpoint != "" && endpoint != provider.BaseURL {
			provider.BaseURL = endpoint
			changed = true
		}
		if key := strings.TrimSpace(getenv(prefix + "_API_KEY")); key != "" {
			plain, decryptErr := enc.Decrypt(provider.APIKey)
			if decryptErr != nil || plain != key {
				provider.APIKey, err = enc.Encrypt(key)
				if err != nil {
					return fmt.Errorf("encrypt %s credential: %w", preset.code, err)
				}
				changed = true
			}
		}
		if provider.ID == 0 {
			if err := catalog.CreateProvider(ctx, &provider); err != nil {
				return fmt.Errorf("create %s provider: %w", preset.code, err)
			}
		} else if changed {
			if err := catalog.UpdateProvider(ctx, &provider); err != nil {
				return fmt.Errorf("sync %s provider: %w", preset.code, err)
			}
		}

		var existing *model.ModelConfig
		for i := range models {
			if models[i].ProviderID == provider.ID && models[i].Alias == preset.alias {
				existing = &models[i]
				break
			}
		}
		override := strings.TrimSpace(getenv(prefix + "_MODEL"))
		if existing != nil {
			if override != "" && override != existing.ModelName {
				existing.ModelName = override
				// Avoid saving a stale preloaded Provider association with this model.
				existing.Provider = nil
				if err := catalog.UpdateModel(ctx, existing); err != nil {
					return fmt.Errorf("sync %s model: %w", preset.code, err)
				}
			}
			continue
		}
		name := preset.modelName
		if override != "" {
			name = override
		}
		m := model.ModelConfig{
			ProviderID: provider.ID, ModelName: name, Alias: preset.alias,
			Type: modelTypeChat, ContextWindow: preset.contextWindow, MaxOutput: 8192,
			Priority: preset.priority, Capabilities: model.CapabilitiesJSON([]string{"function_call"}), Status: 1,
		}
		if err := catalog.CreateModel(ctx, &m); err != nil {
			return fmt.Errorf("create %s model: %w", preset.code, err)
		}
	}
	return nil
}
