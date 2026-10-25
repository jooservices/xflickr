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
		"o.user_id = $1", "o.record_kind = 'photo'", "photos.visibility = 'public'",
		"sharing_settings", "ss.enabled",
	} {
		if !strings.Contains(filter.SQL.Expression, fragment) {
			t.Errorf("SQL predicate does not contain %q: %s", fragment, filter.SQL.Expression)
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
