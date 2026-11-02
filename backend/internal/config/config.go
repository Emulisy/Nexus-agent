// Package config loads environment files, YAML, and NX_ configuration overrides.
// Process variables take precedence over backend .env values, then project-root
// .env values. Base YAML is merged with an optional environment-specific file;
// recognized environment overrides take precedence over the merged settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

// Config groups settings consumed by API, worker, storage clients, and runtime engines.
type Config struct {
	// Env is selected from NX_ENV after .env loading and defaults to dev.
	Env    string `mapstructure:"env"`
	HTTP   HTTP   `mapstructure:"http"`
	Log    Log    `mapstructure:"log"`
	PG     PG     `mapstructure:"pg"`
	Redis  Redis  `mapstructure:"redis"`
	Milvus Milvus `mapstructure:"milvus"`
	MinIO  MinIO  `mapstructure:"minio"`
	JWT    JWT    `mapstructure:"jwt"`
	// SecretKey derives the AES key used to protect persisted provider credentials.
	SecretKey string  `mapstructure:"secret_key"`
	LLM       LLM     `mapstructure:"llm"`
	RAG       RAG     `mapstructure:"rag"`
	Agent     Agent   `mapstructure:"agent"`
	Worker    Worker  `mapstructure:"worker"`
	Sandbox   Sandbox `mapstructure:"sandbox"`
	A2A       A2A     `mapstructure:"a2a"`
	Search    Search  `mapstructure:"search"`
}

