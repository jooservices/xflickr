// Package config loads and validates the application's environment settings.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config contains the settings used by the application's platform adapters.
// Values are loaded from the process environment, with an optional .env file
// for local development.
type Config struct {
	DB       DBConfig
	Redis    RedisConfig
	Search   SearchConfig
	Blob     BlobConfig
	AI       AIConfig
	Obs      ObsConfig
	Node     NodeConfig
	Crypto   CryptoConfig
	Identity IdentityConfig
	Flickr   FlickrConfig

	HTTPPort int
	LogLevel string
}

type runtimeConfig struct {
	HTTPPort int    `env:"HTTP_PORT" envDefault:"8080"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}

// DBConfig contains PostgreSQL connection settings.
type DBConfig struct {
	Host              string        `env:"DB_HOST"`
	Port              int           `env:"DB_PORT" envDefault:"5432"`
	Name              string        `env:"DB_NAME"`
	User              string        `env:"DB_USER"`
	Password          string        `env:"DB_PASSWORD"`
	SSLMode           string        `env:"DB_SSLMODE" envDefault:"disable"`
	MaxConns          int32         `env:"DB_MAX_CONNS" envDefault:"20"`
	MinConns          int32         `env:"DB_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime   time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"1h"`
	MaxConnIdleTime   time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"30m"`
	HealthCheckPeriod time.Duration `env:"DB_HEALTH_CHECK_PERIOD" envDefault:"1m"`
}

// RedisConfig contains Redis connection and namespace settings.
type RedisConfig struct {
	Host     string `env:"REDIS_HOST" envDefault:"127.0.0.1"`
	Port     int    `env:"REDIS_PORT" envDefault:"16382"`
	DB       int    `env:"REDIS_DB" envDefault:"8"`
	Password string `env:"REDIS_PASSWORD"`
	Prefix   string `env:"REDIS_PREFIX"`
}

// SearchConfig contains settings for the shared SearchIndex service.
type SearchConfig struct {
	URL         string `env:"SEARCH_URL" envDefault:"http://127.0.0.1:19200"`
	Username    string `env:"SEARCH_USERNAME"`
	Password    string `env:"SEARCH_PASSWORD"`
	IndexPrefix string `env:"OPENSEARCH_INDEX_PREFIX"`
}

// BlobConfig contains local filesystem and optional S3-compatible settings.
type BlobConfig struct {
	Driver         string `env:"BLOB_DRIVER" envDefault:"local"`
	LocalDir       string `env:"BLOB_LOCAL_DIR" envDefault:"var/blobs"`
	S3Endpoint     string `env:"S3_ENDPOINT"`
	S3Region       string `env:"S3_REGION" envDefault:"us-east-1"`
	S3Bucket       string `env:"S3_BUCKET"`
	S3AccessKey    string `env:"S3_ACCESS_KEY"`
	S3SecretKey    string `env:"S3_SECRET_KEY"`
	S3UsePathStyle bool   `env:"S3_USE_PATH_STYLE" envDefault:"false"`
}

// AIConfig contains Ollama connection and model settings.
type AIConfig struct {
	URL            string `env:"OLLAMA_URL" envDefault:"http://127.0.0.1:11434"`
	CaptionModel   string `env:"OLLAMA_CAPTION_MODEL" envDefault:"llama3.2"`
	EmbeddingModel string `env:"OLLAMA_EMBEDDING_MODEL" envDefault:"nomic-embed-text"`
}

// ObsConfig contains OpenObserve connection settings.
type ObsConfig struct {
	URL      string `env:"OPENOBSERVE_URL" envDefault:"http://127.0.0.1:15080"`
	Username string `env:"OPENOBSERVE_USERNAME"`
	Password string `env:"OPENOBSERVE_PASSWORD"`
	Org      string `env:"OPENOBSERVE_ORG" envDefault:"default"`
	Stream   string `env:"OPENOBSERVE_STREAM" envDefault:"xflickr"`
}

// NodeConfig contains the identity for this application node.
type NodeConfig struct {
	ID string `env:"NODE_ID"`
}

// CryptoConfig contains the versioned master keys used to encrypt application secrets.
type CryptoConfig struct {
	MasterKeys        string `env:"XFLICKR_MASTER_KEYS"`
	ActiveMasterKeyID string `env:"XFLICKR_MASTER_KEY_ACTIVE"`
}

// IdentityConfig contains password hashing parameters.
type IdentityConfig struct {
	Argon2Memory      uint32 `env:"ARGON2ID_MEMORY" envDefault:"65536"`
	Argon2Iterations  uint32 `env:"ARGON2ID_ITERATIONS" envDefault:"3"`
	Argon2Parallelism uint8  `env:"ARGON2ID_PARALLELISM" envDefault:"2"`
	Argon2SaltLength  uint32 `env:"ARGON2ID_SALT_LENGTH" envDefault:"16"`
	Argon2KeyLength   uint32 `env:"ARGON2ID_KEY_LENGTH" envDefault:"32"`
}

// FlickrConfig contains the Flickr OAuth callback. The API key and secret are
// entered in the application UI and never configured here.
type FlickrConfig struct {
	CallbackURL string `env:"FLICKR_OAUTH_CALLBACK_URL" envDefault:"http://127.0.0.1:8080/flickr-accounts/callback"`
}

var requiredEnvironment = []string{
	"DB_HOST",
	"DB_NAME",
	"DB_USER",
	"REDIS_PREFIX",
	"OPENSEARCH_INDEX_PREFIX",
	"NODE_ID",
	"XFLICKR_MASTER_KEYS",
	"XFLICKR_MASTER_KEY_ACTIVE",
}

// Load reads .env when it exists, then overlays the process environment and
// parses the resulting settings. Existing process variables take precedence.
func Load() (Config, error) {
	return LoadWithDotEnv(".env")
}

// LoadWithDotEnv reads an optional dotenv file, overlays the process
// environment, and loads the resulting settings.
func LoadWithDotEnv(path string) (Config, error) {
	values, err := environmentValues(path)
	if err != nil {
		return Config{}, err
	}
	return LoadFromEnv(values)
}

// LoadDatabase reads only PostgreSQL settings from process environment and an
// optional .env file. This keeps database tooling usable with the generated
// worktree environment, which intentionally contains no application secrets.
func LoadDatabase() (DBConfig, error) {
	return LoadDatabaseWithDotEnv(".env")
}

// LoadDatabaseWithDotEnv reads PostgreSQL settings from an optional dotenv
// file, then overlays the process environment.
func LoadDatabaseWithDotEnv(path string) (DBConfig, error) {
	values, err := environmentValues(path)
	if err != nil {
		return DBConfig{}, err
	}
	return LoadDatabaseFromEnv(values)
}

// LoadDatabaseFromEnv parses and validates only PostgreSQL settings.
func LoadDatabaseFromEnv(values map[string]string) (DBConfig, error) {
	values = normalizeDatabaseEnvironment(values)
	missing := missingDatabaseRequired(values)
	if len(missing) > 0 {
		return DBConfig{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	var cfg DBConfig
	if err := env.ParseWithOptions(&cfg, env.Options{Environment: values}); err != nil {
		return DBConfig{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return DBConfig{}, err
	}
	return cfg, nil
}

func environmentValues(path string) (map[string]string, error) {
	values := make(map[string]string)
	if path != "" {
		fileValues, err := godotenv.Read(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read dotenv file: %w", err)
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}

	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	return values, nil
}

// LoadFromEnv parses settings from the supplied environment map. It is also
// useful to callers that already manage environment loading themselves.
func LoadFromEnv(values map[string]string) (Config, error) {
	values = normalizeDatabaseEnvironment(values)
	missing := missingRequired(values)
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	var cfg Config
	var runtime runtimeConfig
	for _, target := range []any{
		&cfg.DB,
		&cfg.Redis,
		&cfg.Search,
		&cfg.Blob,
		&cfg.AI,
		&cfg.Obs,
		&cfg.Node,
		&cfg.Crypto,
		&cfg.Identity,
		&cfg.Flickr,
		&runtime,
	} {
		if err := env.ParseWithOptions(target, env.Options{Environment: values}); err != nil {
			return Config{}, fmt.Errorf("parse environment: %w", err)
		}
	}
	cfg.HTTPPort = runtime.HTTPPort
	cfg.LogLevel = runtime.LogLevel
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func missingRequired(values map[string]string) []string {
	missing := make([]string, 0)
	for _, name := range requiredEnvironment {
		if strings.TrimSpace(values[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func missingDatabaseRequired(values map[string]string) []string {
	missing := make([]string, 0, 3)
	for _, name := range []string{"DB_HOST", "DB_NAME", "DB_USER"} {
		if strings.TrimSpace(values[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func normalizeDatabaseEnvironment(values map[string]string) map[string]string {
	normalized := make(map[string]string, len(values)+2)
	for key, value := range values {
		normalized[key] = value
	}
	if value := strings.TrimSpace(normalized["DB_DATABASE"]); value != "" {
		normalized["DB_NAME"] = value
	}
	if value := strings.TrimSpace(normalized["DB_USERNAME"]); value != "" {
		normalized["DB_USER"] = value
	}
	return normalized
}

// Validate checks PostgreSQL connection and pool settings before use.
func (cfg DBConfig) Validate() error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("DB_HOST is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("DB_NAME is required")
	}
	if strings.TrimSpace(cfg.User) == "" {
		return fmt.Errorf("DB_USER is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("DB_PORT must be between 1 and 65535")
	}
	if cfg.MaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS must be at least 1")
	}
	if cfg.MinConns < 0 || cfg.MinConns > cfg.MaxConns {
		return fmt.Errorf("DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
	}
	if cfg.MaxConnLifetime <= 0 {
		return fmt.Errorf("DB_MAX_CONN_LIFETIME must be greater than 0")
	}
	if cfg.MaxConnIdleTime <= 0 {
		return fmt.Errorf("DB_MAX_CONN_IDLE_TIME must be greater than 0")
	}
	if cfg.HealthCheckPeriod <= 0 {
		return fmt.Errorf("DB_HEALTH_CHECK_PERIOD must be greater than 0")
	}
	return nil
}

//nolint:cyclop // flat list of independent config invariants
func (cfg *Config) validate() error {
	if cfg.Identity.Argon2Memory < 8*uint32(cfg.Identity.Argon2Parallelism) || cfg.Identity.Argon2Iterations == 0 || cfg.Identity.Argon2Parallelism == 0 || cfg.Identity.Argon2SaltLength < 8 || cfg.Identity.Argon2KeyLength < 16 {
		return fmt.Errorf("ARGON2ID parameters are invalid")
	}
	callback, err := url.Parse(strings.TrimSpace(cfg.Flickr.CallbackURL))
	if err != nil || callback.Host == "" || (callback.Scheme != "http" && callback.Scheme != "https") {
		return fmt.Errorf("FLICKR_OAUTH_CALLBACK_URL must be an absolute http or https URL")
	}
	cfg.Flickr.CallbackURL = callback.String()
	if cfg.HTTPPort < 1 || cfg.HTTPPort > 65535 {
		return fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}
	if err := cfg.DB.Validate(); err != nil {
		return err
	}
	if cfg.Redis.Port < 1 || cfg.Redis.Port > 65535 {
		return fmt.Errorf("REDIS_PORT must be between 1 and 65535")
	}

	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, or error")
	}

	cfg.Blob.Driver = strings.ToLower(strings.TrimSpace(cfg.Blob.Driver))
	switch cfg.Blob.Driver {
	case "local", "s3":
	default:
		return fmt.Errorf("BLOB_DRIVER must be local or s3")
	}
	if cfg.Blob.Driver == "s3" {
		var missing []string
		if strings.TrimSpace(cfg.Blob.S3Endpoint) == "" {
			missing = append(missing, "S3_ENDPOINT")
		}
		if strings.TrimSpace(cfg.Blob.S3Bucket) == "" {
			missing = append(missing, "S3_BUCKET")
		}
		if strings.TrimSpace(cfg.Blob.S3AccessKey) == "" {
			missing = append(missing, "S3_ACCESS_KEY")
		}
		if strings.TrimSpace(cfg.Blob.S3SecretKey) == "" {
			missing = append(missing, "S3_SECRET_KEY")
		}
		if len(missing) > 0 {
			return fmt.Errorf("BLOB_DRIVER=s3 requires: %s", strings.Join(missing, ", "))
		}
	}
	return nil
}

// String returns a useful configuration summary without exposing credentials
// or encryption keys. Endpoints are represented as configured flags because
// URL user information and query parameters may also contain secrets.
func (cfg Config) String() string {
	return fmt.Sprintf(
		"Config{DB:{host:%q port:%d name:%q user:%q password:%s}, Redis:{host:%q port:%d db:%d prefix:%q password:%s}, Search:{endpoint_configured:%t username:%q password:%s index_prefix:%q}, Blob:{driver:%q local_dir:%q s3_endpoint_configured:%t s3_region:%q s3_bucket:%q s3_access_key:%s s3_secret_key:%s}, AI:{endpoint_configured:%t caption_model:%q embedding_model:%q}, Obs:{endpoint_configured:%t org:%q stream:%q username:%q password:%s}, Node:{id:%q}, Crypto:{master_keys:%s active_master_key_id:%s}, HTTPPort:%d LogLevel:%q}",
		cfg.DB.Host, cfg.DB.Port, cfg.DB.Name, cfg.DB.User, mask(cfg.DB.Password),
		cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.DB, cfg.Redis.Prefix, mask(cfg.Redis.Password),
		cfg.Search.URL != "", cfg.Search.Username, mask(cfg.Search.Password), cfg.Search.IndexPrefix,
		cfg.Blob.Driver, cfg.Blob.LocalDir, cfg.Blob.S3Endpoint != "", cfg.Blob.S3Region, cfg.Blob.S3Bucket,
		mask(cfg.Blob.S3AccessKey), mask(cfg.Blob.S3SecretKey),
		cfg.AI.URL != "", cfg.AI.CaptionModel, cfg.AI.EmbeddingModel,
		cfg.Obs.URL != "", cfg.Obs.Org, cfg.Obs.Stream, cfg.Obs.Username, mask(cfg.Obs.Password),
		cfg.Node.ID, mask(cfg.Crypto.MasterKeys), mask(cfg.Crypto.ActiveMasterKeyID), cfg.HTTPPort, cfg.LogLevel,
	)
}

func mask(value string) string {
	if value == "" {
		return "<empty>"
	}
	return "<redacted>"
}
