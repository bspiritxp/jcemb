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
	DataDir      string                      `json:"data_dir"`
	Provider     string                      `json:"provider"`
	Model        string                      `json:"model"`
	VectorDim    int                         `json:"vector_dim"`
	Ollama       PersistedOllamaConfig       `json:"ollama"`
	OpenAI       PersistedOpenAIConfig       `json:"openai"`
	Image        PersistedImageConfig        `json:"image"`
	TagExtractor PersistedTagExtractorConfig `json:"tag_extractor"`
}

type PersistedOllamaConfig struct {
	URL       string `json:"url"`
	BatchSize int    `json:"batch_size"`
	Timeout   string `json:"timeout"`
}

type PersistedOpenAIConfig struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key,omitempty"`
	BatchSize  int    `json:"batch_size"`
	Timeout    string `json:"timeout"`
	Dimensions int    `json:"dimensions"`
	InputType  string `json:"input_type,omitempty"`
}

type PersistedImageConfig struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Pretrained  string `json:"pretrained"`
	Dimensions  int    `json:"dimensions"`
	Device      string `json:"device"`
	Python      string `json:"python"`
	VisionModel string `json:"vision_model"`
}

type PersistedTagExtractorConfig struct {
	Enabled          bool              `json:"enabled"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model"`
	MaxTags          int               `json:"max_tags"`
	MinTagLen        int               `json:"min_tag_len"`
	MaxTagLen        int               `json:"max_tag_len"`
	SkipIfHasYAML    bool              `json:"skip_if_has_yaml"`
	Timeout          string            `json:"timeout"`
	Options          map[string]string `json:"options,omitempty"`
	present          bool
	enabledSet       bool
	providerSet      bool
	modelSet         bool
	maxTagsSet       bool
	minTagLenSet     bool
	maxTagLenSet     bool
	skipIfHasYAMLSet bool
	timeoutSet       bool
	optionsSet       bool
}

type fileConfig struct {
	DataDir      *string                 `json:"data_dir,omitempty"`
	Provider     *string                 `json:"provider,omitempty"`
	Model        *string                 `json:"model,omitempty"`
	VectorDim    *int                    `json:"vector_dim,omitempty"`
	Ollama       *fileOllamaConfig       `json:"ollama,omitempty"`
	OpenAI       *fileOpenAIConfig       `json:"openai,omitempty"`
	Image        *fileImageConfig        `json:"image,omitempty"`
	TagExtractor *fileTagExtractorConfig `json:"tag_extractor,omitempty"`
}

type fileOllamaConfig struct {
	URL       *string `json:"url,omitempty"`
	BatchSize *int    `json:"batch_size,omitempty"`
	Timeout   *string `json:"timeout,omitempty"`
}

type fileOpenAIConfig struct {
	BaseURL    *string `json:"base_url,omitempty"`
	APIKey     *string `json:"api_key,omitempty"`
	BatchSize  *int    `json:"batch_size,omitempty"`
	Timeout    *string `json:"timeout,omitempty"`
	Dimensions *int    `json:"dimensions,omitempty"`
	InputType  *string `json:"input_type,omitempty"`
}

type fileImageConfig struct {
	Provider    *string `json:"provider,omitempty"`
	Model       *string `json:"model,omitempty"`
	Pretrained  *string `json:"pretrained,omitempty"`
	Dimensions  *int    `json:"dimensions,omitempty"`
	Device      *string `json:"device,omitempty"`
	Python      *string `json:"python,omitempty"`
	VisionModel *string `json:"vision_model,omitempty"`
}

