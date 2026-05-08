package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRunDoctorCommandRendersJSON(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "jcemb.json")
	require.NoError(t, os.WriteFile(configPath, []byte("{}"), 0o644))

	var output bytes.Buffer
	result, err := RunDoctorCommand(DoctorCommandRequest{
		Out:        &output,
		ConfigPath: configPath,
		Settings:   doctorSettings(root),
		JSON:       true,
		CheckHTTP:  func(context.Context, string) error { return nil },
		RunPython:  func(context.Context, string) error { return nil },
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Checks)
	require.Contains(t, output.String(), `"config_path"`)
	require.Contains(t, output.String(), `"checks"`)
}

func TestRunDoctorCommandReportsRuntimeFailures(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	_, err := RunDoctorCommand(DoctorCommandRequest{
		Out:        &output,
		ConfigPath: filepath.Join(root, "missing.json"),
		Settings:   doctorSettings(root),
		CheckHTTP:  func(context.Context, string) error { return errors.New("connection refused") },
		RunPython:  func(context.Context, string) error { return errors.New("no module named open_clip") },
	})
	require.NoError(t, err)
	text := output.String()
	require.Contains(t, text, "[warn] config")
	require.Contains(t, text, "[fail] ollama")
	require.Contains(t, text, "[fail] image")
}

func doctorSettings(root string) config.Settings {
	return config.Settings{
		DataDir:   root,
		Provider:  config.DefaultProviderName,
		Model:     config.DefaultModelName,
		VectorDim: config.DefaultVectorDim,
		Ollama: config.OllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   30 * time.Second,
		},
		OpenAI: config.OpenAIConfig{
			BaseURL:    "https://api.openai.com/v1",
			BatchSize:  128,
			Timeout:    60 * time.Second,
			Dimensions: config.OpenAIDefaultDim,
		},
		Image: config.ImageConfig{
			Provider:    "openclip",
			Model:       "ViT-B-32",
			Pretrained:  "laion2b_s34b_b79k",
			Dimensions:  512,
			Device:      "auto",
			Python:      "python3",
			VisionModel: "llava",
		},
	}
}
