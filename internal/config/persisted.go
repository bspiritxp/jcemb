package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
)

var (
	ErrConfigNotFound = errors.New("config: file not found")
	ErrInvalidConfig  = errors.New("config: invalid config")
)

type RuntimeConfig struct {
	Path           string
	Settings       Settings
	LoadedFromFile bool
}

type PersistedConfig struct {
	DataDir   string                `json:"data_dir"`
	Provider  string                `json:"provider"`
	Model     string                `json:"model"`
	VectorDim int                   `json:"vector_dim"`
	Ollama    PersistedOllamaConfig `json:"ollama"`
}

type PersistedOllamaConfig struct {
	URL       string `json:"url"`
	BatchSize int    `json:"batch_size"`
	Timeout   string `json:"timeout"`
}

type fileConfig struct {
	DataDir   *string           `json:"data_dir,omitempty"`
	Provider  *string           `json:"provider,omitempty"`
	Model     *string           `json:"model,omitempty"`
	VectorDim *int              `json:"vector_dim,omitempty"`
	Ollama    *fileOllamaConfig `json:"ollama,omitempty"`
}

type fileOllamaConfig struct {
	URL       *string `json:"url,omitempty"`
	BatchSize *int    `json:"batch_size,omitempty"`
	Timeout   *string `json:"timeout,omitempty"`
}

func Load() (RuntimeConfig, error) {
	appPaths, err := jcpaths.ResolveAppPaths()
	if err != nil {
		return RuntimeConfig{}, err
	}
	return LoadFromPath(appPaths.ConfigFile)
}

func LoadFromPath(path string) (RuntimeConfig, error) {
	defaults := DefaultSettings()
	runtime := RuntimeConfig{Path: path, Settings: defaults}

	patch, err := readFileConfig(path)
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			return runtime, nil
		}
		return RuntimeConfig{}, err
	}

	runtime.Settings, err = mergeFileConfig(defaults, patch)
	if err != nil {
		return RuntimeConfig{}, err
	}
	runtime.LoadedFromFile = true
	return runtime, nil
}

func Save(config PersistedConfig) error {
	appPaths, err := jcpaths.ResolveAppPaths()
	if err != nil {
		return err
	}
	return SaveToPath(appPaths.ConfigFile, config)
}

func SaveToPath(path string, config PersistedConfig) error {
	settings, err := config.Settings()
	if err != nil {
		return err
	}

	payload := persistedFromSettings(settings)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create directory: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}

	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return fmt.Errorf("config: encode config: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFile.Name())
		return fmt.Errorf("config: close temp file: %w", err)
	}

	if err := os.Rename(tempFile.Name(), path); err != nil {
		_ = os.Remove(tempFile.Name())
		return fmt.Errorf("config: replace config file: %w", err)
	}

	return nil
}

func (c PersistedConfig) Settings() (Settings, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(c.Ollama.Timeout))
	if err != nil {
		return Settings{}, fmt.Errorf("%w: ollama.timeout must be a valid duration: %v", ErrInvalidConfig, err)
	}

	dataDir, err := resolveConfiguredDataDir(c.DataDir)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: data_dir: %v", ErrInvalidConfig, err)
	}

	settings := Settings{
		DataDir:   dataDir,
		Provider:  strings.TrimSpace(c.Provider),
		Model:     strings.TrimSpace(c.Model),
		VectorDim: c.VectorDim,
		Ollama: OllamaConfig{
			URL:       strings.TrimRight(strings.TrimSpace(c.Ollama.URL), "/"),
			BatchSize: c.Ollama.BatchSize,
			Timeout:   timeout,
		},
	}

	if err := validateSettings(settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func readFileConfig(path string) (fileConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, ErrConfigNotFound
		}
		return fileConfig{}, err
	}

	var cfg fileConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return fileConfig{}, fmt.Errorf("%w: %s: %v", ErrInvalidConfig, path, err)
	}
	return cfg, nil
}

func mergeFileConfig(base Settings, patch fileConfig) (Settings, error) {
	resolved := base

	if patch.DataDir != nil {
		dataDir, err := resolveConfiguredDataDir(*patch.DataDir)
		if err != nil {
			return Settings{}, fmt.Errorf("%w: data_dir: %v", ErrInvalidConfig, err)
		}
		resolved.DataDir = dataDir
	}
	if patch.Provider != nil {
		resolved.Provider = strings.TrimSpace(*patch.Provider)
	}
	if patch.Model != nil {
		resolved.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.VectorDim != nil {
		resolved.VectorDim = *patch.VectorDim
	}
	if patch.Ollama != nil {
		if patch.Ollama.URL != nil {
			resolved.Ollama.URL = strings.TrimRight(strings.TrimSpace(*patch.Ollama.URL), "/")
		}
		if patch.Ollama.BatchSize != nil {
			resolved.Ollama.BatchSize = *patch.Ollama.BatchSize
		}
		if patch.Ollama.Timeout != nil {
			timeout, err := time.ParseDuration(strings.TrimSpace(*patch.Ollama.Timeout))
			if err != nil {
				return Settings{}, fmt.Errorf("%w: ollama.timeout must be a valid duration: %v", ErrInvalidConfig, err)
			}
			resolved.Ollama.Timeout = timeout
		}
	}

	if err := validateSettings(resolved); err != nil {
		return Settings{}, err
	}
	return resolved, nil
}

func validateSettings(settings Settings) error {
	if strings.TrimSpace(settings.DataDir) == "" {
		return fmt.Errorf("%w: data_dir is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Provider) == "" {
		return fmt.Errorf("%w: provider is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Model) == "" {
		return fmt.Errorf("%w: model is required", ErrInvalidConfig)
	}
	if settings.VectorDim <= 0 {
		return fmt.Errorf("%w: vector_dim must be > 0", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Ollama.URL) == "" {
		return fmt.Errorf("%w: ollama.url is required", ErrInvalidConfig)
	}
	if settings.Ollama.BatchSize <= 0 {
		return fmt.Errorf("%w: ollama.batch_size must be > 0", ErrInvalidConfig)
	}
	if settings.Ollama.Timeout <= 0 {
		return fmt.Errorf("%w: ollama.timeout must be > 0", ErrInvalidConfig)
	}
	return nil
}

func persistedFromSettings(settings Settings) PersistedConfig {
	return PersistedConfig{
		DataDir:   settings.DataDir,
		Provider:  settings.Provider,
		Model:     settings.Model,
		VectorDim: settings.VectorDim,
		Ollama: PersistedOllamaConfig{
			URL:       settings.Ollama.URL,
			BatchSize: settings.Ollama.BatchSize,
			Timeout:   settings.Ollama.Timeout.String(),
		},
	}
}

func resolveConfiguredDataDir(value string) (string, error) {
	expanded, err := jcpaths.ExpandUserHome(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(expanded) == "" {
		return "", nil
	}
	return filepath.Clean(expanded), nil
}
