package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadFromPathFallsBackToDefaultsWhenConfigIsMissing(t *testing.T) {
	clearConfigEnv(t)

	loaded, err := LoadFromPath(filepath.Join(t.TempDir(), "missing.json"))
	require.NoError(t, err)
	require.False(t, loaded.LoadedFromFile)
	require.Equal(t, DefaultProviderName, loaded.Settings.Provider)
	require.Equal(t, DefaultModelName, loaded.Settings.Model)
	require.Equal(t, DefaultVectorDim, loaded.Settings.VectorDim)
	require.Equal(t, "http://localhost:11434", loaded.Settings.Ollama.URL)
	require.Equal(t, 8, loaded.Settings.Ollama.BatchSize)
	require.Equal(t, 30*time.Second, loaded.Settings.Ollama.Timeout)
	require.Equal(t, "openclip", loaded.Settings.Image.Provider)
	require.Equal(t, "ViT-B-32", loaded.Settings.Image.Model)
	require.Equal(t, "laion2b_s34b_b79k", loaded.Settings.Image.Pretrained)
	require.Equal(t, 512, loaded.Settings.Image.Dimensions)
	require.Equal(t, "https://api.openai.com/v1", loaded.Settings.OpenAI.BaseURL)
	require.Equal(t, 1536, loaded.Settings.OpenAI.Dimensions)
	require.True(t, loaded.Settings.TagExtractor.Enabled)
	require.Equal(t, DefaultProviderName, loaded.Settings.TagExtractor.Provider)
	require.Equal(t, "qwen2.5:7b", loaded.Settings.TagExtractor.Model)
	require.Equal(t, 8, loaded.Settings.TagExtractor.MaxTags)
	require.Equal(t, 2, loaded.Settings.TagExtractor.MinTagLen)
	require.Equal(t, 32, loaded.Settings.TagExtractor.MaxTagLen)
	require.True(t, loaded.Settings.TagExtractor.SkipIfHasYAML)
	require.Equal(t, 30*time.Second, loaded.Settings.TagExtractor.Timeout)
}

func TestLoadFromPathReturnsTypedErrorForCorruptJSON(t *testing.T) {
	clearConfigEnv(t)

	configPath := filepath.Join(t.TempDir(), "jcemb.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"provider":`), 0o644))

	_, err := LoadFromPath(configPath)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidConfig))
	require.Contains(t, err.Error(), configPath)
}

func TestSaveToPathWritesAtomically(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "jcemb", "jcemb.json")

	require.NoError(t, SaveToPath(configPath, PersistedConfig{
		DataDir:   filepath.Join(t.TempDir(), "data"),
		Provider:  DefaultProviderName,
		Model:     DefaultModelName,
		VectorDim: DefaultVectorDim,
		Ollama: PersistedOllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   "30s",
		},
	}))

	entries, err := os.ReadDir(filepath.Dir(configPath))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "jcemb.json", entries[0].Name())

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var saved map[string]any
	require.NoError(t, json.Unmarshal(content, &saved))
	require.Equal(t, DefaultProviderName, saved["provider"])
	require.Equal(t, float64(DefaultVectorDim), saved["vector_dim"])
}

