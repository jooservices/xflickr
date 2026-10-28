package config

import (
	"strings"
	"testing"
)

func TestInternalHTTPAddrRequiresSpecificIPAndPort(t *testing.T) {
	for _, address := range []string{":8081", "0.0.0.0:8081", "[::]:8081", "localhost:8081", "127.0.0.1:0", "127.0.0.1:nope"} {
		t.Run(address, func(t *testing.T) {
			values := requiredEnv()
			values["INTERNAL_HTTP_ADDR"] = address
			_, err := LoadFromEnv(values)
			if err == nil || !strings.Contains(err.Error(), "INTERNAL_HTTP_ADDR") {
				t.Fatalf("LoadFromEnv() error = %v", err)
			}
		})
	}
	for _, address := range []string{"127.0.0.1:8081", "192.168.2.10:8081", "[2001:db8::10]:8081"} {
		t.Run(address, func(t *testing.T) {
			values := requiredEnv()
			values["INTERNAL_HTTP_ADDR"] = address
			cfg, err := LoadFromEnv(values)
			if err != nil || cfg.InternalHTTPAddr != address {
				t.Fatalf("LoadFromEnv() = %q, %v", cfg.InternalHTTPAddr, err)
			}
		})
	}
}
