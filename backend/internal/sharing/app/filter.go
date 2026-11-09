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
	SQL      SQLPredicate
	PhotoIDs SQLPredicate
	Search   SearchFilter
}

// Filter returns equivalent SQL and search filters for user.
func Filter(user domain.User) FilterSet {
	return FilterByUserID(user.ID)
}

// FilterByUserID returns equivalent SQL and search filters for userID.
func FilterByUserID(userID int64) FilterSet {
	photoIDs := SQLPredicate{
		Expression: `SELECT o.record_id::bigint AS photo_id
FROM observations o
WHERE o.user_id = $1 AND o.record_kind = 'photo'
UNION
SELECT visible_photo.photo_id
FROM sharing_settings ss
CROSS JOIN LATERAL (
    SELECT o.record_id::bigint AS photo_id
    FROM observations o
    WHERE o.user_id = ss.user_id AND o.record_kind = 'photo'
    OFFSET 0
) shared_observation
CROSS JOIN LATERAL (
    SELECT shared_photo.id AS photo_id
    FROM photos shared_photo
    WHERE shared_photo.id = shared_observation.photo_id
      AND shared_photo.visibility = 'public'
    LIMIT 1
) visible_photo
WHERE ss.enabled`,
		Args: []any{userID},
	}
	return FilterSet{
		SQL: SQLPredicate{
			Expression: `photos.id IN (` + photoIDs.Expression + `)`,
			Args:       photoIDs.Args,
		},
		PhotoIDs: photoIDs,
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
