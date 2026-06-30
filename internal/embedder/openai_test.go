package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedderPostsWithoutBearerToken(t *testing.T) {
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

	embedder := NewOpenAIEmbedder(server.URL, "test-model")
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

func TestOpenAIEmbedderPostsArbitraryMarkdownContentAsText(t *testing.T) {
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

	embedder := NewOpenAIEmbedder(server.URL, "test-model")
	if _, err := embedder.Embed(context.Background(), []string{input}); err != nil {
		t.Fatalf("embed arbitrary markdown content: %v", err)
	}
}

func TestOpenAIEmbedderRejectsMissingEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "test-model")
	if _, err := embedder.Embed(context.Background(), []string{"first", "second"}); err == nil {
		t.Fatal("expected missing embedding error")
	}
}

func TestOpenAIEmbedderReportsStringErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected embedding error")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error does not include provider message: %v", err)
	}
}

func TestOpenAIEmbedderReportsObjectErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"input too large"}}`))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "test-model")
	_, err := embedder.Embed(context.Background(), []string{"first"})
	if err == nil {
		t.Fatal("expected embedding error")
	}
	if !strings.Contains(err.Error(), "input too large") {
		t.Fatalf("error does not include provider message: %v", err)
	}
}
