package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	embedapp "github.com/bspiritxp/jcemb/internal/app/embed"
	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestEmbedReturnsFailingFileDetailsInErrorText(t *testing.T) {
	rootDir := t.TempDir()
	badPath := filepath.Join(rootDir, "docs", "bad.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(badPath), 0o755))
	require.NoError(t, os.WriteFile(badPath, []byte("---\ntags: [broken\n---\nbody\n"), 0o644))

	err := Embed(EmbedRequest{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    "ollama",
		Model:       "bge-m3",
		Recursive:   true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "completed with 1 file error(s)")
	require.Contains(t, err.Error(), "docs/bad.md")
	require.Contains(t, err.Error(), "invalid yaml front matter")
}

func TestRunEmbedLoadsEnabledTagExtractorFromConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	defaults := config.DefaultSettings()
	configPath := filepath.Join(homeDir, ".config", "jcemb", "jcemb.json")
	require.NoError(t, config.SaveToPath(configPath, persistedConfigFromSettings(defaults, config.TagExtractorConfig{
		Enabled:       true,
		Provider:      config.OpenAIProviderName,
		Model:         "gpt-4.1-mini",
		MaxTags:       6,
		MinTagLen:     3,
		MaxTagLen:     24,
		SkipIfHasYAML: true,
		Timeout:       45 * time.Second,
		Options: map[string]string{
			"custom_tag_option": "on",
		},
	})))

	original := runEmbedService
	defer func() { runEmbedService = original }()
	var captured embedapp.Request
	runEmbedService = func(_ context.Context, request embedapp.Request) (EmbedResult, error) {
		captured = request
		return EmbedResult{}, nil
	}

	_, err := RunEmbed(context.Background(), EmbedRequest{Path: t.TempDir(), Provider: defaults.Provider, Model: defaults.Model})
	require.NoError(t, err)
	require.Equal(t, "gpt-4.1-mini", captured.TagExtractor.Model)
	require.Equal(t, config.OpenAIProviderName, captured.TagExtractor.Provider)
	require.Equal(t, 45*time.Second, captured.TagExtractor.Timeout)
	require.Equal(t, 6, captured.TagExtractor.MaxTags)
	require.Equal(t, 3, captured.TagExtractor.MinTagLen)
	require.Equal(t, 24, captured.TagExtractor.MaxTagLen)
	require.True(t, captured.TagExtractor.SkipIfHasYAML)
	require.Equal(t, "on", captured.TagExtractor.Options["custom_tag_option"])
	require.Equal(t, defaults.OpenAI.BaseURL, captured.TagExtractor.Options["openai_base_url"])
	require.Equal(t, defaults.OpenAI.APIKey, captured.TagExtractor.Options["openai_api_key"])
}

func TestRunQueryLoadsEnabledTagExtractorFromConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	defaults := config.DefaultSettings()
	configPath := filepath.Join(homeDir, ".config", "jcemb", "jcemb.json")
	require.NoError(t, config.SaveToPath(configPath, persistedConfigFromSettings(defaults, config.TagExtractorConfig{
		Enabled:       true,
		Provider:      config.OpenAIProviderName,
		Model:         "gpt-4.1-mini",
		MaxTags:       5,
		MinTagLen:     2,
		MaxTagLen:     20,
		SkipIfHasYAML: true,
		Timeout:       30 * time.Second,
		Options: map[string]string{
			"custom_tag_option": "query",
		},
	})))

	original := runQueryService
	defer func() { runQueryService = original }()
	var captured queryapp.Request
	runQueryService = func(_ context.Context, request queryapp.Request) (QueryResult, error) {
		captured = request
		return QueryResult{}, nil
	}

	_, err := RunQuery(context.Background(), QueryRequest{Text: "long enough query text", Provider: defaults.Provider, Explain: true})
	require.NoError(t, err)
	require.True(t, captured.Explain)
	require.Equal(t, config.OpenAIProviderName, captured.TagExtractor.Provider)
	require.Equal(t, "gpt-4.1-mini", captured.TagExtractor.Model)
	require.Equal(t, 30*time.Second, captured.TagExtractor.Timeout)
	require.Equal(t, "query", captured.TagExtractor.Options["custom_tag_option"])
	require.Equal(t, defaults.OpenAI.BaseURL, captured.TagExtractor.Options["openai_base_url"])
	require.Equal(t, defaults.OpenAI.APIKey, captured.TagExtractor.Options["openai_api_key"])
}

func TestQueryExplainRequiresJSONOutput(t *testing.T) {
	err := Query(QueryRequest{Text: "lookup", Explain: true, Format: "text"})

	require.Error(t, err)
	require.Equal(t, "query: --explain requires JSON output", err.Error())
}

func TestQueryExplainRejectsJSONFlagWithNonJSONFormat(t *testing.T) {
	err := Query(QueryRequest{Text: "lookup", Explain: true, JSON: true, Format: "table"})

	require.Error(t, err)
	require.Equal(t, "query: --explain requires JSON output", err.Error())
}

func persistedConfigFromSettings(settings config.Settings, tagExtractor config.TagExtractorConfig) config.PersistedConfig {
	return config.PersistedConfig{
		DataDir:   settings.DataDir,
		Provider:  settings.Provider,
		Model:     settings.Model,
		VectorDim: settings.VectorDim,
		Ollama: config.PersistedOllamaConfig{
			URL:       settings.Ollama.URL,
			BatchSize: settings.Ollama.BatchSize,
			Timeout:   settings.Ollama.Timeout.String(),
		},
		OpenAI: config.PersistedOpenAIConfig{
			BaseURL:    settings.OpenAI.BaseURL,
			APIKey:     settings.OpenAI.APIKey,
			BatchSize:  settings.OpenAI.BatchSize,
			Timeout:    settings.OpenAI.Timeout.String(),
			Dimensions: settings.OpenAI.Dimensions,
			InputType:  settings.OpenAI.InputType,
		},
		Image: config.PersistedImageConfig{
			Provider:    settings.Image.Provider,
			Model:       settings.Image.Model,
			Pretrained:  settings.Image.Pretrained,
			Dimensions:  settings.Image.Dimensions,
			Device:      settings.Image.Device,
			Python:      settings.Image.Python,
			VisionModel: settings.Image.VisionModel,
		},
		TagExtractor: config.PersistedTagExtractorConfig{
			Enabled:       tagExtractor.Enabled,
			Provider:      tagExtractor.Provider,
			Model:         tagExtractor.Model,
			MaxTags:       tagExtractor.MaxTags,
			MinTagLen:     tagExtractor.MinTagLen,
			MaxTagLen:     tagExtractor.MaxTagLen,
			SkipIfHasYAML: tagExtractor.SkipIfHasYAML,
			Timeout:       tagExtractor.Timeout.String(),
			Options:       cloneOptions(tagExtractor.Options),
		},
	}
}
