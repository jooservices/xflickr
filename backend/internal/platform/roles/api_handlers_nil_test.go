package roles

import "testing"

func TestHandlerBuildersRejectMissingDatabase(t *testing.T) {
	builders := map[string]func() error{
		"feature flag": func() error { _, err := buildFeatureFlagHandler(nil); return err },
		"home":         func() error { _, err := buildHomeHandler(nil); return err },
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			if err := build(); err == nil {
				t.Fatalf("%s builder accepted a nil database", name)
			}
		})
	}
}
