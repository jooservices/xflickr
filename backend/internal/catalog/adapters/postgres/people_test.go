package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

func TestPeopleWritesChunkAtFiveHundred(t *testing.T) {
	db := &repositoryTestDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsurePeople(context.Background(), nil); err != nil {
		t.Fatalf("ensure empty batch: %v", err)
	}
	if err := repository.RefreshChangedPeople(context.Background(), nil); err != nil {
		t.Fatalf("refresh empty batch: %v", err)
	}
	if db.execCalls != 0 {
		t.Fatalf("empty batches executed %d statements, want 0", db.execCalls)
	}
	people := make([]domain.Person, UpsertPeopleBatchMax+1)
	for i := range people {
		nsid := fmt.Sprintf("person-%03d", i)
		people[i] = domain.Person{NSID: nsid, Username: nsid}
	}
	if err := repository.EnsurePeople(context.Background(), people); err != nil {
		t.Fatalf("ensure 501 people: %v", err)
	}
	if db.execCalls != 2 {
		t.Fatalf("ensure statements = %d, want 2", db.execCalls)
	}
	db.execCalls = 0
	if err := repository.RefreshChangedPeople(context.Background(), people); err != nil {
		t.Fatalf("refresh 501 people: %v", err)
	}
	if db.execCalls != 2 {
		t.Fatalf("refresh statements = %d, want 2", db.execCalls)
	}
	db.execCalls = 0
	if err := repository.UpsertPeople(context.Background(), people); err != nil {
		t.Fatalf("upsert 501 people: %v", err)
	}
	if db.execCalls != 4 {
		t.Fatalf("upsert statements = %d, want 4", db.execCalls)
	}
}

func TestPeopleWritesValidateBeforeExecuting(t *testing.T) {
	db := &repositoryTestDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	for name, write := range map[string]func(context.Context, []domain.Person) error{
		"ensure":  repository.EnsurePeople,
		"refresh": repository.RefreshChangedPeople,
		"upsert":  repository.UpsertPeople,
	} {
		t.Run(name, func(t *testing.T) {
			if err := write(context.Background(), []domain.Person{{NSID: "person"}}); err == nil {
				t.Fatal("person without a username succeeded")
			}
			if db.execCalls != 0 {
				t.Fatalf("invalid input executed %d statements", db.execCalls)
			}
		})
	}
	if _, err := preparePeople([]domain.Person{{NSID: "person", Username: "person", PhotosCount: -1}}); err == nil {
		t.Fatal("negative photos count succeeded")
	}
	got, err := preparePeople([]domain.Person{
		{NSID: "b", Username: "first"},
		{NSID: "a", Username: "a"},
		{NSID: "b", Username: "last"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].NSID != "a" || got[1].Username != "last" {
		t.Fatalf("normalized people = %+v", got)
	}
}

func TestPeopleWritesReturnStatementErrors(t *testing.T) {
	wantErr := errors.New("statement failed")
	for name, write := range map[string]func(*Repository) error{
		"ensure": func(repository *Repository) error {
			return repository.EnsurePeople(context.Background(), []domain.Person{{NSID: "person", Username: "person"}})
		},
		"refresh": func(repository *Repository) error {
			return repository.RefreshChangedPeople(context.Background(), []domain.Person{{NSID: "person", Username: "person"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := &repositoryTestDB{execErr: wantErr}
			repository, err := New(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := write(repository); !errors.Is(err, wantErr) {
				t.Fatalf("write error = %v", err)
			}
		})
	}
	db := &repositoryTestDB{execErrors: []error{nil, wantErr}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(context.Background(), []domain.Person{{NSID: "person", Username: "person"}}); !errors.Is(err, wantErr) {
		t.Fatalf("upsert refresh error = %v", err)
	}
}