func TestLoadFromPathAppliesConfigOverEnvAndEnvOverBuiltins(t *testing.T) {
	envDataDirPath := filepath.Join(t.TempDir(), "env-data")
	t.Setenv(envDataDir, envDataDirPath)
	t.Setenv(envProvider, "env-provider")
	t.Setenv(envModel, "env-model")
	t.Setenv(envVectorDim, "2048")
	t.Setenv(envOllamaURL, "http://env-host:11434")
	t.Setenv(envOllamaBatchSize, "12")
	t.Setenv(envOllamaTimeout, "45s")
	t.Setenv(envImageProvider, "openclip")
	t.Setenv(envImageModel, "env-image-model")
	t.Setenv(envImageDimensions, "768")
	t.Setenv(envOpenAIAPIKey, "env-openai-key")
	t.Setenv(envTagExtractorProvider, "openai")
	t.Setenv(envTagExtractorModel, "gpt-4.1-mini")
	t.Setenv(envTagExtractorEnabled, "false")

	configPath := filepath.Join(t.TempDir(), "jcemb.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"model": "file-model",
		"vector_dim": 1536,
		"openai": {
			"base_url": "https://example.test/v1",
			"dimensions": 512
		},
		"image": {
			"provider": "jina-clip",
			"model": "jinaai/jina-clip-v2",
			"dimensions": 512
		},
		"ollama": {
			"batch_size": 4
		},
		"tag_extractor": {
			"max_tags": 5,
			"skip_if_has_yaml": true
		}
	}`), 0o644))

	loaded, err := LoadFromPath(configPath)
	require.NoError(t, err)
	require.True(t, loaded.LoadedFromFile)
	require.Equal(t, filepath.Clean(envDataDirPath), loaded.Settings.DataDir)
	require.Equal(t, "env-provider", loaded.Settings.Provider)
	require.Equal(t, "file-model", loaded.Settings.Model)
	require.Equal(t, 1536, loaded.Settings.VectorDim)
	require.Equal(t, "http://env-host:11434", loaded.Settings.Ollama.URL)
	require.Equal(t, 4, loaded.Settings.Ollama.BatchSize)
	require.Equal(t, 45*time.Second, loaded.Settings.Ollama.Timeout)
	require.Equal(t, "jina-clip", loaded.Settings.Image.Provider)
	require.Equal(t, "jinaai/jina-clip-v2", loaded.Settings.Image.Model)
	require.Equal(t, 512, loaded.Settings.Image.Dimensions)
	require.Equal(t, "https://example.test/v1", loaded.Settings.OpenAI.BaseURL)
	require.Equal(t, "env-openai-key", loaded.Settings.OpenAI.APIKey)
	require.False(t, loaded.Settings.TagExtractor.Enabled)
	require.Equal(t, "openai", loaded.Settings.TagExtractor.Provider)
	require.Equal(t, "gpt-4.1-mini", loaded.Settings.TagExtractor.Model)
	require.Equal(t, 5, loaded.Settings.TagExtractor.MaxTags)
	require.True(t, loaded.Settings.TagExtractor.SkipIfHasYAML)
}

func TestSaveToPathExcludesTransientQueryFlags(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "jcemb.json")
	require.NoError(t, SaveToPath(configPath, PersistedConfig{
		DataDir:   filepath.Join(t.TempDir(), "data"),
		Provider:  DefaultProviderName,
		Model:     DefaultModelName,
		VectorDim: DefaultVectorDim,
		Ollama: PersistedOllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   "30s",
		},
	}))

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	payload := string(content)
	require.NotContains(t, payload, "threshold-alpha")
	require.NotContains(t, payload, "threshold-delta")
	require.NotContains(t, payload, "mmr-lambda")
	require.NotContains(t, payload, "search-window")
	require.NotContains(t, payload, "unique")
}

func TestPersistedConfigSettingsExpandsTildeDataDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	settings, err := PersistedConfig{
		DataDir:   filepath.Join("~", "jcemb-config-test"),
		Provider:  DefaultProviderName,
		Model:     DefaultModelName,
		VectorDim: DefaultVectorDim,
		Ollama: PersistedOllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   "30s",
		},
	}.Settings()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(homeDir, "jcemb-config-test"), settings.DataDir)
}

func TestLoadFromPathBackfillsTagExtractorDefaultsForOldConfig(t *testing.T) {
	clearConfigEnv(t)

	configPath := filepath.Join(t.TempDir(), "old-config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"provider": "ollama",
		"model": "bge-m3",
		"vector_dim": 1024,
		"ollama": {
			"url": "http://localhost:11434",
			"batch_size": 8,
			"timeout": "30s"
		}
	}`), 0o644))

	loaded, err := LoadFromPath(configPath)
	require.NoError(t, err)
	require.True(t, loaded.Settings.TagExtractor.Enabled)
	require.Equal(t, DefaultProviderName, loaded.Settings.TagExtractor.Provider)
	require.Equal(t, "qwen2.5:7b", loaded.Settings.TagExtractor.Model)
	require.Equal(t, 8, loaded.Settings.TagExtractor.MaxTags)
	require.Equal(t, 2, loaded.Settings.TagExtractor.MinTagLen)
	require.Equal(t, 32, loaded.Settings.TagExtractor.MaxTagLen)
	require.True(t, loaded.Settings.TagExtractor.SkipIfHasYAML)
	require.Equal(t, 30*time.Second, loaded.Settings.TagExtractor.Timeout)
}