type fileTagExtractorConfig struct {
	Enabled       *bool             `json:"enabled,omitempty"`
	Provider      *string           `json:"provider,omitempty"`
	Model         *string           `json:"model,omitempty"`
	MaxTags       *int              `json:"max_tags,omitempty"`
	MinTagLen     *int              `json:"min_tag_len,omitempty"`
	MaxTagLen     *int              `json:"max_tag_len,omitempty"`
	SkipIfHasYAML *bool             `json:"skip_if_has_yaml,omitempty"`
	Timeout       *string           `json:"timeout,omitempty"`
	Options       map[string]string `json:"options,omitempty"`
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

func PersistedFromSettings(settings Settings) PersistedConfig {
	return persistedFromSettings(settings)
}

func (c PersistedConfig) Settings() (Settings, error) {
	defaults := DefaultSettings()
	timeout, err := time.ParseDuration(strings.TrimSpace(c.Ollama.Timeout))
	if err != nil {
		return Settings{}, fmt.Errorf("%w: ollama.timeout must be a valid duration: %v", ErrInvalidConfig, err)
	}
	openAITimeout := defaults.OpenAI.Timeout
	if strings.TrimSpace(c.OpenAI.Timeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(c.OpenAI.Timeout))
		if err != nil {
			return Settings{}, fmt.Errorf("%w: openai.timeout must be a valid duration: %v", ErrInvalidConfig, err)
		}
		openAITimeout = parsed
	}
	tagExtractorSettings, err := c.TagExtractor.settings(defaults.TagExtractor)
	if err != nil {
		return Settings{}, err
	}

	dataDir, err := resolveConfiguredDataDir(c.DataDir)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: data_dir: %v", ErrInvalidConfig, err)
	}

	imageSettings := defaults.Image
	if strings.TrimSpace(c.Image.Provider) != "" {
		imageSettings.Provider = strings.TrimSpace(c.Image.Provider)
	}
	if strings.TrimSpace(c.Image.Model) != "" {
		imageSettings.Model = strings.TrimSpace(c.Image.Model)
	}
	if strings.TrimSpace(c.Image.Pretrained) != "" {
		imageSettings.Pretrained = strings.TrimSpace(c.Image.Pretrained)
	}
	if c.Image.Dimensions > 0 {
		imageSettings.Dimensions = c.Image.Dimensions
	}
	if strings.TrimSpace(c.Image.Device) != "" {
		imageSettings.Device = strings.TrimSpace(c.Image.Device)
	}
	if strings.TrimSpace(c.Image.Python) != "" {
		imageSettings.Python = strings.TrimSpace(c.Image.Python)
	}
	if strings.TrimSpace(c.Image.VisionModel) != "" {
		imageSettings.VisionModel = strings.TrimSpace(c.Image.VisionModel)
	}
	openAISettings := defaults.OpenAI
	if strings.TrimSpace(c.OpenAI.BaseURL) != "" {
		openAISettings.BaseURL = strings.TrimRight(strings.TrimSpace(c.OpenAI.BaseURL), "/")
	}
	if strings.TrimSpace(c.OpenAI.APIKey) != "" {
		openAISettings.APIKey = strings.TrimSpace(c.OpenAI.APIKey)
	}
	if c.OpenAI.BatchSize > 0 {
		openAISettings.BatchSize = c.OpenAI.BatchSize
	}
	if c.OpenAI.Dimensions > 0 {
		openAISettings.Dimensions = c.OpenAI.Dimensions
	}
	if strings.TrimSpace(c.OpenAI.InputType) != "" {
		openAISettings.InputType = strings.TrimSpace(c.OpenAI.InputType)
	}
	openAISettings.Timeout = openAITimeout
	if tagExtractorSettings.Options == nil {
		tagExtractorSettings.Options = map[string]string{}
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
		OpenAI:       openAISettings,
		Image:        imageSettings,
		TagExtractor: tagExtractorSettings,
	}
	applyProviderDefaults(&settings)

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
	if patch.OpenAI != nil {
		if patch.OpenAI.BaseURL != nil {
			resolved.OpenAI.BaseURL = strings.TrimRight(strings.TrimSpace(*patch.OpenAI.BaseURL), "/")
		}
		if patch.OpenAI.APIKey != nil {
			resolved.OpenAI.APIKey = strings.TrimSpace(*patch.OpenAI.APIKey)
		}
		if patch.OpenAI.BatchSize != nil {
			resolved.OpenAI.BatchSize = *patch.OpenAI.BatchSize
		}
		if patch.OpenAI.Timeout != nil {
			timeout, err := time.ParseDuration(strings.TrimSpace(*patch.OpenAI.Timeout))
			if err != nil {
				return Settings{}, fmt.Errorf("%w: openai.timeout must be a valid duration: %v", ErrInvalidConfig, err)
			}
			resolved.OpenAI.Timeout = timeout
		}
		if patch.OpenAI.Dimensions != nil {
			resolved.OpenAI.Dimensions = *patch.OpenAI.Dimensions
		}
		if patch.OpenAI.InputType != nil {
			resolved.OpenAI.InputType = strings.TrimSpace(*patch.OpenAI.InputType)
		}
	}
	if patch.Image != nil {
		if patch.Image.Provider != nil {
			resolved.Image.Provider = strings.TrimSpace(*patch.Image.Provider)
		}
		if patch.Image.Model != nil {
			resolved.Image.Model = strings.TrimSpace(*patch.Image.Model)
		}
		if patch.Image.Pretrained != nil {
			resolved.Image.Pretrained = strings.TrimSpace(*patch.Image.Pretrained)
		}
		if patch.Image.Dimensions != nil {
			resolved.Image.Dimensions = *patch.Image.Dimensions
		}
		if patch.Image.Device != nil {
			resolved.Image.Device = strings.TrimSpace(*patch.Image.Device)
		}
		if patch.Image.Python != nil {
			resolved.Image.Python = strings.TrimSpace(*patch.Image.Python)
		}
		if patch.Image.VisionModel != nil {
			resolved.Image.VisionModel = strings.TrimSpace(*patch.Image.VisionModel)
		}
	}
	if patch.TagExtractor != nil {
		if patch.TagExtractor.Enabled != nil {
			resolved.TagExtractor.Enabled = *patch.TagExtractor.Enabled
		}
		if patch.TagExtractor.Provider != nil {
			resolved.TagExtractor.Provider = strings.TrimSpace(*patch.TagExtractor.Provider)
		}
		if patch.TagExtractor.Model != nil {
			resolved.TagExtractor.Model = strings.TrimSpace(*patch.TagExtractor.Model)
		}
		if patch.TagExtractor.MaxTags != nil {
			resolved.TagExtractor.MaxTags = *patch.TagExtractor.MaxTags
		}
		if patch.TagExtractor.MinTagLen != nil {
			resolved.TagExtractor.MinTagLen = *patch.TagExtractor.MinTagLen
		}
		if patch.TagExtractor.MaxTagLen != nil {
			resolved.TagExtractor.MaxTagLen = *patch.TagExtractor.MaxTagLen
		}
		if patch.TagExtractor.SkipIfHasYAML != nil {
			resolved.TagExtractor.SkipIfHasYAML = *patch.TagExtractor.SkipIfHasYAML
		}
		if patch.TagExtractor.Timeout != nil {
			timeout, err := time.ParseDuration(strings.TrimSpace(*patch.TagExtractor.Timeout))
			if err != nil {
				return Settings{}, fmt.Errorf("%w: tag_extractor.timeout must be a valid duration: %v", ErrInvalidConfig, err)
			}
			resolved.TagExtractor.Timeout = timeout
		}
		if patch.TagExtractor.Options != nil {
			resolved.TagExtractor.Options = cloneStringMap(patch.TagExtractor.Options)
		}
	}
	applyProviderDefaults(&resolved)

	if err := validateSettings(resolved); err != nil {
		return Settings{}, err
	}
	return resolved, nil
}