// HTTP configures the HTTP server listener and the deadline used during graceful shutdown.
type HTTP struct {
	// Port is stored as text so it can be appended directly to the listener host/port string.
	Port string `mapstructure:"port"`
	// ShutdownTimeout bounds HTTP server draining after a termination signal.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// Log selects the shared logger level and output format during process startup.
type Log struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// PG configures the PostgreSQL DSN and the underlying database/sql connection pool.
type PG struct {
	DSN          string `mapstructure:"dsn"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
	// ConnMaxLifetime limits how long a pooled PostgreSQL connection may be reused.
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// Redis identifies the database used for refresh allowlists and document job streams.
type Redis struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// Milvus configures the remote vector endpoint and the collection embedding dimension.
type Milvus struct {
	Addr string `mapstructure:"addr"`
	// Dim must match the embedding vectors stored in newly created collections.
	Dim int `mapstructure:"dim"`
}

// MinIO configures the S3-compatible object endpoint, credentials, bucket, and TLS selection.
type MinIO struct {
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Bucket    string `mapstructure:"bucket"`
	UseSSL    bool   `mapstructure:"use_ssl"`
}

// JWT supplies the signing secret and separate access/refresh lifetimes used when issuing tokens.
type JWT struct {
	// Secret signs both token types; the issuer distinguishes access from refresh tokens.
	Secret     string        `mapstructure:"secret"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

// LLM groups gateway concurrency/circuit settings and the aliases used by default-model resolution.
type LLM struct {
	MaxConnsPerProvider int           `mapstructure:"max_conns_per_provider"`
	CircuitErrorRate    float64       `mapstructure:"circuit_error_rate"`
	CircuitOpen         time.Duration `mapstructure:"circuit_open"`
	// DefaultChat is the preferred chat alias; repository selection can fall back to another available chat model.
	DefaultChat string `mapstructure:"default_chat"`
	// DefaultEmbedding is the preferred embedding alias used by retrieval and memory callbacks.
	DefaultEmbedding string `mapstructure:"default_embedding"`
}

// RAG controls default chunking, retrieval candidate counts, fusion, and optional reranking thresholds.
type RAG struct {
	ChunkSize int `mapstructure:"chunk_size"`
	// ChunkOverlap preserves source continuity between adjacent chunks.
	ChunkOverlap int `mapstructure:"chunk_overlap"`
	// DenseTopK bounds candidates returned by vector similarity search.
	DenseTopK int32 `mapstructure:"dense_top_k"`
	// SparseTopK bounds keyword-scored candidates before fusion.
	SparseTopK int32 `mapstructure:"sparse_top_k"`
	// FusedTopN limits the merged candidates offered to optional reranking.
	FusedTopN int32 `mapstructure:"fused_top_n"`
	// FinalTopK limits references returned by each knowledge-base retrieval.
	FinalTopK int32 `mapstructure:"final_top_k"`
	// RRFK smooths reciprocal-rank fusion scores from dense and keyword rankings.
	RRFK float64 `mapstructure:"rrf_k"`
	// ScoreThreshold is applied after successful reranking, whose scores differ from raw RRF scores.
	ScoreThreshold float32 `mapstructure:"score_threshold"`
	// BatchEmbed limits document chunks sent to the embedding provider in one ingestion batch.
	BatchEmbed int `mapstructure:"batch_embed"`
}

// Agent holds runtime timeouts and the post-response memory extraction window.
type Agent struct {
	// MaxIterations is retained globally; current chat runs take their limit from model.Agent.MaxIterations.
	MaxIterations int `mapstructure:"max_iterations"`
	// StepTimeout is passed to newly constructed ReAct runners for step execution.
	StepTimeout time.Duration `mapstructure:"step_timeout"`
	// RunTimeout is passed to ReAct runners as the overall run timeout.
	RunTimeout time.Duration `mapstructure:"run_timeout"`
	// MemoryExtractEvery is the number of recent messages extracted after each eligible response, not an interval between turns.
	MemoryExtractEvery int `mapstructure:"memory_extract_every"`
}

// Worker sets document consumer concurrency and the interval for reclaiming pending jobs.
type Worker struct {
	Concurrency int `mapstructure:"concurrency"`
	// ClaimInterval controls how often pending stream work is considered for reclamation.
	ClaimInterval time.Duration `mapstructure:"claim_interval"`
}

// Sandbox enables code execution and selects its container image and execution timeout.
type Sandbox struct {
	Enabled bool          `mapstructure:"enabled"`
	Image   string        `mapstructure:"image"`
	Timeout time.Duration `mapstructure:"timeout"`
}

// A2A configures the optional separate inbound Agent-to-Agent HTTP listener and its advertised card.
type A2A struct {
	Enabled bool `mapstructure:"enabled"`
	// Port belongs to the separate net/http listener rather than the HTTP server API listener.
	Port string `mapstructure:"port"`
	Name string `mapstructure:"name"`
	Desc string `mapstructure:"desc"`
}

// Search selects the SearxNG endpoint used by the built-in web search tool.
type Search struct {
	SearxngURL string `mapstructure:"searxng_url"`
}

// Load discovers configuration relative to the backend/project working directory or executable.
// It loads .env values before selecting NX_ENV, so an environment file can choose
// the optional config.<env>.yaml overlay. Missing overlays are allowed; malformed
// files, decoding failures, or invalid production secrets are returned to startup.
func Load(configDir string) (*Config, error) {
	if configDir == "" {
		for _, dir := range []string{"configs", filepath.Join("backend", "configs"), filepath.Join(exeDir(), "configs")} {
			if isDir(dir) {
				configDir = dir
				break
			}
		}
		if configDir == "" {
			configDir = "configs"
		}
	}
	if err := loadDotEnv(configDir); err != nil {
		return nil, err
	}

	env := os.Getenv("NX_ENV")
	if env == "" {
		env = "dev"
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config.yaml: %w", err)
	}
	v.SetConfigName("config." + env)
	if err := v.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("merge config.%s.yaml: %w", env, err)
		}
	}

	// Nested keys use double underscores, for example NX_HTTP__PORT.
	v.SetEnvPrefix("NX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

	// Bind the HTTP port explicitly so an environment-only value is included during unmarshalling.
	if s := os.Getenv("NX_HTTP__PORT"); s != "" {
		v.Set("http.port", s)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.Env = env
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadDotEnv parses the backend and project environment files before exporting values.
// Missing files are allowed, the more specific file wins, and existing process
// variables are preserved even when empty. Syntax errors omit their source line
// to avoid exposing credentials. SA_SEED_ADMIN_PASSWORD fills the NX alias only
// when NX_SEED_ADMIN_PASSWORD is absent.
func loadDotEnv(configDir string) error {
	values := make(map[string]string)
	// Load the more specific file first; the project file only fills missing values.
	for _, name := range []string{
		filepath.Join(configDir, "..", ".env"),
		filepath.Join(configDir, "..", "..", ".env"),
	} {
		file, err := os.Open(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open dotenv file %s: %w", name, err)
		}
		parsed, parseErr := gotenv.StrictParse(file)
		closeErr := file.Close()
		if parseErr != nil {
			// Parser errors may include the entire input line, including credentials.
			return fmt.Errorf("invalid dotenv syntax in %s; expected KEY=value entries", name)
		}
		if closeErr != nil {
			return fmt.Errorf("close dotenv file %s: %w", name, closeErr)
		}
		for key, value := range parsed {
			if _, exists := values[key]; !exists {
				values[key] = value
			}
		}
	}
	// Parse all files before exporting anything, so invalid files have no partial effect.
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set dotenv variable %s: invalid environment variable", key)
		}
	}
	if _, exists := os.LookupEnv("NX_SEED_ADMIN_PASSWORD"); !exists {
		if value, exists := os.LookupEnv("SA_SEED_ADMIN_PASSWORD"); exists {
			return os.Setenv("NX_SEED_ADMIN_PASSWORD", value)
		}
	}
	return nil
}

// validate enforces distinct nondefault JWT/encryption secrets in prod/production.
// Development settings bypass these production-only checks; this function does
// not establish storage connectivity or validate every runtime setting.
func validate(cfg *Config) error {
	if cfg.Env != "prod" && cfg.Env != "production" {
		return nil
	}
	if len(cfg.JWT.Secret) < 32 || cfg.JWT.Secret == "change-me-in-prod" {
		return fmt.Errorf("production requires NX_JWT__SECRET to be a unique secret of at least 32 characters")
	}
	if len(cfg.SecretKey) < 32 || cfg.SecretKey == "change-me-32-bytes!!" {
		return fmt.Errorf("production requires NX_SECRET_KEY to be a unique secret of at least 32 characters")
	}
	if cfg.JWT.Secret == cfg.SecretKey {
		return fmt.Errorf("production requires NX_JWT__SECRET and NX_SECRET_KEY to be different secrets")
	}
	return nil
}

// isDir treats missing or inaccessible paths as non-directories during config discovery.
func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// exeDir returns the executable directory and falls back to the current directory when it cannot be resolved.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
