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
	return FilterByUserID(user.ID)
}

// FilterByUserID returns equivalent SQL and search filters for userID.
func FilterByUserID(userID int64) FilterSet {
	return FilterSet{
		SQL: SQLPredicate{
			Expression: `(
    EXISTS (
        SELECT 1 FROM observations own_observation
        WHERE own_observation.user_id = $1
          AND own_observation.record_kind = 'photo'
          AND own_observation.record_id = photos.id::text
    )
    OR (
        photos.visibility = 'public'
        AND EXISTS (
            SELECT 1
            FROM observations shared_observation
            JOIN sharing_settings setting
              ON setting.user_id = shared_observation.user_id AND setting.enabled
            WHERE shared_observation.record_kind = 'photo'
              AND shared_observation.record_id = photos.id::text
        )
    )
)`,
			Args: []any{userID},
		},
		Search: SearchFilter{
			"bool": map[string]any{
				"should": []any{
					map[string]any{"term": map[string]any{"visible_to": userID}},
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