func applyProviderDefaults(settings *Settings) {
	if strings.TrimSpace(settings.Provider) == OpenAIProviderName {
		if strings.TrimSpace(settings.Model) == "" || settings.Model == DefaultModelName {
			settings.Model = OpenAIDefaultModel
		}
		if settings.VectorDim == DefaultVectorDim {
			settings.VectorDim = OpenAIDefaultDim
		}
	}
	if strings.TrimSpace(settings.Image.Provider) == OpenAIProviderName {
		if strings.TrimSpace(settings.Image.Model) == "" || settings.Image.Model == "ViT-B-32" {
			settings.Image.Model = OpenAIDefaultModel
		}
		if settings.Image.Dimensions == 512 {
			settings.Image.Dimensions = settings.OpenAI.Dimensions
		}
	}
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
	if strings.TrimSpace(settings.OpenAI.BaseURL) == "" {
		return fmt.Errorf("%w: openai.base_url is required", ErrInvalidConfig)
	}
	if settings.OpenAI.BatchSize <= 0 {
		return fmt.Errorf("%w: openai.batch_size must be > 0", ErrInvalidConfig)
	}
	if settings.OpenAI.Timeout <= 0 {
		return fmt.Errorf("%w: openai.timeout must be > 0", ErrInvalidConfig)
	}
	if settings.OpenAI.Dimensions <= 0 {
		return fmt.Errorf("%w: openai.dimensions must be > 0", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Provider) == OpenAIProviderName && strings.TrimSpace(settings.OpenAI.APIKey) == "" {
		return fmt.Errorf("%w: openai.api_key is required when provider is openai", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Image.Provider) == "" {
		return fmt.Errorf("%w: image.provider is required", ErrInvalidConfig)
	}
	switch strings.TrimSpace(settings.Image.Provider) {
	case "openclip":
		if strings.TrimSpace(settings.Image.Model) == "" {
			return fmt.Errorf("%w: image.model is required", ErrInvalidConfig)
		}
		if strings.TrimSpace(settings.Image.Pretrained) == "" {
			return fmt.Errorf("%w: image.pretrained is required for openclip", ErrInvalidConfig)
		}
	case "jina-clip", "jina":
		if strings.TrimSpace(settings.Image.Model) == "" {
			return fmt.Errorf("%w: image.model is required", ErrInvalidConfig)
		}
	case "openai":
		if strings.TrimSpace(settings.Image.Model) == "" {
			return fmt.Errorf("%w: image.model is required", ErrInvalidConfig)
		}
		if strings.TrimSpace(settings.OpenAI.APIKey) == "" {
			return fmt.Errorf("%w: openai.api_key is required when image.provider is openai", ErrInvalidConfig)
		}
	default:
		return fmt.Errorf("%w: image.provider must be openclip, jina-clip, or openai", ErrInvalidConfig)
	}
	if settings.Image.Dimensions <= 0 {
		return fmt.Errorf("%w: image.dimensions must be > 0", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Image.Device) == "" {
		return fmt.Errorf("%w: image.device is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Image.Python) == "" {
		return fmt.Errorf("%w: image.python is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(settings.Image.VisionModel) == "" {
		return fmt.Errorf("%w: image.vision_model is required", ErrInvalidConfig)
	}
	if settings.TagExtractor.Enabled {
		if strings.TrimSpace(settings.TagExtractor.Provider) == "" {
			return fmt.Errorf("%w: tag_extractor.provider is required when enabled", ErrInvalidConfig)
		}
		if strings.TrimSpace(settings.TagExtractor.Model) == "" {
			return fmt.Errorf("%w: tag_extractor.model is required when enabled", ErrInvalidConfig)
		}
	}
	if settings.TagExtractor.MaxTags < 1 {
		return fmt.Errorf("%w: tag_extractor.max_tags must be >= 1", ErrInvalidConfig)
	}
	if settings.TagExtractor.MinTagLen > settings.TagExtractor.MaxTagLen {
		return fmt.Errorf("%w: tag_extractor.min_tag_len must be <= tag_extractor.max_tag_len", ErrInvalidConfig)
	}
	if settings.TagExtractor.Timeout <= 0 {
		return fmt.Errorf("%w: tag_extractor.timeout must be > 0", ErrInvalidConfig)
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
		OpenAI: PersistedOpenAIConfig{
			BaseURL:    settings.OpenAI.BaseURL,
			APIKey:     settings.OpenAI.APIKey,
			BatchSize:  settings.OpenAI.BatchSize,
			Timeout:    settings.OpenAI.Timeout.String(),
			Dimensions: settings.OpenAI.Dimensions,
			InputType:  settings.OpenAI.InputType,
		},
		Image: PersistedImageConfig{
			Provider:    settings.Image.Provider,
			Model:       settings.Image.Model,
			Pretrained:  settings.Image.Pretrained,
			Dimensions:  settings.Image.Dimensions,
			Device:      settings.Image.Device,
			Python:      settings.Image.Python,
			VisionModel: settings.Image.VisionModel,
		},
		TagExtractor: PersistedTagExtractorConfig{
			Enabled:       settings.TagExtractor.Enabled,
			Provider:      settings.TagExtractor.Provider,
			Model:         settings.TagExtractor.Model,
			MaxTags:       settings.TagExtractor.MaxTags,
			MinTagLen:     settings.TagExtractor.MinTagLen,
			MaxTagLen:     settings.TagExtractor.MaxTagLen,
			SkipIfHasYAML: settings.TagExtractor.SkipIfHasYAML,
			Timeout:       settings.TagExtractor.Timeout.String(),
			Options:       cloneStringMap(settings.TagExtractor.Options),
		},
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (cfg *PersistedTagExtractorConfig) UnmarshalJSON(data []byte) error {
	type persistedTagExtractorJSON struct {
		Enabled       *bool              `json:"enabled"`
		Provider      *string            `json:"provider"`
		Model         *string            `json:"model"`
		MaxTags       *int               `json:"max_tags"`
		MinTagLen     *int               `json:"min_tag_len"`
		MaxTagLen     *int               `json:"max_tag_len"`
		SkipIfHasYAML *bool              `json:"skip_if_has_yaml"`
		Timeout       *string            `json:"timeout"`
		Options       *map[string]string `json:"options,omitempty"`
	}

	var decoded persistedTagExtractorJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*cfg = PersistedTagExtractorConfig{present: true}
	if decoded.Enabled != nil {
		cfg.Enabled = *decoded.Enabled
		cfg.enabledSet = true
	}
	if decoded.Provider != nil {
		cfg.Provider = *decoded.Provider
		cfg.providerSet = true
	}
	if decoded.Model != nil {
		cfg.Model = *decoded.Model
		cfg.modelSet = true
	}
	if decoded.MaxTags != nil {
		cfg.MaxTags = *decoded.MaxTags
		cfg.maxTagsSet = true
	}
	if decoded.MinTagLen != nil {
		cfg.MinTagLen = *decoded.MinTagLen
		cfg.minTagLenSet = true
	}
	if decoded.MaxTagLen != nil {
		cfg.MaxTagLen = *decoded.MaxTagLen
		cfg.maxTagLenSet = true
	}
	if decoded.SkipIfHasYAML != nil {
		cfg.SkipIfHasYAML = *decoded.SkipIfHasYAML
		cfg.skipIfHasYAMLSet = true
	}
	if decoded.Timeout != nil {
		cfg.Timeout = *decoded.Timeout
		cfg.timeoutSet = true
	}
	if decoded.Options != nil {
		cfg.Options = cloneStringMap(*decoded.Options)
		cfg.optionsSet = true
	}
	return nil
}

func (cfg PersistedTagExtractorConfig) settings(defaults TagExtractorConfig) (TagExtractorConfig, error) {
	settings := defaults
	if cfg.hasSetFlags() {
		if cfg.enabledSet {
			settings.Enabled = cfg.Enabled
		}
		if cfg.providerSet {
			settings.Provider = strings.TrimSpace(cfg.Provider)
		}
		if cfg.modelSet {
			settings.Model = strings.TrimSpace(cfg.Model)
		}
		if cfg.maxTagsSet {
			settings.MaxTags = cfg.MaxTags
		}
		if cfg.minTagLenSet {
			settings.MinTagLen = cfg.MinTagLen
		}
		if cfg.maxTagLenSet {
			settings.MaxTagLen = cfg.MaxTagLen
		}
		if cfg.skipIfHasYAMLSet {
			settings.SkipIfHasYAML = cfg.SkipIfHasYAML
		}
		if cfg.timeoutSet {
			if strings.TrimSpace(cfg.Timeout) == "" {
				settings.Timeout = 0
			} else {
				parsed, err := time.ParseDuration(strings.TrimSpace(cfg.Timeout))
				if err != nil {
					return TagExtractorConfig{}, fmt.Errorf("%w: tag_extractor.timeout must be a valid duration: %v", ErrInvalidConfig, err)
				}
				settings.Timeout = parsed
			}
		}
		if cfg.optionsSet {
			settings.Options = cloneStringMap(cfg.Options)
		}
		if settings.Options == nil {
			settings.Options = map[string]string{}
		}
		return settings, nil
	}

	if isZeroPersistedTagExtractorConfig(cfg) {
		if settings.Options == nil {
			settings.Options = map[string]string{}
		}
		return settings, nil
	}

	settings.Enabled = cfg.Enabled
	settings.Provider = strings.TrimSpace(cfg.Provider)
	settings.Model = strings.TrimSpace(cfg.Model)
	settings.MaxTags = cfg.MaxTags
	settings.MinTagLen = cfg.MinTagLen
	settings.MaxTagLen = cfg.MaxTagLen
	settings.SkipIfHasYAML = cfg.SkipIfHasYAML
	if strings.TrimSpace(cfg.Timeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(cfg.Timeout))
		if err != nil {
			return TagExtractorConfig{}, fmt.Errorf("%w: tag_extractor.timeout must be a valid duration: %v", ErrInvalidConfig, err)
		}
		settings.Timeout = parsed
	}
	settings.Options = cloneStringMap(cfg.Options)
	if settings.Options == nil {
		settings.Options = map[string]string{}
	}
	return settings, nil
}

func (cfg PersistedTagExtractorConfig) hasSetFlags() bool {
	return cfg.present || cfg.enabledSet || cfg.providerSet || cfg.modelSet || cfg.maxTagsSet || cfg.minTagLenSet || cfg.maxTagLenSet || cfg.skipIfHasYAMLSet || cfg.timeoutSet || cfg.optionsSet
}

func isZeroPersistedTagExtractorConfig(cfg PersistedTagExtractorConfig) bool {
	return !cfg.Enabled &&
		strings.TrimSpace(cfg.Provider) == "" &&
		strings.TrimSpace(cfg.Model) == "" &&
		cfg.MaxTags == 0 &&
		cfg.MinTagLen == 0 &&
		cfg.MaxTagLen == 0 &&
		!cfg.SkipIfHasYAML &&
		strings.TrimSpace(cfg.Timeout) == "" &&
		len(cfg.Options) == 0
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
