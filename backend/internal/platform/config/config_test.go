package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	cfg, err := LoadFromEnv(requiredEnv())
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.DB.Port != 5432 || cfg.DB.SSLMode != "disable" {
		t.Errorf("DB defaults = port %d, sslmode %q", cfg.DB.Port, cfg.DB.SSLMode)
	}
	if cfg.Redis.Host != "127.0.0.1" || cfg.Redis.Port != 16382 || cfg.Redis.DB != 8 {
		t.Errorf("Redis defaults = %#v", cfg.Redis)
	}
	if cfg.Search.URL != "http://127.0.0.1:19200" {
		t.Errorf("Search URL default = %q", cfg.Search.URL)
	}
	if cfg.Blob.Driver != "local" || cfg.Blob.LocalDir != "var/blobs" || cfg.Blob.MaxBytes != 10737418240 || cfg.Blob.S3Region != "us-east-1" {
		t.Errorf("Blob defaults = %#v", cfg.Blob)
	}
	if cfg.DownloadLinkTTL != 15*time.Minute {
		t.Errorf("download link TTL default = %s", cfg.DownloadLinkTTL)
	}
	if cfg.AI.URL != "http://127.0.0.1:11434" || cfg.AI.CaptionModel != "llama3.2" || cfg.AI.EmbeddingModel != "nomic-embed-text" {
		t.Errorf("AI defaults = %#v", cfg.AI)
	}
	if cfg.Obs.URL != "http://127.0.0.1:15080" || cfg.Obs.Org != "default" || cfg.Obs.Stream != "xflickr" {
		t.Errorf("Obs defaults = %#v", cfg.Obs)
	}
	if cfg.Flickr.CallbackURL != "http://127.0.0.1:8080/flickr-accounts/callback" {
		t.Errorf("Flickr callback default = %q", cfg.Flickr.CallbackURL)
	}
	if cfg.CallLog.RetentionDays != 30 {
		t.Errorf("call log retention default = %d", cfg.CallLog.RetentionDays)
	}
	if cfg.HTTPPort != 8080 || cfg.InternalHTTPAddr != "" || cfg.LogLevel != "info" {
		t.Errorf("runtime defaults = port %d, level %q", cfg.HTTPPort, cfg.LogLevel)
	}
}

