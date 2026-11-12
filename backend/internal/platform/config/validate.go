package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/mail"
)

// Validate checks PostgreSQL connection and pool settings before use.
func (cfg DBConfig) Validate() error {
	if err := cfg.validateTarget(); err != nil {
		return err
	}
	return cfg.validatePool()
}

func (cfg DBConfig) validateTarget() error {
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
	return nil
}

func (cfg DBConfig) validatePool() error {
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

// validate checks each configuration section in a fixed order. The first
// failing section decides the returned error.
func (cfg *Config) validate() error {
	for _, check := range []func() error{
		cfg.validateCallLog,
		cfg.validateIdentity,
		cfg.validateFlickr,
		cfg.validateHTTPPort,
		cfg.validateInternalHTTPAddr,
		cfg.DB.Validate,
		cfg.validateMail,
		cfg.validateRedis,
		cfg.validateLog,
		cfg.validateBlob,
		cfg.validateS3,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (cfg *Config) validateCallLog() error {
	if cfg.CallLog.RetentionDays < 1 || cfg.CallLog.RetentionDays > 3650 {
		return fmt.Errorf("API_CALL_LOG_RETENTION_DAYS must be between 1 and 3650")
	}
	return nil
}

func (cfg *Config) validateIdentity() error {
	identity := cfg.Identity
	if identity.Argon2Memory < 8*uint32(identity.Argon2Parallelism) || identity.Argon2Iterations == 0 || identity.Argon2Parallelism == 0 || identity.Argon2SaltLength < 8 || identity.Argon2KeyLength < 16 {
		return fmt.Errorf("ARGON2ID parameters are invalid")
	}
	return nil
}

func (cfg *Config) validateFlickr() error {
	callback, ok := parseHTTPURL(strings.TrimSpace(cfg.Flickr.CallbackURL))
	if !ok {
		return fmt.Errorf("FLICKR_OAUTH_CALLBACK_URL must be an absolute http or https URL")
	}
	cfg.Flickr.CallbackURL = callback.String()

	if upstream := strings.TrimSpace(cfg.Flickr.UpstreamURL); upstream != "" {
		parsed, ok := parseHTTPURL(upstream)
		if !ok {
			return fmt.Errorf("FLICKR_UPSTREAM_URL must be an absolute http or https URL")
		}
		cfg.Flickr.UpstreamURL = strings.TrimRight(parsed.String(), "/")
	}
	return nil
}

// parseHTTPURL parses raw and reports whether it is an absolute http or https
// URL with a host.
func parseHTTPURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, false
	}
	return parsed, true
}

func (cfg *Config) validateHTTPPort() error {
	if cfg.HTTPPort < 1 || cfg.HTTPPort > 65535 {
		return fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}
	return nil
}

func (cfg *Config) validateInternalHTTPAddr() error {
	if cfg.InternalHTTPAddr == "" {
		return nil
	}
	host, portText, err := net.SplitHostPort(cfg.InternalHTTPAddr)
	port, portErr := strconv.Atoi(portText)
	address := net.ParseIP(host)
	if err != nil || portErr != nil || port < 1 || port > 65535 || address == nil {
		return fmt.Errorf("INTERNAL_HTTP_ADDR must be a specific IP address and a port between 1 and 65535")
	}
	if address.IsUnspecified() {
		return fmt.Errorf("INTERNAL_HTTP_ADDR must not bind to a wildcard address")
	}
	return nil
}

func (cfg *Config) validateMail() error {
	settings, err := (mail.SMTPSettings{
		Addr:     cfg.Mail.SMTPAddr,
		From:     cfg.Mail.From,
		Username: cfg.Mail.Username,
		Password: cfg.Mail.Password,
	}).Validate()
	if err != nil {
		return err
	}
	cfg.Mail = MailConfig{
		SMTPAddr: settings.Addr,
		From:     settings.From,
		Username: settings.Username,
		Password: settings.Password,
	}
	return nil
}

func (cfg *Config) validateRedis() error {
	if cfg.Redis.Port < 1 || cfg.Redis.Port > 65535 {
		return fmt.Errorf("REDIS_PORT must be between 1 and 65535")
	}
	return nil
}

func (cfg *Config) validateLog() error {
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, or error")
	}
}

// validateBlob checks the blob store limits, the download link lifetime and
// the driver name. The S3 credentials are checked by validateS3.
func (cfg *Config) validateBlob() error {
	cfg.Blob.Driver = strings.ToLower(strings.TrimSpace(cfg.Blob.Driver))
	if cfg.Blob.MaxBytes < 0 {
		return fmt.Errorf("BLOB_MAX_BYTES must be zero or greater")
	}
	if cfg.Blob.MaxTTL <= 0 {
		return fmt.Errorf("BLOB_MAX_TTL must be greater than zero")
	}
	if cfg.DownloadLinkTTL <= 0 {
		return fmt.Errorf("DOWNLOAD_LINK_TTL must be greater than zero")
	}
	if cfg.DownloadLinkTTL > cfg.Blob.MaxTTL {
		return fmt.Errorf("DOWNLOAD_LINK_TTL must not exceed BLOB_MAX_TTL")
	}
	switch cfg.Blob.Driver {
	case "local", "s3":
		return nil
	default:
		return fmt.Errorf("BLOB_DRIVER must be local or s3")
	}
}

func (cfg *Config) validateS3() error {
	if cfg.Blob.Driver != "s3" {
		return nil
	}
	var missing []string
	for _, setting := range []struct {
		name  string
		value string
	}{
		{name: "S3_ENDPOINT", value: cfg.Blob.S3Endpoint},
		{name: "S3_BUCKET", value: cfg.Blob.S3Bucket},
		{name: "S3_ACCESS_KEY", value: cfg.Blob.S3AccessKey},
		{name: "S3_SECRET_KEY", value: cfg.Blob.S3SecretKey},
	} {
		if strings.TrimSpace(setting.value) == "" {
			missing = append(missing, setting.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("BLOB_DRIVER=s3 requires: %s", strings.Join(missing, ", "))
	}
	return nil
}
