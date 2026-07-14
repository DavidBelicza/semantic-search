package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIClientPostsWithoutBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path mismatch: want /v1/embeddings, got %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("expected no authorization header, got %q", r.Header.Get("Authorization"))
		}

		var request openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "test-model" {
			t.Fatalf("model mismatch: want test-model, got %q", request.Model)
		}
		if len(request.Input) != 2 || request.Input[0] != "first" || request.Input[1] != "second" {
			t.Fatalf("input mismatch: %#v", request.Input)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"index": 0, "embedding": [0.1, 0.2]},
				{"index": 1, "embedding": [0.3, 0.4]}
			]
		}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	vectors, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(vectors) != 2 {
		t.Fatalf("vector count mismatch: want 2, got %d", len(vectors))
	}
	if vectors[0][0] != 0.1 || vectors[1][1] != 0.4 {
		t.Fatalf("vectors mismatch: %#v", vectors)
	}
}

func TestOpenAIClientSendsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization header mismatch: want %q, got %q", "Bearer test-key", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"index": 0, "embedding": [0.1, 0.2]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	embedder.APIKey = "test-key"
	if _, err := embedder.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("embed: %v", err)
	}
}

func TestOpenAIClientPostsArbitraryMarkdownContentAsText(t *testing.T) {
	input := strings.Join([]string{
		"```json",
		`{"title":"Art of Seduction","items":[1,true,null],"quote":"\"hello\""}`,
		"```",
		"```sql",
		"SELECT * FROM notes WHERE body LIKE '%{json}%';",
		"```",
		"raw path C:\\tmp\\notes and control chars \x00 \t \n",
		"<not-html>&still text</not-html>",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.Input) != 1 {
			t.Fatalf("input count mismatch: want 1, got %d", len(request.Input))
		}
		if request.Input[0] != input {
			t.Fatalf("input was not preserved as text:\nwant: %q\n got: %q", input, request.Input[0])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	if _, err := embedder.Embed(context.Background(), []string{input}); err != nil {
		t.Fatalf("embed arbitrary markdown content: %v", err)
	}
}

func TestOpenAIClientRejectsMissingEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	if _, err := embedder.Embed(context.Background(), []string{"first", "second"}); err == nil {
		t.Fatal("expected missing embedding error")
	}
}

func TestOpenAIClientReportsStringErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected embedding error")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error does not include provider message: %v", err)
	}
}

func TestOpenAIClientReportsObjectErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"input too large"}}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected embedding error")
	}
	if !strings.Contains(err.Error(), "input too large") {
		t.Fatalf("error does not include provider message: %v", err)
	}
}

func TestEncodeEmbeddingRequestRoundTripsAndKeepsRawHTML(t *testing.T) {
	inputs := []string{
		"## Heading\n\n- item one\n- item two\n",
		"UTF-8: § ' café %C3%A9 — em dash",
		"URL: https://example.com/a?b=1&c=2<tag>&d=3",
		"```go\nif a && b {\n\treturn `x`\n}\n```",
	}

	body, err := encodeEmbeddingRequest(openAIEmbeddingRequest{Model: "m", Input: inputs})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if !json.Valid(body) {
		t.Fatalf("encoded body is not valid JSON: %s", body)
	}

	var decoded openAIEmbeddingRequest
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Input) != len(inputs) {
		t.Fatalf("input count mismatch: want %d, got %d", len(inputs), len(decoded.Input))
	}
	for i, want := range inputs {
		if decoded.Input[i] != want {
			t.Fatalf("input %d not byte-identical:\nwant %q\n got %q", i, want, decoded.Input[i])
		}
	}

	raw := string(body)
	for _, token := range []string{"&", "<", ">"} {
		if !strings.Contains(raw, token) {
			t.Fatalf("expected raw %q in body (SetEscapeHTML(false)), got: %s", token, raw)
		}
	}
	for _, escaped := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(raw, escaped) {
			t.Fatalf("expected no HTML escape %q in body, got: %s", escaped, raw)
		}
	}
}

func TestOpenAIClientRejectsDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	embedder.Dimensions = 4
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	if !strings.Contains(err.Error(), "dimension mismatch") {
		t.Fatalf("error does not mention dimension mismatch: %v", err)
	}
}

func TestOpenAIClientRetriesTransientStatus(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily unavailable"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	embedder.BackoffBase = time.Millisecond
	vectors, err := embedder.Embed(context.Background(), []string{"first"})
	if err != nil {
		t.Fatalf("embed with retries: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("vector count mismatch: want 1, got %d", len(vectors))
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls (2 retries), got %d", got)
	}
}

func TestOpenAIClientDoesNotRetryClientError(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad input"}`))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	embedder.BackoffBase = time.Millisecond
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected client error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected no retry on 4xx, got %d calls", got)
	}
}

