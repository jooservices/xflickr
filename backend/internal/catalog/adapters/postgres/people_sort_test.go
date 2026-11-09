package postgres

import (
	"testing"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

func TestPeopleBatchSortedByNSID(t *testing.T) {
	people := []domain.Person{
		{NSID: "c@N00", Username: "c"},
		{NSID: "a@N00", Username: "a"},
		{NSID: "b@N00", Username: "b"},
	}
	got := sortPeopleByNSID(people)
	if got[0].NSID != "a@N00" || got[1].NSID != "b@N00" || got[2].NSID != "c@N00" {
		t.Fatalf("sorted = %v", []string{got[0].NSID, got[1].NSID, got[2].NSID})
	}
	if people[0].NSID != "c@N00" {
		t.Fatal("sortPeopleByNSID mutated the caller's slice")
	}
}