func TestLoadFromEnvReadsFieldsAndTicketPrefixes(t *testing.T) {
	values := requiredEnv()
	testDBPassword := strings.Repeat("x", 32)
	for key, value := range map[string]string{
		"DB_HOST":                     "db.internal",
		"DB_PORT":                     "5544",
		"DB_NAME":                     "worktree_db",
		"DB_USER":                     "worker",
		"DB_PASSWORD":                 testDBPassword,
		"DB_SSLMODE":                  "require",
		"DB_MAX_CONNS":                "35",
		"DB_MIN_CONNS":                "4",
		"DB_MAX_CONN_LIFETIME":        "45m",
		"DB_MAX_CONN_IDLE_TIME":       "12m",
		"DB_HEALTH_CHECK_PERIOD":      "30s",
		"REDIS_HOST":                  "redis.internal",
		"REDIS_PORT":                  "16383",
		"REDIS_DB":                    "9",
		"REDIS_PASSWORD":              "redis-pass",
		"REDIS_PREFIX":                "xflickr_wt68:",
		"SEARCH_URL":                  "http://search.internal:9200",
		"SEARCH_USERNAME":             "search-user",
		"SEARCH_PASSWORD":             "search-pass",
		"OPENSEARCH_INDEX_PREFIX":     "xflickr_wt68_",
		"BLOB_DRIVER":                 "s3",
		"BLOB_LOCAL_DIR":              "/tmp/blobs",
		"BLOB_MAX_BYTES":              "1234567",
		"BLOB_MAX_TTL":                "48h",
		"DOWNLOAD_LINK_TTL":           "2h",
		"S3_ENDPOINT":                 "https://objects.internal",
		"S3_REGION":                   "ap-southeast-1",
		"S3_BUCKET":                   "xflickr-worktree",
		"S3_ACCESS_KEY":               "s3-access",
		"S3_SECRET_KEY":               "s3-secret",
		"S3_USE_PATH_STYLE":           "true",
		"OLLAMA_URL":                  "http://ollama.internal:11434",
		"OLLAMA_CAPTION_MODEL":        "caption-model",
		"OLLAMA_EMBEDDING_MODEL":      "embedding-model",
		"OPENOBSERVE_URL":             "https://observe.internal",
		"OPENOBSERVE_USERNAME":        "observe-user",
		"OPENOBSERVE_PASSWORD":        "observe-pass",
		"OPENOBSERVE_ORG":             "xflickr-org",
		"OPENOBSERVE_STREAM":          "worktree-logs",
		"API_CALL_LOG_RETENTION_DAYS": "45",
		"NODE_ID":                     "node-worktree-68",
		"XFLICKR_MASTER_KEYS":         "test-v1:AA==",
		"XFLICKR_MASTER_KEY_ACTIVE":   "test-v1",
		"HTTP_PORT":                   "9080",
		"INTERNAL_HTTP_ADDR":          "192.168.2.10:8081",
		"LOG_LEVEL":                   "DEBUG",
	} {
		values[key] = value
	}

	cfg, err := LoadFromEnv(values)
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.DB.Host != "db.internal" || cfg.DB.Port != 5544 || cfg.DB.Name != "worktree_db" || cfg.DB.User != "worker" || cfg.DB.Password != testDBPassword || cfg.DB.SSLMode != "require" {
		t.Errorf("DB values not loaded: %#v", cfg.DB)
	}
	if cfg.DB.MaxConns != 35 || cfg.DB.MinConns != 4 || cfg.DB.MaxConnLifetime != 45*time.Minute || cfg.DB.MaxConnIdleTime != 12*time.Minute || cfg.DB.HealthCheckPeriod != 30*time.Second {
		t.Errorf("DB pool values not loaded: %#v", cfg.DB)
	}
	if cfg.Redis.Host != "redis.internal" || cfg.Redis.Port != 16383 || cfg.Redis.DB != 9 || cfg.Redis.Password != "redis-pass" || cfg.Redis.Prefix != "xflickr_wt68:" {
		t.Errorf("Redis values not loaded: %#v", cfg.Redis)
	}
	if cfg.Search.URL != "http://search.internal:9200" || cfg.Search.Username != "search-user" || cfg.Search.Password != "search-pass" || cfg.Search.IndexPrefix != "xflickr_wt68_" {
		t.Errorf("Search values not loaded: %#v", cfg.Search)
	}
	if cfg.Blob.Driver != "s3" || cfg.Blob.LocalDir != "/tmp/blobs" || cfg.Blob.MaxBytes != 1234567 || cfg.Blob.S3Endpoint != "https://objects.internal" || cfg.Blob.S3Region != "ap-southeast-1" || cfg.Blob.S3Bucket != "xflickr-worktree" || cfg.Blob.S3AccessKey != "s3-access" || cfg.Blob.S3SecretKey != "s3-secret" || !cfg.Blob.S3UsePathStyle {
		t.Errorf("Blob values not loaded: %#v", cfg.Blob)
	}
	if cfg.DownloadLinkTTL != 2*time.Hour {
		t.Errorf("download link TTL = %s, want 2h", cfg.DownloadLinkTTL)
	}
	if cfg.AI.URL != "http://ollama.internal:11434" || cfg.AI.CaptionModel != "caption-model" || cfg.AI.EmbeddingModel != "embedding-model" {
		t.Errorf("AI values not loaded: %#v", cfg.AI)
	}
	if cfg.Obs.URL != "https://observe.internal" || cfg.Obs.Username != "observe-user" || cfg.Obs.Password != "observe-pass" || cfg.Obs.Org != "xflickr-org" || cfg.Obs.Stream != "worktree-logs" {
		t.Errorf("Obs values not loaded: %#v", cfg.Obs)
	}
	if cfg.Node.ID != "node-worktree-68" || cfg.Crypto.MasterKeys != "test-v1:AA==" || cfg.Crypto.ActiveMasterKeyID != "test-v1" || cfg.HTTPPort != 9080 || cfg.InternalHTTPAddr != "192.168.2.10:8081" || cfg.LogLevel != "debug" {
		t.Errorf("identity/runtime values not loaded: node %#v crypto %#v port %d level %q", cfg.Node, cfg.Crypto, cfg.HTTPPort, cfg.LogLevel)
	}
	if cfg.CallLog.RetentionDays != 45 {
		t.Errorf("call log retention = %d, want 45", cfg.CallLog.RetentionDays)
	}
}

