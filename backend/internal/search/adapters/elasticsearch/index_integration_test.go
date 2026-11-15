package elasticsearch_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/jooservices/xflickr/internal/search/adapters/elasticsearch"
	"github.com/jooservices/xflickr/internal/search/app"
)

func TestCreateIndexIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree Elasticsearch")
	}

	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal("load worktree integration settings")
	}
	searchURL := integrationSetting(values, "SEARCH_URL", "http://127.0.0.1:19200")
	username := integrationSetting(values, "SEARCH_USERNAME", "")
	password := integrationSetting(values, "SEARCH_PASSWORD", "")
	worktreePrefix := values["OPENSEARCH_INDEX_PREFIX"]
	if worktreePrefix == "" {
		t.Fatal("OPENSEARCH_INDEX_PREFIX is not set in .env")
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal("generate isolated index suffix")
	}
	prefix := worktreePrefix + "it_" + hex.EncodeToString(suffix[:]) + "_"
	physical := prefix + "photos-v1"
	alias := prefix + "photos"
	client := &http.Client{Timeout: 10 * time.Second}
	index, err := elasticsearch.NewIndex(searchURL, username, password, prefix, client)
	if err != nil {
		t.Fatalf("construct Elasticsearch index adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := index.Create(ctx); err != nil {
		t.Fatalf("create isolated Elasticsearch index: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		response, err := doSearchRequest(cleanupContext, client, searchURL, username, password, http.MethodDelete, physical, "", nil)
		if err != nil {
			t.Error("delete isolated Elasticsearch index: request failed")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
			t.Errorf("delete isolated Elasticsearch index: HTTP status %d", response.StatusCode)
		}
	})

	documentIDRandom, err := rand.Int(rand.Reader, big.NewInt(1<<63-1))
	if err != nil {
		t.Fatal("generate document ID")
	}
	documentID := strconv.FormatInt(documentIDRandom.Int64()+1, 10)
	first := app.Document{
		ID: documentIDRandom.Int64() + 1, Title: "old integration photo",
		VisibleTo: []string{"123"}, Visibility: "public", PubliclyShared: true,
	}
	second := first
	second.Title = "replacement integration photo"
	second.VisibleTo = []string{"456"}
	second.Visibility = "private"
	second.PubliclyShared = false
	if err := index.Upsert(ctx, first); err != nil {
		t.Fatalf("write first integration document through alias: %v", err)
	}
	writePath := alias + "/_doc/" + documentID
	if err := index.Upsert(ctx, second); err != nil {
		t.Fatalf("replace integration document through alias: %v", err)
	}

	response, err := doSearchRequest(ctx, client, searchURL, username, password, http.MethodGet, writePath, "", nil)
	if err != nil {
		t.Fatal("read integration document through alias: request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read integration document through alias: HTTP status %d", response.StatusCode)
	}
	var document struct {
		Found  bool `json:"found"`
		Source struct {
			Title          string          `json:"title"`
			VisibleTo      []string        `json:"visible_to"`
			Visibility     string          `json:"visibility"`
			PubliclyShared *bool           `json:"publicly_shared"`
			ID             json.RawMessage `json:"id"`
		} `json:"_source"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&document); err != nil {
		t.Fatal("decode integration document")
	}
	if !document.Found || document.Source.Title != second.Title || len(document.Source.VisibleTo) != 1 || document.Source.VisibleTo[0] != "456" || document.Source.Visibility != "private" || document.Source.PubliclyShared == nil || *document.Source.PubliclyShared || len(document.Source.ID) != 0 {
		t.Fatalf("document read through alias = %+v, want the replacement document", document)
	}
}

func integrationSetting(values map[string]string, key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	if value, ok := values[key]; ok {
		return value
	}
	return defaultValue
}

func doSearchRequest(ctx context.Context, client *http.Client, baseURL, username, password, method, path, query string, body []byte) (*http.Response, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, errors.New("invalid Elasticsearch URL")
	}
	target := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/" + path, RawQuery: query}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("build Elasticsearch request")
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if username != "" {
		request.SetBasicAuth(username, password)
	}
	return client.Do(request)
}
