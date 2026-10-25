package domain

import "testing"

func TestTransitionTable(t *testing.T) {
	tests := []struct {
		from, to State
		wantErr  bool
	}{
		{StatePending, StateReady, false},
		{StateReady, StateRunning, false},
		{StateRunning, StateWaiting, false},
		{StateRunning, StateDone, false},
		{StateRunning, StateFailed, false},
		{StatePending, StateRunning, true},
		{StateReady, StateDone, true},
		{StateRunning, StatePending, true},
		{StateWaiting, StateReady, true},
		{StateDone, StatePending, true},
		{StateFailed, StateRunning, true},
		{State("unknown"), StateReady, true},
		{StatePending, State("unknown"), true},
	}
	for _, test := range tests {
		t.Run(string(test.from)+"_"+string(test.to), func(t *testing.T) {
			err := Transition(test.from, test.to)
			if (err != nil) != test.wantErr {
				t.Fatalf("Transition(%q, %q) error = %v", test.from, test.to, err)
			}
		})
	}
}
