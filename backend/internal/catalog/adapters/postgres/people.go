package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// UpsertPeopleBatchMax is the largest people batch sent to PostgreSQL.
const UpsertPeopleBatchMax = 500

// UpsertPeople keeps the public repository operation compatible by ensuring
// people exist before refreshing rows whose merged values have changed.
func (r *Repository) UpsertPeople(ctx context.Context, people []domain.Person) error {
	if err := r.EnsurePeople(ctx, people); err != nil {
		return err
	}
	return r.RefreshChangedPeople(ctx, people)
}

// EnsurePeople inserts missing people without locking existing conflicts.
func (r *Repository) EnsurePeople(ctx context.Context, people []domain.Person) error {
	ordered, err := preparePeople(people)
	if err != nil || len(ordered) == 0 {
		return err
	}
	return forEachChunk(ordered, UpsertPeopleBatchMax, func(chunk []domain.Person) error {
		if err := r.queries.EnsurePeople(ctx, newPeopleArrays(chunk).ensureParams()); err != nil {
			return fmt.Errorf("catalog: ensure people: %w", err)
		}
		return nil
	})
}

// RefreshChangedPeople locks and updates only rows whose merged values differ.
func (r *Repository) RefreshChangedPeople(ctx context.Context, people []domain.Person) error {
	ordered, err := preparePeople(people)
	if err != nil || len(ordered) == 0 {
		return err
	}
	return forEachChunk(ordered, UpsertPeopleBatchMax, func(chunk []domain.Person) error {
		if err := r.queries.RefreshChangedPeople(ctx, newPeopleArrays(chunk).refreshParams()); err != nil {
			return fmt.Errorf("catalog: refresh changed people: %w", err)
		}
		return nil
	})
}

// RefreshPeople exposes the crawl writer operation over this catalog adapter.
func (r *Repository) RefreshPeople(ctx context.Context, people []domain.Person) error {
	return r.RefreshChangedPeople(ctx, people)
}

func preparePeople(people []domain.Person) ([]domain.Person, error) {
	ordered := sortPeopleByNSID(people)
	unique := make([]domain.Person, 0, len(ordered))
	for _, person := range ordered {
		if person.NSID == "" || person.Username == "" || person.PhotosCount < 0 {
			return nil, errors.New("catalog: invalid person")
		}
		if len(unique) > 0 && unique[len(unique)-1].NSID == person.NSID {
			unique[len(unique)-1] = person
			continue
		}
		unique = append(unique, person)
	}
	return unique, nil
}

type peopleArrays struct {
	nsids        []string
	usernames    []string
	realnames    []string
	profiles     [][]byte
	icons        []string
	photosCounts []int64
	lastSeenAts  []pgtype.Timestamptz
	facetStates  [][]byte
}

func newPeopleArrays(people []domain.Person) peopleArrays {
	values := peopleArrays{
		nsids: make([]string, 0, len(people)), usernames: make([]string, 0, len(people)),
		realnames: make([]string, 0, len(people)), profiles: make([][]byte, 0, len(people)),
		icons: make([]string, 0, len(people)), photosCounts: make([]int64, 0, len(people)),
		lastSeenAts: make([]pgtype.Timestamptz, 0, len(people)), facetStates: make([][]byte, 0, len(people)),
	}
	for _, person := range people {
		values.nsids = append(values.nsids, person.NSID)
		values.usernames = append(values.usernames, person.Username)
		values.realnames = append(values.realnames, person.Realname)
		values.profiles = append(values.profiles, jsonOrObject(person.Profile))
		values.icons = append(values.icons, person.Icon)
		values.photosCounts = append(values.photosCounts, person.PhotosCount)
		values.lastSeenAts = append(values.lastSeenAts, pgTime(person.LastSeenAt))
		values.facetStates = append(values.facetStates, jsonOrObject(person.FacetState))
	}
	return values
}

func (p peopleArrays) ensureParams() gen.EnsurePeopleParams {
	return gen.EnsurePeopleParams{
		Nsids: p.nsids, Usernames: p.usernames, Realnames: p.realnames, Profiles: p.profiles,
		Icons: p.icons, PhotosCounts: p.photosCounts, LastSeenAts: p.lastSeenAts, FacetStates: p.facetStates,
	}
}

func (p peopleArrays) refreshParams() gen.RefreshChangedPeopleParams {
	return gen.RefreshChangedPeopleParams{
		Nsids: p.nsids, Usernames: p.usernames, Realnames: p.realnames, Profiles: p.profiles,
		Icons: p.icons, PhotosCounts: p.photosCounts, LastSeenAts: p.lastSeenAts, FacetStates: p.facetStates,
	}
}

func sortPeopleByNSID(people []domain.Person) []domain.Person {
	ordered := append([]domain.Person(nil), people...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].NSID < ordered[j].NSID })
	return ordered
}
