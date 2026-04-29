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

var defaultOllamaModelOptions = []string{
	config.DefaultModelName,
	"qwen3-embedding:0.5b",
	"qwen3-embedding:4b",
	CustomModelOptionLabel,
}

func defaultImageSettings() config.ImageConfig {
	return config.ImageConfig{
		Provider:    "openclip",
		Model:       "ViT-B-32",
		Pretrained:  "laion2b_s34b_b79k",
		Dimensions:  512,
		Device:      "auto",
		Python:      "python3",
		VisionModel: "llava",
	}
}

func TestRunConfigCommandPersistsInteractiveSelections(t *testing.T) {
	input := bytes.NewBufferString("~/custom-store\n2048\n512\nllava\n")
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
			Image: defaultImageSettings(),
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
			switch selectCalls {
			case 1:
				require.Equal(t, "Provider", request.Label)
				require.Equal(t, []string{config.DefaultProviderName}, request.Options)
				return config.DefaultProviderName, nil
			case 2:
				require.Equal(t, "Model", request.Label)
				require.Equal(t, defaultOllamaModelOptions, request.Options)
				return config.DefaultModelName, nil
			case 3:
				require.Equal(t, "Image provider", request.Label)
				return "openclip", nil
			default:
				require.Equal(t, "Image model", request.Label)
				return "ViT-B-32", nil
			}
		},
	})
	require.NoError(t, err)
	require.Equal(t, 4, selectCalls)

	homeDir, homeErr := os.UserHomeDir()
	require.NoError(t, homeErr)
	require.Equal(t, filepath.Join(homeDir, "custom-store"), saved.DataDir)
	require.Equal(t, config.DefaultProviderName, saved.Provider)
	require.Equal(t, config.DefaultModelName, saved.Model)
	require.Equal(t, 2048, saved.VectorDim)
	require.Equal(t, "http://localhost:11434", saved.Ollama.URL)
	require.Equal(t, 8, saved.Ollama.BatchSize)
	require.Equal(t, "30s", saved.Ollama.Timeout)
	require.Equal(t, "openclip", saved.Image.Provider)
	require.Equal(t, "ViT-B-32", saved.Image.Model)
	require.Equal(t, 512, saved.Image.Dimensions)
	require.Equal(t, "llava", saved.Image.VisionModel)
	require.Equal(t, saved, result.Saved)

	require.Contains(t, output.String(), "Config file:")
	require.Contains(t, output.String(), "Data directory")
	require.Contains(t, output.String(), "Config saved.")
	require.Contains(t, output.String(), "Image scan provider:")
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
	input := bytes.NewBufferString("\n\n\n\n")
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
			Image: defaultImageSettings(),
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
	require.Len(t, prompts, 4)
	require.Equal(t, "Provider", prompts[0].Label)
	require.Equal(t, []string{config.DefaultProviderName}, prompts[0].Options)
	require.Equal(t, config.DefaultProviderName, prompts[0].DefaultValue)
	require.Equal(t, "Model", prompts[1].Label)
	require.Equal(t, defaultOllamaModelOptions, prompts[1].Options)
	require.Equal(t, config.DefaultModelName, prompts[1].DefaultValue)
	require.Equal(t, "Image provider", prompts[2].Label)
	require.Equal(t, []string{"openclip", "jina-clip", "openai", CustomImageProviderOptionLabel}, prompts[2].Options)
	require.Equal(t, "openclip", prompts[2].DefaultValue)
	require.Equal(t, "Image model", prompts[3].Label)
	require.Equal(t, []string{"ViT-B-32", "ViT-L-14", CustomModelOptionLabel}, prompts[3].Options)
	require.Equal(t, "ViT-B-32", prompts[3].DefaultValue)
}

func TestRunConfigCommandPreservesExistingCustomModel(t *testing.T) {
	input := bytes.NewBufferString("\n1024\n\n\n")
	output := &bytes.Buffer{}

	var saved config.PersistedConfig
	var modelPrompt ConfigSelectRequest
	_, err := RunConfigCommand(ConfigCommandRequest{
		In:         input,
		Out:        output,
		ConfigPath: filepath.Join(t.TempDir(), "jcemb.json"),
		Settings: config.Settings{
			DataDir:   filepath.Join(t.TempDir(), ".local", "share", "jcemb"),
			Provider:  config.DefaultProviderName,
			Model:     "my-private-model",
			VectorDim: config.DefaultVectorDim,
			Ollama: config.OllamaConfig{
				URL:       "http://localhost:11434",
				BatchSize: 8,
				Timeout:   30 * time.Second,
			},
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			if request.Label == "Model" {
				modelPrompt = request
			}
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{config.DefaultModelName, "qwen3-embedding:0.5b", "qwen3-embedding:4b", "my-private-model", CustomModelOptionLabel}, modelPrompt.Options)
	require.Equal(t, "my-private-model", modelPrompt.DefaultValue)
	require.Equal(t, "my-private-model", saved.Model)
}

func TestSupportedModelsIncludesQwen3EmbeddingOptions(t *testing.T) {
	require.Equal(t, []string{
		config.DefaultModelName,
		"qwen3-embedding:0.5b",
		"qwen3-embedding:4b",
	}, supportedModels(config.DefaultProviderName))
}

func TestRunConfigCommandAcceptsCustomModelInput(t *testing.T) {
	input := bytes.NewBufferString("\nbrand-new-model\n1024\n\n\n")
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
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			if request.Label == "Model" {
				return CustomModelOptionLabel, nil
			}
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, "brand-new-model", saved.Model)
	require.Contains(t, output.String(), "Custom model name")
}

func TestRunConfigCommandRejectsEmptyCustomModelName(t *testing.T) {
	input := bytes.NewBufferString("\n   \nmy-model\n1024\n\n\n")
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
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			if request.Label == "Model" {
				return CustomModelOptionLabel, nil
			}
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, "my-model", saved.Model)
	require.Contains(t, output.String(), "custom model name is required")
}

