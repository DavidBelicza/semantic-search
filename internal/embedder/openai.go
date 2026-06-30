package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultBaseURL    = "http://127.0.0.1:1234"
	DefaultModel      = "text-embedding-model"
	DefaultDimensions = 768
)

type OpenAIEmbedder struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewOpenAIEmbedder(baseURL string, model string) OpenAIEmbedder {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}

	return OpenAIEmbedder{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		HTTPClient: http.DefaultClient,
	}
}

func (e OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(e.Model) == "" {
		return nil, fmt.Errorf("embedding model is required")
	}

	endpoint, err := embeddingsEndpoint(e.BaseURL)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(openAIEmbeddingRequest{Model: e.Model, Input: texts})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var payload openAIEmbeddingResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if payload.Error != nil && payload.Error.Message != "" {
			return nil, fmt.Errorf("embedding request failed: %s", payload.Error.Message)
		}
		return nil, fmt.Errorf("embedding request failed with status %d", response.StatusCode)
	}

	vectors := make([][]float32, len(texts))
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("embedding response index %d out of range", item.Index)
		}
		vectors[item.Index] = item.Embedding
	}

	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("missing embedding for input %d", i)
		}
	}

	return vectors, nil
}

func embeddingsEndpoint(baseURL string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}

	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("parse embedding base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("embedding base url must include scheme and host")
	}

	if strings.HasSuffix(parsed.Path, "/v1/embeddings") {
		return parsed.String(), nil
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/embeddings"
	return parsed.String(), nil
}
