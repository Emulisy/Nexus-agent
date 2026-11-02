// Model persistence stores provider credentials and model metadata without decrypting keys.
// Provider associations are preloaded for descriptor assembly. Startup preset
// changes can share a transaction-bound catalog handle under an advisory lock.
package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/Emulisy/SupaAgent/internal/model"
)

// LLMRepo reads and writes model providers/configurations using either the shared DB or a transaction handle.
type LLMRepo struct {
	db *gorm.DB
}

// WithCatalogLock serializes cooperating API/worker startup catalog changes.
// The PostgreSQL transaction-level advisory lock is held through the callback;
// returning its error rolls back all catalog writes and releases the lock.
func (r *LLMRepo) WithCatalogLock(ctx context.Context, fn func(*LLMRepo) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(735920104)").Error; err != nil {
			return err
		}
		return fn(&LLMRepo{db: tx})
	})
}

// ---- Provider ----

// CreateProvider inserts an already-encrypted provider record and populates its ID.
func (r *LLMRepo) CreateProvider(ctx context.Context, p *model.ModelProvider) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// UpdateProvider saves the complete provider; partial request merging happens in ModelService.
func (r *LLMRepo) UpdateProvider(ctx context.Context, p *model.ModelProvider) error {
	return r.db.WithContext(ctx).Save(p).Error
}

// DeleteProvider delegates referential restrictions and dependent-row behavior to database constraints.
func (r *LLMRepo) DeleteProvider(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.ModelProvider{}, id).Error
}

// ListProviders returns providers in ID order; callers must mask credentials before returning public views.
func (r *LLMRepo) ListProviders(ctx context.Context) ([]model.ModelProvider, error) {
	var list []model.ModelProvider
	err := r.db.WithContext(ctx).Order("id ASC").Find(&list).Error
	return list, err
}

// GetProvider loads one provider, including encrypted key material required by service-side decryption.
func (r *LLMRepo) GetProvider(ctx context.Context, id int64) (*model.ModelProvider, error) {
	var p model.ModelProvider
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// ---- ModelConfig ----

// CreateModel inserts model metadata that references a persisted provider.
func (r *LLMRepo) CreateModel(ctx context.Context, m *model.ModelConfig) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// UpdateModel saves a complete model; callers should avoid retaining stale provider associations during updates.
func (r *LLMRepo) UpdateModel(ctx context.Context, m *model.ModelConfig) error {
	return r.db.WithContext(ctx).Save(m).Error
}

// DeleteModel removes one configuration and lets database constraints enforce dependent references.
func (r *LLMRepo) DeleteModel(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.ModelConfig{}, id).Error
}

// ListModels preloads providers and orders by priority/ID, optionally filtering the model type.
func (r *LLMRepo) ListModels(ctx context.Context, typ string) ([]model.ModelConfig, error) {
	var list []model.ModelConfig
	q := r.db.WithContext(ctx).Preload("Provider").Order("priority ASC, id ASC")
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	err := q.Find(&list).Error
	return list, err
}

// GetModel preloads the provider so the service can validate availability and decrypt its credential.
func (r *LLMRepo) GetModel(ctx context.Context, id int64) (*model.ModelConfig, error) {
	var m model.ModelConfig
	if err := r.db.WithContext(ctx).Preload("Provider").First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// DefaultModel first tries the requested alias among currently available models.
// Models and providers must be enabled; cloud providers need a nonempty stored
// key, while Ollama may be keyless. Missing aliases fall back to is_default,
// priority, then ID ordering; decryption validity is checked later by the service.
func (r *LLMRepo) DefaultModel(ctx context.Context, alias string, typ string) (*model.ModelConfig, error) {
	var m model.ModelConfig
	available := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.ModelConfig{}).Preload("Provider").
			Joins("JOIN model_providers ON model_providers.id = model_configs.provider_id").
			Where("model_configs.type = ? AND model_configs.status = 1 AND model_providers.status = 1", typ).
			Where("model_providers.api_key <> '' OR model_providers.code = ?", "ollama")
	}
	err := available().Where("model_configs.alias = ?", alias).First(&m).Error
	if err == nil {
		return &m, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	// Fall back to the default model of this type, then the lowest priority value.
	err = available().Order("model_configs.is_default DESC, model_configs.priority ASC, model_configs.id ASC").First(&m).Error
	return &m, err
}
