package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewBootstrapLoadsPersistedConfig(t *testing.T) {
	homeDir := t.TempDir()
	setTestHome(t, homeDir)

	require.NoError(t, config.Save(config.PersistedConfig{
		DataDir:   filepath.Join(homeDir, ".local", "share", "jcemb-custom"),
		Provider:  config.DefaultProviderName,
		Model:     "custom-model",
		VectorDim: 2048,
		Ollama: config.PersistedOllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 4,
			Timeout:   "45s",
		},
	}))

	bootstrap := NewBootstrap()
	require.NoError(t, bootstrap.Validate())
	require.True(t, bootstrap.Config.LoadedFromFile)
	require.Equal(t, "custom-model", bootstrap.Config.Settings.Model)
	require.Equal(t, 2048, bootstrap.Config.Settings.VectorDim)
	require.Equal(t, 4, bootstrap.Config.Settings.Ollama.BatchSize)
}

func TestNewBootstrapPreservesFallbackDefaultsWhenConfigIsInvalid(t *testing.T) {
	homeDir := t.TempDir()
	setTestHome(t, homeDir)

	configPath := filepath.Join(homeDir, ".config", "jcemb", "jcemb.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`{"provider":`), 0o644))

	bootstrap := NewBootstrap()
	require.Error(t, bootstrap.Validate())
	require.True(t, errors.Is(bootstrap.Validate(), config.ErrInvalidConfig))
	require.Equal(t, config.DefaultProviderName, bootstrap.Config.Settings.Provider)
	require.Equal(t, config.DefaultModelName, bootstrap.Config.Settings.Model)
}

func setTestHome(t *testing.T, homeDir string) {
	t.Helper()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
}
