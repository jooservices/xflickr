package queue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJobValidation(t *testing.T) {
	tests := []struct {
		name string
		job  Job
		want string
	}{
		{"missing kind", Job{Args: json.RawMessage(`{}`)}, "kind"},
		{"invalid args", Job{Kind: "crawl", Args: json.RawMessage(`{`)}, "valid JSON"},
		{"missing args", Job{Kind: "crawl"}, "valid JSON"},
		{"valid", Job{Kind: "crawl", Args: json.RawMessage(`{"id":1}`)}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.job.validate()
			if test.want == "" && err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOptionsValidation(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"missing queue", Options{}, "queue name"},
		{"negative attempts", Options{Queue: "media", MaxAttempts: -1}, "max attempts"},
		{"negative timeout", Options{Queue: "media", LeaseTimeout: -time.Second}, "lease timeout"},
		{"negative backoff", Options{Queue: "media", Backoff: -time.Second}, "backoff"},
		{"valid", Options{Queue: "crawl@node-1", MaxAttempts: 3, LeaseTimeout: time.Minute, Backoff: time.Second}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.opts.validate()
			if test.want == "" && err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("validate() error = %v, want %q", err, test.want)
			}
		})
	}
}
