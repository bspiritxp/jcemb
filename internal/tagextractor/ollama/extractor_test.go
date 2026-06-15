package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestNewRegistersExtractor(t *testing.T) {
	factory, err := registry.GetTagExtractor("ollama")
	require.NoError(t, err)
	require.NotNil(t, factory)

	extractor, err := factory(domain.TagExtractorConfig{Model: "qwen2.5"})
	require.NoError(t, err)
	require.NotNil(t, extractor)
}

func TestExtractorExtractSuccess(t *testing.T) {
	var captured struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
		Format string `json:"format"`
		Prompt string `json:"prompt"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/generate", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_, _ = w.Write([]byte(`{"response":"{\"tags\":[\" Go \",\"CLI\",\"go\",\"http://evil\"]}"}`))
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, time.Second)
	content := strings.Repeat("界", maxContentRunes+50)

	result, err := extractor.Extract(context.Background(), domain.TagExtractRequest{
		Document: domain.Document{Title: " Doc Title ", Content: content},
		Config:   domain.TagExtractorConfig{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"go", "cli"}, result.Tags)
	require.Equal(t, "qwen2.5", captured.Model)
	require.False(t, captured.Stream)
	require.Equal(t, "json", captured.Format)
	require.Contains(t, captured.Prompt, "Extract 1-8 concise topical tags for retrieval")
	require.Contains(t, captured.Prompt, "Document title: Doc Title")
	require.Contains(t, captured.Prompt, "Content (truncated): ")
	require.Equal(t, maxContentRunes, utf8.RuneCountInString(truncateRunes(content, maxContentRunes)))
	require.Contains(t, captured.Prompt, truncateRunes(content, maxContentRunes))
	require.NotContains(t, captured.Prompt, content)
}

func TestExtractorHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"service down"}`))
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, time.Second)
	_, err := extractor.Extract(context.Background(), domain.TagExtractRequest{Document: domain.Document{Content: "body"}, Config: defaultRequestConfig()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama")
	require.Contains(t, err.Error(), "service down")
}

func TestExtractorInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"not json at all"}`))
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, time.Second)
	_, err := extractor.Extract(context.Background(), domain.TagExtractRequest{Document: domain.Document{Content: "body"}, Config: defaultRequestConfig()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode tags JSON")
}

func TestExtractorTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"response":"{\"tags\":[\"go\"]}"}`))
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, 10*time.Millisecond)
	_, err := extractor.Extract(context.Background(), domain.TagExtractRequest{Document: domain.Document{Content: "body"}, Config: defaultRequestConfig()})
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestExtractorEmptyTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"{\"tags\":[]}"}`))
	}))
	defer server.Close()

	extractor := newTestExtractor(t, server.URL, time.Second)
	result, err := extractor.Extract(context.Background(), domain.TagExtractRequest{Document: domain.Document{Content: "body"}, Config: defaultRequestConfig()})
	require.NoError(t, err)
	require.Empty(t, result.Tags)
	require.NotNil(t, result.Tags)
}

func newTestExtractor(t *testing.T, baseURL string, timeout time.Duration) *Extractor {
	t.Helper()
	extractor, err := New(domain.TagExtractorConfig{
		Model:   "qwen2.5",
		Timeout: timeout,
		Options: map[string]string{ollama.OptionOllamaURL: baseURL},
	})
	require.NoError(t, err)
	typed, ok := extractor.(*Extractor)
	require.True(t, ok)
	return typed
}

func defaultRequestConfig() domain.TagExtractorConfig {
	return domain.TagExtractorConfig{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32}
}

func TestDecodeAPIErrorFallback(t *testing.T) {
	require.Equal(t, "plain text", decodeAPIError([]byte("plain text")))
	require.Equal(t, http.StatusText(http.StatusInternalServerError), decodeAPIError(nil))
}