func TestCallLogRetentionDaysValidation(t *testing.T) {
	for _, value := range []string{"0", "-1", "3651", "invalid"} {
		values := requiredEnv()
		values["API_CALL_LOG_RETENTION_DAYS"] = value
		_, err := LoadFromEnv(values)
		if err == nil {
			t.Errorf("retention %q error = %v", value, err)
		} else if value != "invalid" && !strings.Contains(err.Error(), "API_CALL_LOG_RETENTION_DAYS") {
			t.Errorf("retention %q error = %v", value, err)
		}
	}
}

func TestLoadFromEnvListsEveryMissingRequiredName(t *testing.T) {
	_, err := LoadFromEnv(map[string]string{})
	if err == nil {
		t.Fatal("LoadFromEnv() succeeded with no required settings")
	}
	message := err.Error()
	for _, name := range requiredEnvironment {
		if !strings.Contains(message, name) {
			t.Errorf("missing-variable error %q does not contain %s", message, name)
		}
	}
	if strings.Count(message, "missing required environment variables:") != 1 {
		t.Errorf("expected one combined missing-variable error, got %q", message)
	}
}

func TestLoadFromEnvValidatesSettings(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "http port low", key: "HTTP_PORT", value: "0", want: "HTTP_PORT"},
		{name: "database port high", key: "DB_PORT", value: "65536", want: "DB_PORT"},
		{name: "database pool minimum exceeds maximum", key: "DB_MIN_CONNS", value: "21", want: "DB_MIN_CONNS"},
		{name: "database pool maximum is zero", key: "DB_MAX_CONNS", value: "0", want: "DB_MAX_CONNS"},
		{name: "database pool lifetime is invalid", key: "DB_MAX_CONN_LIFETIME", value: "0s", want: "DB_MAX_CONN_LIFETIME"},
		{name: "redis port invalid", key: "REDIS_PORT", value: "not-a-port", want: "parse environment"},
		{name: "log level", key: "LOG_LEVEL", value: "trace", want: "LOG_LEVEL"},
		{name: "blob driver", key: "BLOB_DRIVER", value: "garage", want: "BLOB_DRIVER"},
		{name: "blob cap", key: "BLOB_MAX_BYTES", value: "-1", want: "BLOB_MAX_BYTES"},
		{name: "blob ttl", key: "BLOB_MAX_TTL", value: "0s", want: "BLOB_MAX_TTL"},
		{name: "download link ttl", key: "DOWNLOAD_LINK_TTL", value: "0s", want: "DOWNLOAD_LINK_TTL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := requiredEnv()
			values[test.key] = test.value
			_, err := LoadFromEnv(values)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadFromEnv() error = %v, want an error containing %q", err, test.want)
			}
		})
	}

	values := requiredEnv()
	values["BLOB_DRIVER"] = "s3"
	_, err := LoadFromEnv(values)
	if err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY") {
		t.Errorf("incomplete S3 configuration error = %v", err)
	}

	values = requiredEnv()
	values["DOWNLOAD_LINK_TTL"] = "48h"
	_, err = LoadFromEnv(values)
	if err == nil || !strings.Contains(err.Error(), "DOWNLOAD_LINK_TTL must not exceed BLOB_MAX_TTL") {
		t.Errorf("link TTL beyond blob limit error = %v", err)
	}
}

