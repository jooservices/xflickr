package fakeflickr

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScenarioConfig controls deterministic failure and delay injection.
type ScenarioConfig struct {
	QuotaPerHour   int      `json:"quota_per_hour"`
	RateLimitAfter int      `json:"rate_limit_after"`
	BanIPs         []string `json:"ban_ips"`
	LatencyMS      int      `json:"latency_ms"`
	RevokedTokens  []string `json:"revoked_tokens"`
}

type keyUsage struct {
	hour  int64
	calls int
}

// Scenarios safely applies runtime-configurable conditions to incoming calls.
type Scenarios struct {
	mu     sync.Mutex
	config ScenarioConfig
	usage  map[string]keyUsage
}

func NewScenarios(config ScenarioConfig) *Scenarios {
	return &Scenarios{config: config, usage: make(map[string]keyUsage)}
}

// Configure atomically replaces active scenario settings and resets counters.
func (s *Scenarios) Configure(config ScenarioConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
	s.usage = make(map[string]keyUsage)
}

func (s *Scenarios) snapshot() ScenarioConfig { s.mu.Lock(); defer s.mu.Unlock(); return s.config }

func (s *Scenarios) before(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	config := s.config
	clientIP := strings.TrimSpace(r.Header.Get("X-Client-IP"))
	for _, banned := range config.BanIPs {
		if clientIP == banned {
			s.mu.Unlock()
			http.Error(w, "client IP banned", http.StatusForbidden)
			return false
		}
	}
	key := r.FormValue("api_key")
	if key != "" && (config.QuotaPerHour > 0 || config.RateLimitAfter > 0) {
		hour := time.Now().Unix() / 3600
		usage := s.usage[key]
		if usage.hour != hour {
			usage = keyUsage{hour: hour}
		}
		usage.calls++
		s.usage[key] = usage
		if (config.QuotaPerHour > 0 && usage.calls > config.QuotaPerHour) || (config.RateLimitAfter > 0 && usage.calls > config.RateLimitAfter) {
			s.mu.Unlock()
			w.Header().Set("Retry-After", "3600")
			http.Error(w, "quota exceeded", http.StatusTooManyRequests)
			return false
		}
	}
	s.mu.Unlock()
	if config.LatencyMS > 0 {
		timer := time.NewTimer(time.Duration(config.LatencyMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return false
		case <-timer.C:
		}
	}
	return true
}

func (s *Scenarios) revoked(token string) bool {
	if token == "" {
		return false
	}
	for _, candidate := range s.snapshot().RevokedTokens {
		if candidate == token {
			return true
		}
	}
	return false
}

// ScenarioConfigFromEnvironment reads optional FAKEFLICKR_* scenario switches.
func ScenarioConfigFromEnvironment() ScenarioConfig {
	c := ScenarioConfig{QuotaPerHour: envInt("FAKEFLICKR_QUOTA_PER_HOUR"), RateLimitAfter: envInt("FAKEFLICKR_RATE_LIMIT_AFTER"), LatencyMS: envInt("FAKEFLICKR_LATENCY_MS")}
	if ip := strings.TrimSpace(getenv("FAKEFLICKR_BAN_IP")); ip != "" {
		c.BanIPs = []string{ip}
	}
	if token := strings.TrimSpace(getenv("FAKEFLICKR_REVOKED_TOKEN")); token != "" {
		c.RevokedTokens = []string{token}
	}
	return c
}

var getenv = func(key string) string { value, _ := os.LookupEnv(key); return strings.TrimSpace(value) }

func envInt(key string) int {
	n, _ := strconv.Atoi(getenv(key))
	if n < 0 {
		return 0
	}
	return n
}
