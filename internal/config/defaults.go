package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
)

const (
	DefaultProviderName = "ollama"
	DefaultModelName    = "bge-m3"
	DefaultVectorDim    = 1024
	OpenAIProviderName  = "openai"
	OpenAIDefaultModel  = "text-embedding-3-small"
	OpenAIDefaultDim    = 1536

	envDataDir          = "JCEMB_DATA_DIR"
	envProvider         = "JCEMB_PROVIDER"
	envModel            = "JCEMB_MODEL"
	envVectorDim        = "JCEMB_VECTOR_DIM"
	envOllamaBatchSize  = "JCEMB_OLLAMA_BATCH_SIZE"
	envOllamaTimeout    = "JCEMB_OLLAMA_TIMEOUT"
	envOllamaURL        = "OLLAMA_HOST"
	envImageProvider    = "JCEMB_IMAGE_PROVIDER"
	envImageModel       = "JCEMB_IMAGE_MODEL"
	envImagePretrained  = "JCEMB_IMAGE_PRETRAINED"
	envImageDimensions  = "JCEMB_IMAGE_DIMENSIONS"
	envImageDevice      = "JCEMB_IMAGE_DEVICE"
	envImagePython      = "JCEMB_IMAGE_PYTHON"
	envImageVision      = "JCEMB_IMAGE_VISION_MODEL"
	envOpenAIBaseURL    = "OPENAI_BASE_URL"
	envOpenAIAPIKey     = "OPENAI_API_KEY"
	envOpenAITimeout    = "JCEMB_OPENAI_TIMEOUT"
	envOpenAIBatchSize  = "JCEMB_OPENAI_BATCH_SIZE"
	envOpenAIDimensions = "JCEMB_OPENAI_DIMENSIONS"
	envOpenAIInputType  = "JCEMB_OPENAI_INPUT_TYPE"
)

type DefaultsConfig struct {
	AppName        string
	DefaultPath    string
	IntegrationTag string
	IntegrationEnv string
	ConfigFile     string
	DataRoot       string
	Global         Settings
	Ollama         OllamaDefaultsConfig
}

type Settings struct {
	DataDir   string
	Provider  string
	Model     string
	VectorDim int
	Ollama    OllamaConfig
	OpenAI    OpenAIConfig
	Image     ImageConfig
}

type OllamaConfig struct {
	URL       string
	BatchSize int
	Timeout   time.Duration
}

type OllamaDefaultsConfig struct {
	URL       string
	BatchSize int
	Timeout   time.Duration
}

type OpenAIConfig struct {
	BaseURL    string
	APIKey     string
	BatchSize  int
	Timeout    time.Duration
	Dimensions int
	InputType  string
}

type ImageConfig struct {
	Provider    string
	Model       string
	Pretrained  string
	Dimensions  int
	Device      string
	Python      string
	VisionModel string
}

func Defaults() DefaultsConfig {
	appPaths, _ := jcpaths.ResolveAppPaths()
	builtIn := builtInSettings(appPaths.DataRoot)
	effective := applyEnvOverrides(builtIn)

	return DefaultsConfig{
		AppName:        "jcemb",
		DefaultPath:    ".",
		IntegrationTag: "integration",
		IntegrationEnv: "INTEGRATION",
		ConfigFile:     appPaths.ConfigFile,
		DataRoot:       appPaths.DataRoot,
		Global:         effective,
		Ollama: OllamaDefaultsConfig{
			URL:       effective.Ollama.URL,
			BatchSize: effective.Ollama.BatchSize,
			Timeout:   effective.Ollama.Timeout,
		},
	}
}

func DefaultSettings() Settings {
	defaults := Defaults()
	return defaults.Global
}

func (s Settings) ProviderOptions(provider string) map[string]string {
	options := map[string]string{
		"image_provider":   s.Image.Provider,
		"image_model":      s.Image.Model,
		"image_pretrained": s.Image.Pretrained,
		"image_dimensions": strconv.Itoa(s.Image.Dimensions),
		"image_device":     s.Image.Device,
		"image_python":     s.Image.Python,
		"vision_model":     s.Image.VisionModel,
	}
	if strings.TrimSpace(provider) == DefaultProviderName {
		options["ollama_url"] = s.Ollama.URL
		options["batch_size"] = strconv.Itoa(s.Ollama.BatchSize)
		options["timeout"] = s.Ollama.Timeout.String()
	}
	if strings.TrimSpace(provider) == OpenAIProviderName || strings.TrimSpace(s.Image.Provider) == OpenAIProviderName {
		options["openai_base_url"] = s.OpenAI.BaseURL
		options["openai_api_key"] = s.OpenAI.APIKey
		options["openai_batch_size"] = strconv.Itoa(s.OpenAI.BatchSize)
		options["openai_timeout"] = s.OpenAI.Timeout.String()
		options["openai_dimensions"] = strconv.Itoa(s.OpenAI.Dimensions)
		if value := strings.TrimSpace(s.OpenAI.InputType); value != "" {
			options["openai_input_type"] = value
		}
	}
	return options
}

func builtInSettings(dataRoot string) Settings {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = filepath.Join(".local", "share", "jcemb")
	}

	return Settings{
		DataDir:   dataRoot,
		Provider:  DefaultProviderName,
		Model:     DefaultModelName,
		VectorDim: DefaultVectorDim,
		Ollama: OllamaConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   30 * time.Second,
		},
		OpenAI: OpenAIConfig{
			BaseURL:    "https://api.openai.com/v1",
			BatchSize:  128,
			Timeout:    60 * time.Second,
			Dimensions: OpenAIDefaultDim,
		},
		Image: ImageConfig{
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

func applyEnvOverrides(base Settings) Settings {
	resolved := base

	if value := strings.TrimSpace(os.Getenv(envDataDir)); value != "" {
		resolved.DataDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(os.Getenv(envProvider)); value != "" {
		resolved.Provider = value
	}
	if value := strings.TrimSpace(os.Getenv(envModel)); value != "" {
		resolved.Model = value
	}
	if value := strings.TrimSpace(os.Getenv(envVectorDim)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			resolved.VectorDim = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envOllamaURL)); value != "" {
		resolved.Ollama.URL = strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(os.Getenv(envOllamaBatchSize)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			resolved.Ollama.BatchSize = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envOllamaTimeout)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			resolved.Ollama.Timeout = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envImageProvider)); value != "" {
		resolved.Image.Provider = value
	}
	if value := strings.TrimSpace(os.Getenv(envImageModel)); value != "" {
		resolved.Image.Model = value
	}
	if value := strings.TrimSpace(os.Getenv(envImagePretrained)); value != "" {
		resolved.Image.Pretrained = value
	}
	if value := strings.TrimSpace(os.Getenv(envImageDimensions)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			resolved.Image.Dimensions = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envImageDevice)); value != "" {
		resolved.Image.Device = value
	}
	if value := strings.TrimSpace(os.Getenv(envImagePython)); value != "" {
		resolved.Image.Python = value
	}
	if value := strings.TrimSpace(os.Getenv(envImageVision)); value != "" {
		resolved.Image.VisionModel = value
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAIBaseURL)); value != "" {
		resolved.OpenAI.BaseURL = strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAIAPIKey)); value != "" {
		resolved.OpenAI.APIKey = value
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAIBatchSize)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			resolved.OpenAI.BatchSize = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAITimeout)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			resolved.OpenAI.Timeout = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAIDimensions)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			resolved.OpenAI.Dimensions = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv(envOpenAIInputType)); value != "" {
		resolved.OpenAI.InputType = value
	}
	applyProviderDefaults(&resolved)

	return resolved
}
