package app

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRunConfigCommandPersistsInteractiveSelections(t *testing.T) {
	input := bytes.NewBufferString("~/custom-store\n2048\n")
	output := &bytes.Buffer{}

	var saved config.PersistedConfig
	selectCalls := 0
	result, err := RunConfigCommand(ConfigCommandRequest{
		In:         input,
		Out:        output,
		ConfigPath: filepath.Join(t.TempDir(), "jcemb.json"),
		Settings: config.Settings{
			DataDir:   filepath.Join(t.TempDir(), ".local", "share", "jcemb"),
			Provider:  config.DefaultProviderName,
			Model:     config.DefaultModelName,
			VectorDim: config.DefaultVectorDim,
			Ollama: config.OllamaConfig{
				URL:       "http://localhost:11434",
				BatchSize: 8,
				Timeout:   30 * time.Second,
			},
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			selectCalls++
			if selectCalls == 1 {
				require.Equal(t, "Provider", request.Label)
				require.Equal(t, []string{config.DefaultProviderName}, request.Options)
				return config.DefaultProviderName, nil
			}

			require.Equal(t, "Model", request.Label)
			require.Equal(t, []string{config.DefaultModelName}, request.Options)
			return config.DefaultModelName, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, selectCalls)

	homeDir, homeErr := os.UserHomeDir()
	require.NoError(t, homeErr)
	require.Equal(t, filepath.Join(homeDir, "custom-store"), saved.DataDir)
	require.Equal(t, config.DefaultProviderName, saved.Provider)
	require.Equal(t, config.DefaultModelName, saved.Model)
	require.Equal(t, 2048, saved.VectorDim)
	require.Equal(t, "http://localhost:11434", saved.Ollama.URL)
	require.Equal(t, 8, saved.Ollama.BatchSize)
	require.Equal(t, "30s", saved.Ollama.Timeout)
	require.Equal(t, saved, result.Saved)

	require.Contains(t, output.String(), "Config file:")
	require.Contains(t, output.String(), "Data directory")
	require.Contains(t, output.String(), "Config saved.")
}

func TestRunConfigCommandRePromptsUntilVectorDimensionIsValid(t *testing.T) {
	input := bytes.NewBufferString("\nabc\n0\n1024\n")
	output := &bytes.Buffer{}

	var saved config.PersistedConfig
	_, err := RunConfigCommand(ConfigCommandRequest{
		In:         input,
		Out:        output,
		ConfigPath: filepath.Join(t.TempDir(), "jcemb.json"),
		Settings: config.Settings{
			DataDir:   filepath.Join(t.TempDir(), ".local", "share", "jcemb"),
			Provider:  config.DefaultProviderName,
			Model:     config.DefaultModelName,
			VectorDim: config.DefaultVectorDim,
			Ollama: config.OllamaConfig{
				URL:       "http://localhost:11434",
				BatchSize: 8,
				Timeout:   30 * time.Second,
			},
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1024, saved.VectorDim)
	require.Contains(t, output.String(), "vector dimension must be a positive integer")
}

func TestRunConfigCommandUsesSelectorDefaultsForProviderAndModel(t *testing.T) {
	input := bytes.NewBufferString("\n\n")
	output := &bytes.Buffer{}

	var prompts []ConfigSelectRequest
	_, err := RunConfigCommand(ConfigCommandRequest{
		In:         input,
		Out:        output,
		ConfigPath: filepath.Join(t.TempDir(), "jcemb.json"),
		Settings: config.Settings{
			DataDir:   filepath.Join(t.TempDir(), ".local", "share", "jcemb"),
			Provider:  config.DefaultProviderName,
			Model:     config.DefaultModelName,
			VectorDim: config.DefaultVectorDim,
			Ollama: config.OllamaConfig{
				URL:       "http://localhost:11434",
				BatchSize: 8,
				Timeout:   30 * time.Second,
			},
		},
		Save: func(cfg config.PersistedConfig) error {
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			prompts = append(prompts, request)
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, prompts, 2)
	require.Equal(t, "Provider", prompts[0].Label)
	require.Equal(t, []string{config.DefaultProviderName}, prompts[0].Options)
	require.Equal(t, config.DefaultProviderName, prompts[0].DefaultValue)
	require.Equal(t, "Model", prompts[1].Label)
	require.Equal(t, []string{config.DefaultModelName}, prompts[1].Options)
	require.Equal(t, config.DefaultModelName, prompts[1].DefaultValue)
}

func TestReadSelectionActionUnderstandsArrowKeys(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\x1b[B\x1b[A\r"))

	action, err := readSelectionAction(reader)
	require.NoError(t, err)
	require.Equal(t, selectionActionDown, action)

	selected := applySelectionAction(0, action, 2)
	require.Equal(t, 1, selected)

	action, err = readSelectionAction(reader)
	require.NoError(t, err)
	require.Equal(t, selectionActionUp, action)

	selected = applySelectionAction(selected, action, 2)
	require.Equal(t, 0, selected)

	action, err = readSelectionAction(reader)
	require.NoError(t, err)
	require.Equal(t, selectionActionConfirm, action)
}
