// Model entities retain provider credentials and selectable model metadata.
// Runtime gateway descriptors are assembled by services after availability
// checks and decryption, keeping stored ciphertext out of provider requests.
package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// ModelProvider stores the endpoint, adapter code, enabled status, and encrypted API key for an upstream provider.
type ModelProvider struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// Code selects the provider adapter and matches provider-prefixed startup environment settings.
	Code    string `gorm:"size:32;uniqueIndex" json:"code"`
	Name    string `gorm:"size:64" json:"name"`
	BaseURL string `gorm:"column:base_url;size:512" json:"base_url"`
	// APIKey contains AES-GCM ciphertext and is excluded from entity JSON serialization.
	APIKey    string    `gorm:"column:api_key;type:text" json:"-"`
	Status    int16     `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Model types classify records used for conversation, query/document embeddings, and reranking.
const (
	ModelTypeChat   = "chat"
	ModelTypeEmbed  = "embedding"
	ModelTypeRerank = "rerank"
)

// ModelConfig identifies an upstream model and its platform alias, selection priority, limits, and capabilities.
type ModelConfig struct {
	ID         int64 `gorm:"primaryKey" json:"id"`
	ProviderID int64 `gorm:"index" json:"provider_id"`
	// ModelName is passed upstream; it is distinct from the platform-facing alias.
	ModelName string `gorm:"size:64" json:"model_name"`
	// Alias is used by default-model configuration and workflow model selection.
	Alias string `gorm:"size:64" json:"alias"`
	Type  string `gorm:"size:16;index:idx_model_configs_type,priority:1" json:"type"`
	// ContextWindow is stored model metadata; chat history currently uses its own fixed token budget.
	ContextWindow int `gorm:"default:8192" json:"context_window"`
	MaxOutput     int `gorm:"default:4096" json:"max_output"`
	// InputPrice is the configured CNY price per million prompt tokens.
	InputPrice float64 `gorm:"type:numeric(12,6);default:0" json:"input_price"`
	// OutputPrice is the configured CNY price per million completion tokens.
	OutputPrice float64 `gorm:"type:numeric(12,6);default:0" json:"output_price"`
	// Priority sorts eligible default-model candidates after the is_default flag; lower values come first.
	Priority int `gorm:"default:100;index:idx_model_configs_type,priority:2" json:"priority"`
	// Capabilities is a JSON array such as vision, function_call, or reasoning.
	Capabilities datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"capabilities"`
	IsDefault    bool           `json:"is_default"`
	Status       int16          `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	// Provider is preloaded by repository reads and supplies endpoint, availability, and encrypted credentials.
	Provider *ModelProvider `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
}

// ProviderView is an API-safe provider projection containing a masked key summary instead of its stored credential.
type ProviderView struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// APIKeyMasked empty means no readable credential was available for the view.
	APIKeyMasked string    `json:"api_key_masked"`
	Status       int16     `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MaskAPIKey leaves an empty key empty and fully masks keys of six code points or fewer.
// Longer keys expose two leading and four trailing code points so administrators
// can distinguish configured credentials without receiving the full secret.
func MaskAPIKey(plain string) string {
	if plain == "" {
		return ""
	}
	r := []rune(plain)
	if len(r) <= 6 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-4:])
}

// ProviderUpsert uses pointers to distinguish omitted fields from explicit values.
// APIKey is plaintext input encrypted by the service; omitted/empty keys preserve
// the stored credential during updates rather than clearing it.
type ProviderUpsert struct {
	Code    *string `json:"code"`
	Name    *string `json:"name"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"` // Encrypted before storage; empty preserves the existing key on update.
	Status  *int16  `json:"status"`
}

// ModelUpsert preserves omitted fields during updates while allowing explicit false/zero values.
type ModelUpsert struct {
	ProviderID    *int64    `json:"provider_id"`
	ModelName     *string   `json:"model_name"`
	Alias         *string   `json:"alias"`
	Type          *string   `json:"type"`
	ContextWindow *int      `json:"context_window"`
	MaxOutput     *int      `json:"max_output"`
	InputPrice    *float64  `json:"input_price"`
	OutputPrice   *float64  `json:"output_price"`
	Priority      *int      `json:"priority"`
	Capabilities  *[]string `json:"capabilities"` // ["vision","function_call","reasoning"]
	IsDefault     *bool     `json:"is_default"`
	Status        *int16    `json:"status"`
}

// CapabilitiesJSON normalizes nil to [] before marshaling, preserving an array shape in JSONB/API responses.
func CapabilitiesJSON(caps []string) datatypes.JSON {
	if caps == nil {
		caps = []string{}
	}
	raw, _ := json.Marshal(caps)
	return datatypes.JSON(raw)
}
