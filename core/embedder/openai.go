package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL        = "http://127.0.0.1:1234"
	DefaultMaxRetries     = 3
	DefaultRequestTimeout = 60 * time.Second
	DefaultBackoffBase    = 200 * time.Millisecond
	DefaultBackoffMax     = 5 * time.Second
)

type OpenAIClient struct {
	BaseURL     string
	Model       string
	Dimensions  int
	MaxRetries  int
	BackoffBase time.Duration
	HTTPClient  *http.Client
	// APIKey, when set, is sent as an "Authorization: Bearer <APIKey>" header. Leave it empty
	// for local servers (e.g. LM Studio) that need no authentication.
	APIKey string
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
	Error embeddingError `json:"error,omitempty"`
}

type embeddingError struct {
	Message string
}

func (e *embeddingError) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var message string
	if err := json.Unmarshal(data, &message); err == nil {
		e.Message = message
		return nil
	}

	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	e.Message = payload.Message
	return nil
}

func NewOpenAIClient(baseURL string, model string) OpenAIClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}

	return OpenAIClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		MaxRetries: DefaultMaxRetries,
		HTTPClient: &http.Client{Timeout: DefaultRequestTimeout},
	}
}

func (e OpenAIClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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

	body, err := encodeEmbeddingRequest(openAIEmbeddingRequest{Model: e.Model, Input: texts})
	if err != nil {
		return nil, err
	}

	return e.embedWithRetry(ctx, endpoint, body, len(texts))
}

func (e OpenAIClient) embedWithRetry(ctx context.Context, endpoint string, body []byte, count int) ([][]float32, error) {
	var lastErr error
	for attempt := 0; attempt <= e.MaxRetries; attempt++ {
		vectors, retryable, err := e.embedOnce(ctx, endpoint, body, count)
		if err == nil {
			return vectors, nil
		}

		lastErr = err
		if !retryable || attempt == e.MaxRetries {
			return nil, lastErr
		}
		if waitErr := sleepBackoff(ctx, e.backoffDelay(attempt)); waitErr != nil {
			return nil, waitErr
		}
	}

	return nil, lastErr
}

// embedOnce performs a single embedding request. The boolean reports whether the
// returned error is transient and worth retrying.
func (e OpenAIClient) embedOnce(ctx context.Context, endpoint string, body []byte, count int) ([][]float32, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+e.APIKey)
	}

	response, err := e.client().Do(request)
	if err != nil {
		return nil, true, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, true, err
	}

	if response.StatusCode/100 != 2 {
		return nil, isRetryableStatus(response.StatusCode), responseError(response.StatusCode, data)
	}

	vectors, err := parseEmbeddings(data, count)
	if err != nil {
		return nil, false, err
	}
	if err := e.validateDimensions(vectors); err != nil {
		return nil, false, err
	}

	return vectors, false, nil
}

func (e OpenAIClient) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}

	return &http.Client{Timeout: DefaultRequestTimeout}
}

func (e OpenAIClient) validateDimensions(vectors [][]float32) error {
	if e.Dimensions <= 0 {
		return nil
	}

	for i, vector := range vectors {
		if len(vector) != e.Dimensions {
			return fmt.Errorf("embedding dimension mismatch for input %d: configured %d, got %d", i, e.Dimensions, len(vector))
		}
	}

	return nil
}

func (e OpenAIClient) backoffDelay(attempt int) time.Duration {
	base := e.BackoffBase
	if base <= 0 {
		base = DefaultBackoffBase
	}

	shift := attempt
	if shift > 16 {
		shift = 16
	}

	delay := base * time.Duration(1<<shift)
	if delay > DefaultBackoffMax {
		return DefaultBackoffMax
	}

	return delay
}

func parseEmbeddings(data []byte, count int) ([][]float32, error) {
	var payload openAIEmbeddingResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if payload.Error.Message != "" {
		return nil, fmt.Errorf("embedding request failed: %s", payload.Error.Message)
	}

	vectors := make([][]float32, count)
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= count {
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

func sleepBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableStatus(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}

	return code >= 500 && code <= 599
}

func responseError(code int, body []byte) error {
	message := providerErrorMessage(body)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		return fmt.Errorf("embedding request failed with status %d", code)
	}

	return fmt.Errorf("embedding request failed with status %d: %s", code, truncate(message, 512))
}

func providerErrorMessage(body []byte) string {
	var payload openAIEmbeddingResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	return payload.Error.Message
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}

	return text[:max]
}

func encodeEmbeddingRequest(request openAIEmbeddingRequest) ([]byte, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
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
