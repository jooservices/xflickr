package ollama

import (
	"context"
	"errors"
	"fmt"
	"strings"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

const (
	embeddingModel      = "bge-m3"
	embeddingDimensions = 1024
	maxEmbeddingBatch   = 16
)

type embedRequest struct {
	Model    string   `json:"model"`
	Input    []string `json:"input"`
	Truncate bool     `json:"truncate"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed requests one vector per text from Ollama's embedding model.
func (c *Client) Embed(parent context.Context, texts []string) ([][]float32, aiapp.ModelInfo, error) {
	if parent == nil {
		return nil, aiapp.ModelInfo{}, fmt.Errorf("%w: context is required", aiapp.ErrInvalidConfiguration)
	}
	if len(texts) < 1 || len(texts) > maxEmbeddingBatch {
		return nil, aiapp.ModelInfo{}, aiapp.ErrInvalidTexts
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, aiapp.ModelInfo{}, aiapp.ErrInvalidTexts
		}
	}

	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return nil, aiapp.ModelInfo{}, ctx.Err()
	}
	if !c.breaker.Allow() {
		return nil, aiapp.ModelInfo{}, aiapp.ErrCircuitOpen
	}

	vectors, info, err := c.embed(ctx, texts)
	if err != nil {
		if parent.Err() != nil || errors.Is(err, aiapp.ErrInvalidTexts) {
			c.breaker.RecordAbandoned()
		} else {
			c.breaker.RecordFailure()
		}
		return nil, aiapp.ModelInfo{}, err
	}
	c.breaker.RecordSuccess()
	return vectors, info, nil
}

func (c *Client) embed(ctx context.Context, texts []string) ([][]float32, aiapp.ModelInfo, error) {
	var show showResponse
	if err := c.post(ctx, "api/show", showRequest{Model: embeddingModel}, &show); err != nil {
		return nil, aiapp.ModelInfo{}, err
	}
	if strings.TrimSpace(show.ModifiedAt) == "" {
		return nil, aiapp.ModelInfo{}, fmt.Errorf("%w: Ollama /api/show omitted modified_at", aiapp.ErrInvalidResponse)
	}

	var response embedResponse
	request := embedRequest{Model: embeddingModel, Input: texts, Truncate: false}
	if err := c.post(ctx, "api/embed", request, &response); err != nil {
		return nil, aiapp.ModelInfo{}, err
	}
	if len(response.Embeddings) != len(texts) {
		return nil, aiapp.ModelInfo{}, fmt.Errorf("%w: Ollama returned %d vectors for %d texts", aiapp.ErrInvalidResponse, len(response.Embeddings), len(texts))
	}
	for index, vector := range response.Embeddings {
		if len(vector) != embeddingDimensions {
			return nil, aiapp.ModelInfo{}, fmt.Errorf("%w: Ollama vector %d has %d dimensions, want %d", aiapp.ErrInvalidResponse, index, len(vector), embeddingDimensions)
		}
	}

	return response.Embeddings, aiapp.ModelInfo{Model: embeddingModel, Version: show.ModifiedAt}, nil
}

var _ aiapp.Embedder = (*Client)(nil)
