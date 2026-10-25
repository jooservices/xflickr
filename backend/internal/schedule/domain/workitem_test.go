package domain

import (
	"encoding/json"
	"testing"
)

func TestWorkItemValidate(t *testing.T) {
	tests := []struct {
		name string
		item WorkItem
		want bool
	}{
		{name: "valid", item: WorkItem{Kind: "person", SubjectKey: "alice"}},
		{name: "valid cursor", item: WorkItem{Kind: "person", SubjectKey: "alice", Cursor: json.RawMessage(`{"page":2}`)}},
		{name: "missing kind", item: WorkItem{SubjectKey: "alice"}, want: true},
		{name: "blank kind", item: WorkItem{Kind: " ", SubjectKey: "alice"}, want: true},
		{name: "missing subject", item: WorkItem{Kind: "person"}, want: true},
		{name: "blank subject", item: WorkItem{Kind: "person", SubjectKey: " "}, want: true},
		{name: "bad state", item: WorkItem{Kind: "person", SubjectKey: "alice", State: "queued"}, want: true},
		{name: "negative attempts", item: WorkItem{Kind: "person", SubjectKey: "alice", Attempts: -1}, want: true},
		{name: "invalid cursor", item: WorkItem{Kind: "person", SubjectKey: "alice", Cursor: json.RawMessage("{")}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.item.Validate()
			if (err != nil) != test.want {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.want)
			}
		})
	}
}
