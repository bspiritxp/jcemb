//go:build integration

package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	providerollama "github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/stretchr/testify/require"
)

type integrationModel struct {
	Name string `json:"name"`
}

func TestOllamaIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("integration tests require INTEGRATION=1")
	}

	baseURL := "http://localhost:11434"
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		t.Skipf("ollama not available: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("ollama tags endpoint returned status %d", resp.StatusCode)
	}

	var tags struct {
		Models []integrationModel `json:"models"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tags))

	model := firstAvailableModel(tags.Models, []string{"qwen2.5", "llama3.2", "llama3.1"})
	if model == "" {
		t.Skip("no supported local ollama model found for integration test")
	}

	extractor, err := New(domain.TagExtractorConfig{
		Model:   model,
		Options: map[string]string{providerollama.OptionOllamaURL: baseURL},
	})
	require.NoError(t, err)

	result, err := extractor.Extract(context.Background(), domain.TagExtractRequest{
		Document: domain.Document{
			Title:   "Go systems programming",
			Content: "Go is a statically typed language for systems programming and command line tools.",
		},
		Config: domain.TagExtractorConfig{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Tags)
	require.True(t, containsAny(result.Tags, []string{"go", "programming", "language", "systems"}), "expected semantic tags to mention go/programming/language/systems, got %v", result.Tags)
}

func firstAvailableModel(models []integrationModel, candidates []string) string {
	for _, candidate := range candidates {
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.Name), strings.ToLower(candidate)) {
				return model.Name
			}
		}
	}
	return ""
}

func containsAny(values []string, candidates []string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if strings.Contains(strings.ToLower(value), strings.ToLower(candidate)) {
				return true
			}
		}
	}
	return false
}
