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

	envDataDir         = "JCEMB_DATA_DIR"
	envProvider        = "JCEMB_PROVIDER"
	envModel           = "JCEMB_MODEL"
	envVectorDim       = "JCEMB_VECTOR_DIM"
	envOllamaBatchSize = "JCEMB_OLLAMA_BATCH_SIZE"
	envOllamaTimeout   = "JCEMB_OLLAMA_TIMEOUT"
	envOllamaURL       = "OLLAMA_HOST"
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
	if strings.TrimSpace(provider) != DefaultProviderName {
		return nil
	}

	return map[string]string{
		"ollama_url": s.Ollama.URL,
		"batch_size": strconv.Itoa(s.Ollama.BatchSize),
		"timeout":    s.Ollama.Timeout.String(),
	}
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

	return resolved
}
