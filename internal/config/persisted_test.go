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

	configPath := filepath.Join(t.TempDir(), "jcemb.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"model": "file-model",
		"vector_dim": 1536,
		"ollama": {
			"batch_size": 4
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

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{envDataDir, envProvider, envModel, envVectorDim, envOllamaURL, envOllamaBatchSize, envOllamaTimeout} {
		t.Setenv(name, "")
	}
}