func TestRenderSelectionOptionsHighlightsSelectedRow(t *testing.T) {
	output := &bytes.Buffer{}
	renderSelectionOptions(output, []string{"alpha", "beta"}, 1, false)

	rendered := output.String()
	require.Contains(t, rendered, ansiSelectedPrefix+"> beta"+ansiReset)
	require.NotContains(t, rendered, ansiSelectedPrefix+"> alpha"+ansiReset)
	require.Contains(t, rendered, "  alpha\n")
}

func TestRunConfigCommandPromptsImageProviderAndModelLists(t *testing.T) {
	input := bytes.NewBufferString("\n\n\n\n")
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
			Image: config.ImageConfig{
				Provider:    "jina-clip",
				Model:       "jinaai/jina-clip-v2",
				Pretrained:  "laion2b_s34b_b79k",
				Dimensions:  1024,
				Device:      "auto",
				Python:      "python3",
				VisionModel: "llava",
			},
		},
		Save: func(cfg config.PersistedConfig) error { return nil },
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			prompts = append(prompts, request)
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Len(t, prompts, 4)
	require.Equal(t, "Image provider", prompts[2].Label)
	require.Equal(t, []string{"openclip", "jina-clip", "openai", CustomImageProviderOptionLabel}, prompts[2].Options)
	require.Equal(t, "jina-clip", prompts[2].DefaultValue)
	require.Equal(t, "Image model", prompts[3].Label)
	require.Equal(t, []string{"jinaai/jina-clip-v2", CustomModelOptionLabel}, prompts[3].Options)
	require.Equal(t, "jinaai/jina-clip-v2", prompts[3].DefaultValue)
}

func TestRunConfigCommandPersistsCustomImageDimensionsAndVisionModel(t *testing.T) {
	input := bytes.NewBufferString("\n\n768\nllava-v1.6\n")
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
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			switch request.Label {
			case "Image provider":
				return "openclip", nil
			case "Image model":
				return "ViT-L-14", nil
			default:
				return request.DefaultValue, nil
			}
		},
	})
	require.NoError(t, err)
	require.Equal(t, "openclip", saved.Image.Provider)
	require.Equal(t, "ViT-L-14", saved.Image.Model)
	require.Equal(t, 768, saved.Image.Dimensions)
	require.Equal(t, "llava-v1.6", saved.Image.VisionModel)
}

func TestRunConfigCommandAcceptsCustomImageProviderInput(t *testing.T) {
	input := bytes.NewBufferString("\n\nmy-clip\n\n\n")
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
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error {
			saved = cfg
			return nil
		},
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			switch request.Label {
			case "Image provider":
				return CustomImageProviderOptionLabel, nil
			case "Image model":
				return request.DefaultValue, nil
			default:
				return request.DefaultValue, nil
			}
		},
	})
	require.NoError(t, err)
	require.Equal(t, "my-clip", saved.Image.Provider)
	require.Contains(t, output.String(), "Custom image provider")
}

func TestRunConfigCommandRejectsInvalidImageDimension(t *testing.T) {
	input := bytes.NewBufferString("\n\nabc\n0\n768\nllava\n")
	output := &bytes.Buffer{}

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
			Image: defaultImageSettings(),
		},
		Save: func(cfg config.PersistedConfig) error { return nil },
		IsTerminal: func(reader io.Reader) bool {
			return true
		},
		Select: func(request ConfigSelectRequest) (string, error) {
			return request.DefaultValue, nil
		},
	})
	require.NoError(t, err)
	require.Contains(t, output.String(), "image vector dimension must be a positive integer")
}

func TestSupportedImageProvidersAndRecommendedDimensions(t *testing.T) {
	require.Equal(t, []string{"openclip", "jina-clip", "openai"}, supportedImageProviders())
	require.Equal(t, []string{"ViT-B-32", "ViT-L-14"}, supportedImageModels("openclip"))
	require.Equal(t, []string{"jinaai/jina-clip-v2"}, supportedImageModels("jina-clip"))
	require.Equal(t, []string{"text-embedding-3-small", "text-embedding-3-large"}, supportedImageModels("openai"))

	require.Equal(t, 512, recommendedImageDimensions("openclip", "ViT-B-32"))
	require.Equal(t, 768, recommendedImageDimensions("openclip", "ViT-L-14"))
	require.Equal(t, 1024, recommendedImageDimensions("jina-clip", "jinaai/jina-clip-v2"))
	require.Equal(t, 1536, recommendedImageDimensions("openai", "text-embedding-3-small"))
	require.Equal(t, 3072, recommendedImageDimensions("openai", "text-embedding-3-large"))
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
