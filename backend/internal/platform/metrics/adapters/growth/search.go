package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SearchReader reads aggregate index stats from Elasticsearch/OpenSearch.
type SearchReader struct {
	URL         string
	Username    string
	Password    string
	IndexPrefix string
	Client      *http.Client
}

var _ SearchStats = (*SearchReader)(nil)

type searchStatsResponse struct {
	All struct {
		Primaries struct {
			Docs struct {
				Count int64 `json:"count"`
			} `json:"docs"`
			Store struct {
				SizeInBytes int64 `json:"size_in_bytes"`
			} `json:"store"`
		} `json:"primaries"`
	} `json:"_all"`
}

// DocumentCount returns primary document count across matching indices.
func (s *SearchReader) DocumentCount(ctx context.Context) (int64, error) {
	stats, err := s.fetch(ctx)
	if err != nil {
		return 0, err
	}
	return stats.All.Primaries.Docs.Count, nil
}

// StoreBytes returns primary store size across matching indices.
func (s *SearchReader) StoreBytes(ctx context.Context) (int64, error) {
	stats, err := s.fetch(ctx)
	if err != nil {
		return 0, err
	}
	return stats.All.Primaries.Store.SizeInBytes, nil
}

func (s *SearchReader) fetch(ctx context.Context) (searchStatsResponse, error) {
	if strings.TrimSpace(s.URL) == "" {
		return searchStatsResponse{}, fmt.Errorf("metrics: search URL is required")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	target := strings.TrimRight(s.URL, "/") + "/" + s.indexPattern() + "/_stats/docs,store"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return searchStatsResponse{}, fmt.Errorf("metrics: search request: %w", err)
	}
	if s.Username != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return searchStatsResponse{}, fmt.Errorf("metrics: search stats: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return searchStatsResponse{}, fmt.Errorf("metrics: read search stats: %w", err)
	}
	if resp.StatusCode >= 300 {
		return searchStatsResponse{}, fmt.Errorf("metrics: search stats status %d", resp.StatusCode)
	}
	var stats searchStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return searchStatsResponse{}, fmt.Errorf("metrics: decode search stats: %w", err)
	}
	return stats, nil
}

func (s *SearchReader) indexPattern() string {
	prefix := strings.TrimSpace(s.IndexPrefix)
	if prefix == "" {
		return "_all"
	}
	return prefix + "*"
}
