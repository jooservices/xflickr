package app

import "github.com/jooservices/xflickr/internal/sharing/domain"

// SQLPredicate is a parameterized SQL condition for catalog photo queries.
// The table name is intentionally fixed to the catalog's photos table so the
// same predicate can be appended to API, search-fallback, and download reads.
type SQLPredicate struct {
	Expression string
	Args       []any
}

// SearchFilter is the Elasticsearch-compatible predicate over visibility
// fields computed by the indexer.
type SearchFilter map[string]any

// Filter bundles the SQL and search forms of the visibility rule for a user.
type FilterSet struct {
	SQL    SQLPredicate
	Search SearchFilter
}

// Filter returns equivalent SQL and search filters for user.
func Filter(user domain.User) FilterSet {
	return FilterSet{
		SQL: PhotoVisibilityPredicate(user.ID),
		Search: SearchFilter{
			"bool": map[string]any{
				"should": []any{
					map[string]any{"term": map[string]any{"visible_to": user.ID}},
					map[string]any{"bool": map[string]any{"filter": []any{
						map[string]any{"term": map[string]any{"visibility": "public"}},
						map[string]any{"term": map[string]any{"publicly_shared": true}},
					}}},
				},
				"minimum_should_match": 1,
			},
		},
	}
}

// PhotoVisibilityPredicate returns the parameterized SQL filter for a user.
func PhotoVisibilityPredicate(userID int64) SQLPredicate {
	visibleObservation := VisiblePhotoObservation(userID)
	return SQLPredicate{
		Expression: `(EXISTS (SELECT 1 FROM observations o WHERE o.record_kind = 'photo' AND o.record_id = photos.id::text AND ` + visibleObservation.Expression + `))`,
		Args:       visibleObservation.Args,
	}
}

// VisiblePhotoObservation returns the visibility rule for an observation row
// aliased as o joined to photos. It is shared by catalog queries that count or
// select visible photo observations.
func VisiblePhotoObservation(userID int64) SQLPredicate {
	return SQLPredicate{
		Expression: `(o.user_id = $1 OR (photos.visibility = 'public' AND EXISTS (SELECT 1 FROM sharing_settings ss WHERE ss.user_id = o.user_id AND ss.enabled)))`,
		Args:       []any{userID},
	}
}
