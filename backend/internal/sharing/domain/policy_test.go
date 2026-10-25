package domain

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestCanSeeRules(t *testing.T) {
	tests := []struct {
		name   string
		user   User
		record Record
		want   bool
	}{
		{"own public observation", User{1}, Record{"public", []Observer{{UserID: 1}}}, true},
		{"own private observation", User{1}, Record{"private", []Observer{{UserID: 1}}}, true},
		{"own friends observation", User{1}, Record{"friends", []Observer{{UserID: 1}}}, true},
		{"own family observation", User{1}, Record{"family", []Observer{{UserID: 1}}}, true},
		{"public shared by another observer", User{1}, Record{"public", []Observer{{UserID: 2, SharingEnabled: true}}}, true},
		{"public not shared by another observer", User{1}, Record{"public", []Observer{{UserID: 2}}}, false},
		{"friends never shared", User{1}, Record{"friends", []Observer{{UserID: 2, SharingEnabled: true}}}, false},
		{"family never shared", User{1}, Record{"family", []Observer{{UserID: 2, SharingEnabled: true}}}, false},
		{"private never shared", User{1}, Record{"private", []Observer{{UserID: 2, SharingEnabled: true}}}, false},
		{"no observers", User{1}, Record{"public", nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanSee(tt.user, tt.record); got != tt.want {
				t.Fatalf("CanSee() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVisibleToProjection(t *testing.T) {
	record := Record{"public", []Observer{
		{UserID: 4}, {UserID: 4, SharingEnabled: true}, {UserID: 9},
	}}
	want := IndexVisibility{VisibleTo: []int64{4, 9}, PubliclyShared: true}
	if got := VisibleTo(record); !reflect.DeepEqual(got, want) {
		t.Fatalf("VisibleTo() = %#v, want %#v", got, want)
	}
	private := VisibleTo(Record{"private", []Observer{{UserID: 4, SharingEnabled: true}}})
	if private.PubliclyShared || !reflect.DeepEqual(private.VisibleTo, []int64{4}) {
		t.Fatalf("private projection = %#v", private)
	}
}

func TestCanSeeNonPublicWithoutObservationProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(104))
	visibilities := []string{"friends", "family", "private"}
	for i := 0; i < 10000; i++ {
		userID := int64(rng.Intn(1000) + 1)
		observers := make([]Observer, rng.Intn(12))
		for j := range observers {
			observers[j] = Observer{UserID: int64(rng.Intn(1000) + 1001), SharingEnabled: rng.Intn(2) == 1}
		}
		record := Record{Visibility: visibilities[rng.Intn(len(visibilities))], Observers: observers}
		if CanSee(User{ID: userID}, record) {
			t.Fatalf("non-public record visible without observation: user=%d record=%+v", userID, record)
		}
	}
}
