package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnvRejectsInvalidArgon2idParameters(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "memory below eight KiB per lane", key: "ARGON2ID_MEMORY", value: "15"},
		{name: "zero iterations", key: "ARGON2ID_ITERATIONS", value: "0"},
		{name: "zero parallelism", key: "ARGON2ID_PARALLELISM", value: "0"},
		{name: "salt shorter than eight bytes", key: "ARGON2ID_SALT_LENGTH", value: "7"},
		{name: "key shorter than sixteen bytes", key: "ARGON2ID_KEY_LENGTH", value: "15"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := requiredEnv()
			values[test.key] = test.value
			_, err := LoadFromEnv(values)
			if err == nil || !strings.Contains(err.Error(), "ARGON2ID parameters are invalid") {
				t.Fatalf("LoadFromEnv() error = %v, want ARGON2ID parameters are invalid", err)
			}
		})
	}
}

func TestLoadFromEnvRejectsRedisPortOutOfRange(t *testing.T) {
	for _, value := range []string{"0", "65536"} {
		values := requiredEnv()
		values["REDIS_PORT"] = value
		_, err := LoadFromEnv(values)
		if err == nil || !strings.Contains(err.Error(), "REDIS_PORT must be between 1 and 65535") {
			t.Fatalf("REDIS_PORT=%s error = %v", value, err)
		}
	}
}

func TestDBConfigValidateChecksEachSetting(t *testing.T) {
	valid := DBConfig{
		Host:              "127.0.0.1",
		Port:              5432,
		Name:              "xflickr_test",
		User:              "postgres",
		MaxConns:          20,
		MinConns:          2,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() of valid settings error = %v", err)
	}

	tests := []struct {
		name   string
		change func(*DBConfig)
		want   string
	}{
		{name: "host blank", change: func(c *DBConfig) { c.Host = " " }, want: "DB_HOST is required"},
		{name: "name blank", change: func(c *DBConfig) { c.Name = "" }, want: "DB_NAME is required"},
		{name: "user blank", change: func(c *DBConfig) { c.User = " " }, want: "DB_USER is required"},
		{name: "port zero", change: func(c *DBConfig) { c.Port = 0 }, want: "DB_PORT must be between 1 and 65535"},
		{name: "max conns zero", change: func(c *DBConfig) { c.MaxConns = 0 }, want: "DB_MAX_CONNS must be at least 1"},
		{name: "min conns negative", change: func(c *DBConfig) { c.MinConns = -1 }, want: "DB_MIN_CONNS must be between 0 and DB_MAX_CONNS"},
		{name: "min conns above max", change: func(c *DBConfig) { c.MinConns = 21 }, want: "DB_MIN_CONNS must be between 0 and DB_MAX_CONNS"},
		{name: "lifetime zero", change: func(c *DBConfig) { c.MaxConnLifetime = 0 }, want: "DB_MAX_CONN_LIFETIME must be greater than 0"},
		{name: "idle time zero", change: func(c *DBConfig) { c.MaxConnIdleTime = 0 }, want: "DB_MAX_CONN_IDLE_TIME must be greater than 0"},
		{name: "health check period zero", change: func(c *DBConfig) { c.HealthCheckPeriod = 0 }, want: "DB_HEALTH_CHECK_PERIOD must be greater than 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.change(&cfg)
			err := cfg.Validate()
			if err == nil || err.Error() != test.want {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadFromEnvReportsTheFirstFailingSectionInOrder(t *testing.T) {
	tests := []struct {
		name   string
		first  [2]string
		second [2]string
		want   string
	}{
		{name: "call log before identity", first: [2]string{"API_CALL_LOG_RETENTION_DAYS", "0"}, second: [2]string{"ARGON2ID_MEMORY", "15"}, want: "API_CALL_LOG_RETENTION_DAYS"},
		{name: "identity before callback", first: [2]string{"ARGON2ID_ITERATIONS", "0"}, second: [2]string{"FLICKR_OAUTH_CALLBACK_URL", "ftp://example.test/cb"}, want: "ARGON2ID parameters are invalid"},
		{name: "callback before upstream", first: [2]string{"FLICKR_OAUTH_CALLBACK_URL", "ftp://example.test/cb"}, second: [2]string{"FLICKR_UPSTREAM_URL", "ftp://example.test"}, want: "FLICKR_OAUTH_CALLBACK_URL"},
		{name: "upstream before http port", first: [2]string{"FLICKR_UPSTREAM_URL", "ftp://example.test"}, second: [2]string{"HTTP_PORT", "0"}, want: "FLICKR_UPSTREAM_URL"},
		{name: "http port before internal address", first: [2]string{"HTTP_PORT", "0"}, second: [2]string{"INTERNAL_HTTP_ADDR", "0.0.0.0:8081"}, want: "HTTP_PORT"},
		{name: "internal address before database", first: [2]string{"INTERNAL_HTTP_ADDR", "0.0.0.0:8081"}, second: [2]string{"DB_PORT", "0"}, want: "INTERNAL_HTTP_ADDR"},
		{name: "database before mail", first: [2]string{"DB_PORT", "0"}, second: [2]string{"MAIL_SMTP_ADDR", "127.0.0.1:0"}, want: "DB_PORT"},
		{name: "mail before redis", first: [2]string{"MAIL_SMTP_ADDR", "127.0.0.1:0"}, second: [2]string{"REDIS_PORT", "0"}, want: "SMTP port"},
		{name: "redis before log level", first: [2]string{"REDIS_PORT", "0"}, second: [2]string{"LOG_LEVEL", "trace"}, want: "REDIS_PORT"},
		{name: "log level before blob", first: [2]string{"LOG_LEVEL", "trace"}, second: [2]string{"BLOB_DRIVER", "garage"}, want: "LOG_LEVEL"},
		{name: "blob cap before blob driver", first: [2]string{"BLOB_MAX_BYTES", "-1"}, second: [2]string{"BLOB_DRIVER", "garage"}, want: "BLOB_MAX_BYTES"},
		{name: "link ttl before blob driver", first: [2]string{"DOWNLOAD_LINK_TTL", "0s"}, second: [2]string{"BLOB_DRIVER", "garage"}, want: "DOWNLOAD_LINK_TTL"},
		{name: "link ttl limit before s3 credentials", first: [2]string{"DOWNLOAD_LINK_TTL", "48h"}, second: [2]string{"BLOB_DRIVER", "s3"}, want: "DOWNLOAD_LINK_TTL must not exceed BLOB_MAX_TTL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := requiredEnv()
			values[test.first[0]] = test.first[1]
			values[test.second[0]] = test.second[1]
			_, err := LoadFromEnv(values)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadFromEnv() error = %v, want %q", err, test.want)
			}
		})
	}
}