func TestOpenAIClientRetriesExhaustedReturnsError(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	embedder := NewOpenAIClient(server.URL, "test-model")
	embedder.MaxRetries = 2
	embedder.BackoffBase = time.Millisecond
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", got)
	}
}

func TestEmbeddingsEndpointVariants(t *testing.T) {
	if _, err := embeddingsEndpoint("://nope"); err == nil {
		t.Fatal("expected a parse error")
	}
	if _, err := embeddingsEndpoint("/no/host"); err == nil {
		t.Fatal("expected a missing scheme/host error")
	}
	if got, err := embeddingsEndpoint("http://h:1/v1/embeddings"); err != nil || got != "http://h:1/v1/embeddings" {
		t.Fatalf("suffix passthrough: %v %q", err, got)
	}
	if got, err := embeddingsEndpoint("http://h:1"); err != nil || got != "http://h:1/v1/embeddings" {
		t.Fatalf("append: %v %q", err, got)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	if !isRetryableStatus(429) || !isRetryableStatus(503) {
		t.Fatal("429 and 503 should be retryable")
	}
	if isRetryableStatus(400) {
		t.Fatal("400 should not be retryable")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" || truncate("abcdef", 3) != "abc" {
		t.Fatal("truncate behavior mismatch")
	}
}

func TestBackoffDelayCaps(t *testing.T) {
	c := OpenAIClient{}
	if c.backoffDelay(0) != DefaultBackoffBase {
		t.Fatalf("attempt 0 should be the base delay, got %v", c.backoffDelay(0))
	}
	if c.backoffDelay(100) != DefaultBackoffMax {
		t.Fatalf("a large attempt should cap at the max, got %v", c.backoffDelay(100))
	}
}

func TestResponseError(t *testing.T) {
	if err := responseError(500, nil); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("empty body: %v", err)
	}
	if err := responseError(500, []byte("boom")); !strings.Contains(err.Error(), "boom") {
		t.Fatalf("plain body: %v", err)
	}
	if err := responseError(500, []byte(`{"error":{"message":"nope"}}`)); !strings.Contains(err.Error(), "nope") {
		t.Fatalf("provider message: %v", err)
	}
}

func TestParseEmbeddingsErrors(t *testing.T) {
	if _, err := parseEmbeddings([]byte("not json"), 1); err == nil {
		t.Fatal("expected a decode error")
	}
	if _, err := parseEmbeddings([]byte(`{"data":[{"index":5,"embedding":[1]}]}`), 1); err == nil {
		t.Fatal("expected an index-out-of-range error")
	}
}

func TestValidateDimensions(t *testing.T) {
	if err := (OpenAIClient{Dimensions: 0}).validateDimensions([][]float32{{1}}); err != nil {
		t.Fatalf("zero dimensions should skip validation: %v", err)
	}
	if err := (OpenAIClient{Dimensions: 2}).validateDimensions([][]float32{{1, 2}}); err != nil {
		t.Fatalf("matching dimensions should pass: %v", err)
	}
	if err := (OpenAIClient{Dimensions: 2}).validateDimensions([][]float32{{1}}); err == nil {
		t.Fatal("expected a dimension mismatch error")
	}
}

func TestEmbeddingErrorUnmarshalJSON(t *testing.T) {
	var e embeddingError
	if err := e.UnmarshalJSON([]byte("null")); err != nil || e.Message != "" {
		t.Fatalf("null: %v %q", err, e.Message)
	}
	e = embeddingError{}
	if err := e.UnmarshalJSON([]byte(`{"message":"boom"}`)); err != nil || e.Message != "boom" {
		t.Fatalf("object form: %v %q", err, e.Message)
	}
	if err := (&embeddingError{}).UnmarshalJSON([]byte("123")); err == nil {
		t.Fatal("expected an error for an unexpected shape")
	}
}

func TestEmbedEarlyReturns(t *testing.T) {
	ctx := context.Background()
	if v, err := (OpenAIClient{Model: "m"}).Embed(ctx, nil); v != nil || err != nil {
		t.Fatalf("empty input: %v %v", v, err)
	}
	if _, err := (OpenAIClient{Model: "  "}).Embed(ctx, []string{"x"}); err == nil {
		t.Fatal("expected an error for an empty model")
	}
	if _, err := (OpenAIClient{Model: "m", BaseURL: "/no-scheme"}).Embed(ctx, []string{"x"}); err == nil {
		t.Fatal("expected an error for a bad base url")
	}
}

func TestClientDefaultsWhenNil(t *testing.T) {
	if (OpenAIClient{}).client() == nil {
		t.Fatal("expected a default http client")
	}
}

func TestSleepBackoffCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepBackoff(ctx, time.Hour); err == nil {
		t.Fatal("expected a context error")
	}
}