func TestLoadWithDotEnvOptionalFileAndProcessPrecedence(t *testing.T) {
	clearConfigEnvironment(t)
	path := filepath.Join(t.TempDir(), ".env")
	contents := strings.Join([]string{
		"DB_HOST=dotenv-db",
		"DB_NAME=dotenv-name",
		"DB_USER=dotenv-user",
		"REDIS_PREFIX=xflickr_dotenv:",
		"OPENSEARCH_INDEX_PREFIX=xflickr_dotenv_",
		"NODE_ID=dotenv-node",
		"XFLICKR_MASTER_KEYS=dotenv-v1:AA==",
		"XFLICKR_MASTER_KEY_ACTIVE=dotenv-v1",
		"LOG_LEVEL=debug",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write dotenv file: %v", err)
	}
	t.Setenv("DB_HOST", "process-db")

	cfg, err := LoadWithDotEnv(path)
	if err != nil {
		t.Fatalf("LoadWithDotEnv() error = %v", err)
	}
	if cfg.DB.Host != "process-db" || cfg.DB.Name != "dotenv-name" || cfg.LogLevel != "debug" {
		t.Errorf("dotenv/process values = DB %#v, log level %q", cfg.DB, cfg.LogLevel)
	}
}

func TestLoadWithDotEnvIgnoresMissingOptionalFile(t *testing.T) {
	clearConfigEnvironment(t)
	for key, value := range requiredEnv() {
		t.Setenv(key, value)
	}
	cfg, err := LoadWithDotEnv(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatalf("LoadWithDotEnv() with absent optional file error = %v", err)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("HTTP_PORT default = %d, want 8080", cfg.HTTPPort)
	}
}

func TestLoadDatabaseFromEnvSupportsGeneratedWorktreeNames(t *testing.T) {
	cfg, err := LoadDatabaseFromEnv(map[string]string{
		"DB_HOST":     "127.0.0.1",
		"DB_PORT":     "15435",
		"DB_DATABASE": "xflickr_wtk80",
		"DB_USERNAME": "shared-user",
		"DB_PASSWORD": "shared-password",
	})
	if err != nil {
		t.Fatalf("LoadDatabaseFromEnv() error = %v", err)
	}
	if cfg.Name != "xflickr_wtk80" || cfg.User != "shared-user" || cfg.Port != 15435 || cfg.SSLMode != "disable" {
		t.Errorf("database settings = %#v", cfg)
	}
	if cfg.MaxConns != 20 || cfg.MinConns != 2 || cfg.MaxConnLifetime != time.Hour || cfg.MaxConnIdleTime != 30*time.Minute || cfg.HealthCheckPeriod != time.Minute {
		t.Errorf("database pool defaults = %#v", cfg)
	}
}

func TestLoadDatabaseWithDotEnvAppliesProcessOverrides(t *testing.T) {
	clearConfigEnvironment(t)
	path := filepath.Join(t.TempDir(), ".env")
	contents := strings.Join([]string{
		"DB_HOST=dotenv-db",
		"DB_DATABASE=dotenv-database",
		"DB_USERNAME=dotenv-user",
		"DB_MAX_CONNS=12",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write dotenv file: %v", err)
	}
	t.Setenv("DB_HOST", "process-db")
	t.Setenv("DB_DATABASE", "process-database")

	cfg, err := LoadDatabaseWithDotEnv(path)
	if err != nil {
		t.Fatalf("LoadDatabaseWithDotEnv() error = %v", err)
	}
	if cfg.Host != "process-db" || cfg.Name != "process-database" || cfg.User != "dotenv-user" || cfg.MaxConns != 12 {
		t.Errorf("database dotenv/process values = %#v", cfg)
	}
}

func TestLoadWithDotEnvReportsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("not a valid dotenv assignment\n"), 0o600); err != nil {
		t.Fatalf("write malformed dotenv file: %v", err)
	}
	_, err := LoadWithDotEnv(path)
	if err == nil || !strings.Contains(err.Error(), "read dotenv file") {
		t.Fatalf("LoadWithDotEnv() error = %v, want dotenv read error", err)
	}
}