func TestLoadFromPathDefaultsTagExtractorToOpenAIWhenProviderIsOpenAI(t *testing.T) {
	clearConfigEnv(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"data_dir": "`+filepath.ToSlash(t.TempDir())+`",
		"provider": "openai",
		"openai": {
			"api_key": "sk-test"
		}
	}`), 0o644))

	loaded, err := LoadFromPath(configPath)

	require.NoError(t, err)
	require.Equal(t, OpenAIProviderName, loaded.Settings.Provider)
	require.Equal(t, OpenAIDefaultModel, loaded.Settings.Model)
	require.Equal(t, OpenAIProviderName, loaded.Settings.TagExtractor.Provider)
	require.Equal(t, OpenAITagExtractorDefaultModel, loaded.Settings.TagExtractor.Model)
}

func TestPersistedConfigSettingsKeepsExplicitDisabledTagExtractor(t *testing.T) {
	clearConfigEnv(t)

	base := PersistedFromSettings(DefaultSettings())
	base.DataDir = filepath.Join(t.TempDir(), "data")

	payload, err := json.Marshal(base)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	decoded["tag_extractor"] = map[string]any{"enabled": false}

	payload, err = json.Marshal(decoded)
	require.NoError(t, err)

	var persisted PersistedConfig
	require.NoError(t, json.Unmarshal(payload, &persisted))

	settings, err := persisted.Settings()
	require.NoError(t, err)
	require.False(t, settings.TagExtractor.Enabled)
	require.Equal(t, DefaultProviderName, settings.TagExtractor.Provider)
	require.Equal(t, "qwen2.5:7b", settings.TagExtractor.Model)
	require.Equal(t, 8, settings.TagExtractor.MaxTags)
	require.Equal(t, 2, settings.TagExtractor.MinTagLen)
	require.Equal(t, 32, settings.TagExtractor.MaxTagLen)
	require.True(t, settings.TagExtractor.SkipIfHasYAML)
	require.Equal(t, 30*time.Second, settings.TagExtractor.Timeout)
	require.Empty(t, settings.TagExtractor.Options)
}

func TestTagExtractorConfigRoundTrip(t *testing.T) {
	clearConfigEnv(t)

	original := DefaultSettings()
	original.DataDir = filepath.Join(t.TempDir(), "data")
	original.TagExtractor = TagExtractorConfig{
		Enabled:       true,
		Provider:      "openai",
		Model:         "gpt-4.1-mini",
		MaxTags:       6,
		MinTagLen:     3,
		MaxTagLen:     24,
		SkipIfHasYAML: false,
		Timeout:       45 * time.Second,
		Options: map[string]string{
			"openai_base_url": "https://example.test/v1",
			"custom":          "value",
		},
	}
	original.OpenAI.APIKey = "sk-test"

	configPath := filepath.Join(t.TempDir(), "jcemb.json")
	require.NoError(t, SaveToPath(configPath, PersistedFromSettings(original)))

	loaded, err := LoadFromPath(configPath)
	require.NoError(t, err)
	require.Equal(t, original, loaded.Settings)
}

func TestPersistedConfigSettingsValidatesTagExtractor(t *testing.T) {
	base := PersistedFromSettings(DefaultSettings())

	t.Run("enabled requires provider", func(t *testing.T) {
		cfg := base
		cfg.TagExtractor.Provider = ""
		_, err := cfg.Settings()
		require.ErrorContains(t, err, "tag_extractor.provider is required")
	})

	t.Run("enabled requires model", func(t *testing.T) {
		cfg := base
		cfg.TagExtractor.Model = ""
		_, err := cfg.Settings()
		require.ErrorContains(t, err, "tag_extractor.model is required")
	})

	t.Run("max tags must be positive", func(t *testing.T) {
		cfg := base
		cfg.TagExtractor.MaxTags = 0
		_, err := cfg.Settings()
		require.ErrorContains(t, err, "tag_extractor.max_tags must be >= 1")
	})

	t.Run("min cannot exceed max", func(t *testing.T) {
		cfg := base
		cfg.TagExtractor.MinTagLen = 10
		cfg.TagExtractor.MaxTagLen = 5
		_, err := cfg.Settings()
		require.ErrorContains(t, err, "tag_extractor.min_tag_len must be <= tag_extractor.max_tag_len")
	})
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{envDataDir, envProvider, envModel, envVectorDim, envOllamaURL, envOllamaBatchSize, envOllamaTimeout, envImageProvider, envImageModel, envImagePretrained, envImageDimensions, envImageDevice, envImagePython, envImageVision, envOpenAIBaseURL, envOpenAIAPIKey, envOpenAITimeout, envOpenAIBatchSize, envOpenAIDimensions, envOpenAIInputType, envTagExtractorProvider, envTagExtractorModel, envTagExtractorEnabled} {
		t.Setenv(name, "")
	}
}
