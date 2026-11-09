package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/sharing/domain"
)

func TestFilterBuildsEquivalentSQLAndSearchRules(t *testing.T) {
	filter := Filter(domain.User{ID: 42})
	for _, fragment := range []string{
		"FROM observations own_observation",
		"own_observation.user_id = $1",
		"own_observation.record_kind = 'photo'",
		"own_observation.record_id = photos.id::text",
		"FROM observations shared_observation",
		"shared_observation.record_kind = 'photo'",
		"shared_observation.record_id = photos.id::text",
		"setting.user_id = shared_observation.user_id",
		"setting.enabled",
	} {
		if !strings.Contains(filter.SQL.Expression, fragment) {
			t.Errorf("SQL predicate does not contain %q: %s", fragment, filter.SQL.Expression)
		}
	}
	for _, fragment := range []string{"UNION", "OFFSET 0", "CROSS JOIN LATERAL"} {
		if strings.Contains(filter.SQL.Expression, fragment) {
			t.Errorf("SQL predicate contains unbounded observation enumeration %q: %s", fragment, filter.SQL.Expression)
		}
	}
	if len(filter.SQL.Args) != 1 || filter.SQL.Args[0] != int64(42) {
		t.Fatalf("SQL args = %#v, want user id 42", filter.SQL.Args)
	}
	encoded, err := json.Marshal(filter.Search)
	if err != nil {
		t.Fatal(err)
	}
	search := string(encoded)
	for _, fragment := range []string{"\"visible_to\":42", "\"visibility\":\"public\"", "\"publicly_shared\":true", "\"minimum_should_match\":1"} {
		if !strings.Contains(search, fragment) {
			t.Errorf("search filter does not contain %q: %s", fragment, search)
		}
	}
}