func TestConfigStringRedactsSecrets(t *testing.T) {
	values := requiredEnv()
	values["DB_PASSWORD"] = "secret-db-password"
	values["REDIS_PASSWORD"] = "secret-redis-password"
	values["SEARCH_PASSWORD"] = "secret-search-password"
	values["S3_ACCESS_KEY"] = "secret-s3-access"
	values["S3_SECRET_KEY"] = "secret-s3-secret"
	values["OPENOBSERVE_PASSWORD"] = "secret-observe-password"
	values["XFLICKR_MASTER_KEYS"] = "secret-master-key-list"
	values["XFLICKR_MASTER_KEY_ACTIVE"] = "secret-active-id"
	values["SEARCH_URL"] = "https://url-user:url-password@search.internal/?token=url-token"
	values["S3_ENDPOINT"] = "https://access:secret@objects.internal/?token=s3-token"
	values["OPENOBSERVE_URL"] = "https://observe.internal/?token=observe-token"
	cfg, err := LoadFromEnv(values)
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	result := cfg.String()
	for _, secret := range []string{
		"secret-db-password", "secret-redis-password", "secret-search-password",
		"secret-s3-access", "secret-s3-secret", "secret-observe-password",
		"secret-master-key-list", "secret-active-id", "url-user", "url-password", "url-token", "s3-token", "observe-token",
	} {
		if strings.Contains(result, secret) {
			t.Errorf("Config.String() exposed %q: %s", secret, result)
		}
	}
	if !strings.Contains(result, "<redacted>") || !strings.Contains(result, "endpoint_configured:true") {
		t.Errorf("Config.String() did not include safe masked summary: %s", result)
	}
	if got := (Config{}).String(); !strings.Contains(got, "<empty>") {
		t.Errorf("Config.String() for empty values = %s, want empty-secret marker", got)
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"DB_HOST":                   "127.0.0.1",
		"DB_NAME":                   "xflickr_test",
		"DB_USER":                   "postgres",
		"REDIS_PREFIX":              "xflickr_test:",
		"OPENSEARCH_INDEX_PREFIX":   "xflickr_test_",
		"NODE_ID":                   "xflickr-test",
		"XFLICKR_MASTER_KEYS":       "test-v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"XFLICKR_MASTER_KEY_ACTIVE": "test-v1",
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD", "DB_SSLMODE",
		"DB_MAX_CONNS", "DB_MIN_CONNS", "DB_MAX_CONN_LIFETIME", "DB_MAX_CONN_IDLE_TIME", "DB_HEALTH_CHECK_PERIOD",
		"REDIS_HOST", "REDIS_PORT", "REDIS_DB", "REDIS_PASSWORD", "REDIS_PREFIX",
		"SEARCH_URL", "SEARCH_USERNAME", "SEARCH_PASSWORD", "OPENSEARCH_INDEX_PREFIX",
		"BLOB_DRIVER", "BLOB_LOCAL_DIR", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_PATH_STYLE",
		"OLLAMA_URL", "OLLAMA_CAPTION_MODEL", "OLLAMA_EMBEDDING_MODEL",
		"OPENOBSERVE_URL", "OPENOBSERVE_USERNAME", "OPENOBSERVE_PASSWORD", "OPENOBSERVE_ORG", "OPENOBSERVE_STREAM",
		"NODE_ID", "XFLICKR_MASTER_KEYS", "XFLICKR_MASTER_KEY_ACTIVE", "HTTP_PORT", "LOG_LEVEL",
	} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

func TestLoadFromEnvValidatesTheFlickrCallbackURL(t *testing.T) {
	for _, bad := range []string{"/flickr-accounts/callback", "ftp://example.com/cb", "not a url", "http://"} {
		values := requiredEnv()
		values["FLICKR_OAUTH_CALLBACK_URL"] = bad
		if _, err := LoadFromEnv(values); err == nil || !strings.Contains(err.Error(), "FLICKR_OAUTH_CALLBACK_URL") {
			t.Errorf("callback %q error = %v", bad, err)
		}
	}
	values := requiredEnv()
	values["FLICKR_OAUTH_CALLBACK_URL"] = " https://xflickr.example/flickr-accounts/callback "
	cfg, err := LoadFromEnv(values)
	if err != nil || cfg.Flickr.CallbackURL != "https://xflickr.example/flickr-accounts/callback" {
		t.Fatalf("callback = %q %v", cfg.Flickr.CallbackURL, err)
	}
}

func TestLoadFromEnvValidatesTheFlickrUpstreamURL(t *testing.T) {
	for _, bad := range []string{"/services", "ftp://example.com", "http://"} {
		values := requiredEnv()
		values["FLICKR_UPSTREAM_URL"] = bad
		if _, err := LoadFromEnv(values); err == nil || !strings.Contains(err.Error(), "FLICKR_UPSTREAM_URL") {
			t.Errorf("upstream %q error = %v", bad, err)
		}
	}
	values := requiredEnv()
	values["FLICKR_UPSTREAM_URL"] = " http://127.0.0.1:8088/ "
	cfg, err := LoadFromEnv(values)
	if err != nil || cfg.Flickr.UpstreamURL != "http://127.0.0.1:8088" {
		t.Fatalf("upstream = %q %v", cfg.Flickr.UpstreamURL, err)
	}
}
