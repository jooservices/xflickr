package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// UpsertPeopleBatchMax is the largest supported batch size.
const UpsertPeopleBatchMax = 500

// UpsertPeople inserts or refreshes people using sqlc's pgx batch.
// Batches are sorted by nsid so concurrent writers lock rows in one order.
func (r *Repository) UpsertPeople(ctx context.Context, people []domain.Person) error {
	if err := validateBatchSize(len(people)); err != nil {
		return err
	}
	if len(people) == 0 {
		return nil
	}
	ordered := sortPeopleByNSID(people)
	params := make([]gen.UpsertPersonParams, 0, len(ordered))
	for _, person := range ordered {
		if person.NSID == "" || person.Username == "" || person.PhotosCount < 0 {
			return errors.New("catalog: invalid person")
		}
		params = append(params, gen.UpsertPersonParams{
			Nsid: person.NSID, Username: person.Username, Realname: person.Realname,
			Profile: jsonOrObject(person.Profile), Icon: person.Icon, PhotosCount: person.PhotosCount,
			LastSeenAt: pgTime(person.LastSeenAt), FacetState: jsonOrObject(person.FacetState),
		})
	}
	results := r.queries.UpsertPerson(ctx, params)
	var batchErr error
	results.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if batchErr != nil {
		return fmt.Errorf("catalog: upsert people: %w", batchErr)
	}
	return nil
}

func sortPeopleByNSID(people []domain.Person) []domain.Person {
	ordered := append([]domain.Person(nil), people...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NSID < ordered[j].NSID })
	return ordered
}
