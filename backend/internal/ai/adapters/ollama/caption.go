package ollama

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

const captionPrompt = `Describe the visible contents of this image concisely. Return only strict JSON with exactly two fields: "text", a concise natural-language description, and "objects", an array of visible object names. Use an empty array when no objects can be identified. Do not include markdown or any text outside the JSON object.`

var captionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"text":    map[string]string{"type": "string"},
		"objects": map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
	},
	"required":             []string{"text", "objects"},
	"additionalProperties": false,
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Format   map[string]any `json:"format"`
	Options  map[string]any `json:"options"`
}

type chatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type showRequest struct {
	Model string `json:"model"`
}

type showResponse struct {
	ModifiedAt string `json:"modified_at"`
}

func encodeImage(image []byte) string {
	return base64.StdEncoding.EncodeToString(image)
}

func parseCaption(content, model, version string) (aiapp.Caption, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var caption aiapp.Caption
	if err := decoder.Decode(&caption); err != nil {
		return aiapp.Caption{}, fmt.Errorf("%w: decode caption JSON: %w", aiapp.ErrInvalidResponse, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return aiapp.Caption{}, fmt.Errorf("%w: caption JSON has trailing data", aiapp.ErrInvalidResponse)
		}
		return aiapp.Caption{}, fmt.Errorf("%w: read caption JSON trailer: %w", aiapp.ErrInvalidResponse, err)
	}
	if caption.Objects == nil {
		return aiapp.Caption{}, fmt.Errorf("%w: objects must be an array", aiapp.ErrInvalidResponse)
	}
	caption.Text = strings.TrimSpace(caption.Text)
	for index := range caption.Objects {
		caption.Objects[index] = strings.TrimSpace(caption.Objects[index])
		if caption.Objects[index] == "" {
			return aiapp.Caption{}, fmt.Errorf("%w: objects must not contain empty names", aiapp.ErrInvalidResponse)
		}
	}
	caption.Model = model
	caption.Version = version
	if err := caption.Validate(); err != nil {
		return aiapp.Caption{}, fmt.Errorf("%w: %w", aiapp.ErrInvalidResponse, err)
	}
	return caption, nil
}
