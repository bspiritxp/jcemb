package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/bspiritxp/jcemb/internal/config"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"golang.org/x/term"
)

var errConfigRequiresTTY = fmt.Errorf("config: interactive mode requires a terminal on stdin; run `jcemb config` in a terminal")

// CustomModelOptionLabel is the sentinel option appended to the model selection
// list. Choosing it prompts the user to type a custom model name.
const CustomModelOptionLabel = "Custom…"

// CustomImageProviderOptionLabel is the sentinel option appended to the image
// provider selection list. Choosing it prompts the user to type a custom image
// provider name (e.g. an out-of-tree provider).
const CustomImageProviderOptionLabel = "Custom…"

// ANSI escape sequences used to highlight the currently selected option.
const (
	ansiSelectedPrefix = "\x1b[1;36m" // bold cyan
	ansiReset          = "\x1b[0m"
)

type selectionAction int

const (
	selectionActionNone selectionAction = iota
	selectionActionUp
	selectionActionDown
	selectionActionConfirm
)

type ConfigCommandRequest struct {
	In         io.Reader
	Out        io.Writer
	ConfigPath string
	Settings   config.Settings
	Show       bool
	JSON       bool
	Updates    ConfigUpdates
	Save       func(config.PersistedConfig) error
	IsTerminal func(io.Reader) bool
	Select     func(ConfigSelectRequest) (string, error)
}

type ConfigUpdates struct {
	Provider                  *string
	Model                     *string
	DataDir                   *string
	VectorDim                 *int
	OllamaURL                 *string
	OllamaBatchSize           *int
	OllamaTimeout             *string
	OpenAIBaseURL             *string
	OpenAIAPIKey              *string
	OpenAIBatchSize           *int
	OpenAITimeout             *string
	OpenAIDim                 *int
	OpenAIInputType           *string
	ImageProvider             *string
	ImageModel                *string
	ImagePretrained           *string
	ImageDim                  *int
	ImageDevice               *string
	ImagePython               *string
	ImageVision               *string
	TagExtractorEnabled       *bool
	TagExtractorProvider      *string
	TagExtractorModel         *string
	TagExtractorMaxTags       *int
	TagExtractorSkipIfHasYAML *bool
}

type ConfigCommandResult struct {
	ConfigPath string
	Saved      config.PersistedConfig
}

type ConfigSelectRequest struct {
	In           io.Reader
	Reader       *bufio.Reader
	Out          io.Writer
	Label        string
	Options      []string
	DefaultValue string
}

func RunConfigCommand(request ConfigCommandRequest) (ConfigCommandResult, error) {
	if request.In == nil {
		request.In = os.Stdin
	}
	if request.Out == nil {
		request.Out = io.Discard
	}
	if request.IsTerminal == nil {
		request.IsTerminal = isTerminalReader
	}
	if request.Select == nil {
		request.Select = promptSelect
	}
	if strings.TrimSpace(request.ConfigPath) == "" {
		request.ConfigPath = config.Defaults().ConfigFile
	}
	if request.Save == nil {
		request.Save = func(cfg config.PersistedConfig) error {
			return config.SaveToPath(request.ConfigPath, cfg)
		}
	}
	if isZeroSettings(request.Settings) {
		request.Settings = config.DefaultSettings()
	}

	if hasConfigUpdates(request.Updates) {
		if request.Show {
			return ConfigCommandResult{}, fmt.Errorf("config: --show cannot be combined with --set-* options")
		}
		return runConfigUpdate(request)
	}
	if request.Show || request.JSON {
		if err := renderConfig(request.Out, request.ConfigPath, request.Settings, request.JSON); err != nil {
			return ConfigCommandResult{}, err
		}
		return ConfigCommandResult{ConfigPath: request.ConfigPath, Saved: config.PersistedFromSettings(request.Settings)}, nil
	}

	if !request.IsTerminal(request.In) {
		return ConfigCommandResult{}, errConfigRequiresTTY
	}

	current := request.Settings
	providerDefault := supportedProvider(current.Provider)
	modelDefault := supportedModel(current.Model)

	reader := bufio.NewReader(request.In)
	writer := request.Out

	_, _ = fmt.Fprintf(writer, "Config file: %s\n", request.ConfigPath)
	_, _ = fmt.Fprintln(writer, "Press Enter to keep the current value.")

	dataDir, err := promptLine(reader, writer, "Data directory", current.DataDir, func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("data directory is required")
		}
		return nil
	})
	if err != nil {
		return ConfigCommandResult{}, err
	}

	provider, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Provider",
		Options:      supportedProviders(),
		DefaultValue: providerDefault,
	})
	if err != nil {
		return ConfigCommandResult{}, err
	}

	modelOptions, modelDefaultValue := buildModelOptions(provider, current.Model)
	if modelDefaultValue == "" {
		modelDefaultValue = modelDefault
	}
	model, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Model",
		Options:      modelOptions,
		DefaultValue: modelDefaultValue,
	})
	if err != nil {
		return ConfigCommandResult{}, err
	}

	if model == CustomModelOptionLabel {
		customDefault := strings.TrimSpace(current.Model)
		if customDefault == "" || isStandardModel(provider, customDefault) {
			customDefault = ""
		}
		model, err = promptLine(reader, writer, "Custom model name", customDefault, func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("custom model name is required")
			}
			return nil
		})
		if err != nil {
			return ConfigCommandResult{}, err
		}
		model = strings.TrimSpace(model)
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Tag extraction:")

	tagExtractorProvider := provider
	tagExtractorModelOptions, tagExtractorModelDefault := buildTagExtractorModelOptions(tagExtractorProvider, current.TagExtractor.Model)
	tagExtractorModel, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Tag extraction model",
		Options:      tagExtractorModelOptions,
		DefaultValue: tagExtractorModelDefault,
	})
	if err != nil {
		return ConfigCommandResult{}, err
	}
	if tagExtractorModel == CustomModelOptionLabel {
		customDefault := strings.TrimSpace(current.TagExtractor.Model)
		if customDefault == "" || isStandardTagExtractorModel(tagExtractorProvider, customDefault) {
			customDefault = ""
		}
		tagExtractorModel, err = promptLine(reader, writer, "Custom tag extraction model name", customDefault, func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("custom tag extraction model name is required")
			}
			return nil
		})
		if err != nil {
			return ConfigCommandResult{}, err
		}
		tagExtractorModel = strings.TrimSpace(tagExtractorModel)
	}

	vectorDimValue, err := promptLine(reader, writer, "Vector dimension", strconv.Itoa(current.VectorDim), func(value string) error {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || parsed <= 0 {
			return fmt.Errorf("vector dimension must be a positive integer")
		}
		return nil
	})
	if err != nil {
		return ConfigCommandResult{}, err
	}

	vectorDim, _ := strconv.Atoi(strings.TrimSpace(vectorDimValue))

	openAISettings := current.OpenAI
	if provider == config.OpenAIProviderName {
		openAISettings, err = promptOpenAISettings(reader, writer, current.OpenAI, vectorDim)
		if err != nil {
			return ConfigCommandResult{}, err
		}
	}

	imageProvider, imageModel, imageDim, imageVisionModel, err := promptImageSettings(request, reader, writer, current.Image)
	if err != nil {
		return ConfigCommandResult{}, err
	}
	resolvedDataDir, err := jcpaths.ExpandUserHome(strings.TrimSpace(dataDir))
	if err != nil {
		return ConfigCommandResult{}, fmt.Errorf("config: data directory: %w", err)
	}
	saved := config.PersistedConfig{
		DataDir:   resolvedDataDir,
		Provider:  provider,
		Model:     model,
		VectorDim: vectorDim,
		Ollama: config.PersistedOllamaConfig{
			URL:       current.Ollama.URL,
			BatchSize: current.Ollama.BatchSize,
			Timeout:   current.Ollama.Timeout.String(),
		},
		OpenAI: config.PersistedOpenAIConfig{
			BaseURL:    openAISettings.BaseURL,
			APIKey:     openAISettings.APIKey,
			BatchSize:  openAISettings.BatchSize,
			Timeout:    openAISettings.Timeout.String(),
			Dimensions: openAISettings.Dimensions,
			InputType:  openAISettings.InputType,
		},
		Image: config.PersistedImageConfig{
			Provider:    imageProvider,
			Model:       imageModel,
			Pretrained:  current.Image.Pretrained,
			Dimensions:  imageDim,
			Device:      current.Image.Device,
			Python:      current.Image.Python,
			VisionModel: imageVisionModel,
		},
		TagExtractor: config.PersistedTagExtractorConfig{
			Enabled:       current.TagExtractor.Enabled,
			Provider:      tagExtractorProvider,
			Model:         tagExtractorModel,
			MaxTags:       current.TagExtractor.MaxTags,
			MinTagLen:     current.TagExtractor.MinTagLen,
			MaxTagLen:     current.TagExtractor.MaxTagLen,
			SkipIfHasYAML: current.TagExtractor.SkipIfHasYAML,
			Timeout:       current.TagExtractor.Timeout.String(),
			Options:       cloneOptions(current.TagExtractor.Options),
		},
	}

	if err := request.Save(saved); err != nil {
		return ConfigCommandResult{}, err
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Config saved.")
	_, _ = fmt.Fprintf(writer, "  data_dir: %s\n", saved.DataDir)
	_, _ = fmt.Fprintf(writer, "  provider: %s\n", saved.Provider)
	_, _ = fmt.Fprintf(writer, "  model: %s\n", saved.Model)
	_, _ = fmt.Fprintf(writer, "  vector_dim: %d\n", saved.VectorDim)
	_, _ = fmt.Fprintf(writer, "  image.provider: %s\n", saved.Image.Provider)
	_, _ = fmt.Fprintf(writer, "  image.model: %s\n", saved.Image.Model)
	_, _ = fmt.Fprintf(writer, "  image.dimensions: %d\n", saved.Image.Dimensions)
	_, _ = fmt.Fprintf(writer, "  image.vision_model: %s\n", saved.Image.VisionModel)
	_, _ = fmt.Fprintf(writer, "  tag_extractor.enabled: %t\n", saved.TagExtractor.Enabled)
	_, _ = fmt.Fprintf(writer, "  tag_extractor.provider: %s\n", saved.TagExtractor.Provider)
	_, _ = fmt.Fprintf(writer, "  tag_extractor.model: %s\n", saved.TagExtractor.Model)
	_, _ = fmt.Fprintf(writer, "  tag_extractor.max_tags: %d\n", saved.TagExtractor.MaxTags)

	return ConfigCommandResult{ConfigPath: request.ConfigPath, Saved: saved}, nil
}

func runConfigUpdate(request ConfigCommandRequest) (ConfigCommandResult, error) {
	settings, err := applyConfigUpdates(request.Settings, request.Updates)
	if err != nil {
		return ConfigCommandResult{}, err
	}
	saved := config.PersistedFromSettings(settings)
	if err := request.Save(saved); err != nil {
		return ConfigCommandResult{}, err
	}
	if err := renderConfig(request.Out, request.ConfigPath, settings, request.JSON); err != nil {
		return ConfigCommandResult{}, err
	}
	return ConfigCommandResult{ConfigPath: request.ConfigPath, Saved: saved}, nil
}

func applyConfigUpdates(current config.Settings, updates ConfigUpdates) (config.Settings, error) {
	saved := config.PersistedFromSettings(current)
	if updates.Provider != nil {
		saved.Provider = strings.TrimSpace(*updates.Provider)
	}
	if updates.Model != nil {
		saved.Model = strings.TrimSpace(*updates.Model)
	}
	if updates.DataDir != nil {
		saved.DataDir = strings.TrimSpace(*updates.DataDir)
	}
	if updates.VectorDim != nil {
		saved.VectorDim = *updates.VectorDim
	}
	if updates.OllamaURL != nil {
		saved.Ollama.URL = strings.TrimRight(strings.TrimSpace(*updates.OllamaURL), "/")
	}
	if updates.OllamaBatchSize != nil {
		saved.Ollama.BatchSize = *updates.OllamaBatchSize
	}
	if updates.OllamaTimeout != nil {
		saved.Ollama.Timeout = strings.TrimSpace(*updates.OllamaTimeout)
	}
	if updates.OpenAIBaseURL != nil {
		saved.OpenAI.BaseURL = strings.TrimRight(strings.TrimSpace(*updates.OpenAIBaseURL), "/")
	}
	if updates.OpenAIAPIKey != nil {
		saved.OpenAI.APIKey = strings.TrimSpace(*updates.OpenAIAPIKey)
	}
	if updates.OpenAIBatchSize != nil {
		saved.OpenAI.BatchSize = *updates.OpenAIBatchSize
	}
	if updates.OpenAITimeout != nil {
		saved.OpenAI.Timeout = strings.TrimSpace(*updates.OpenAITimeout)
	}
	if updates.OpenAIDim != nil {
		saved.OpenAI.Dimensions = *updates.OpenAIDim
	}
	if updates.OpenAIInputType != nil {
		saved.OpenAI.InputType = strings.TrimSpace(*updates.OpenAIInputType)
	}
	if updates.ImageProvider != nil {
		saved.Image.Provider = strings.TrimSpace(*updates.ImageProvider)
	}
	if updates.ImageModel != nil {
		saved.Image.Model = strings.TrimSpace(*updates.ImageModel)
	}
	if updates.ImagePretrained != nil {
		saved.Image.Pretrained = strings.TrimSpace(*updates.ImagePretrained)
	}
	if updates.ImageDim != nil {
		saved.Image.Dimensions = *updates.ImageDim
	}
	if updates.ImageDevice != nil {
		saved.Image.Device = strings.TrimSpace(*updates.ImageDevice)
	}
	if updates.ImagePython != nil {
		saved.Image.Python = strings.TrimSpace(*updates.ImagePython)
	}
	if updates.ImageVision != nil {
		saved.Image.VisionModel = strings.TrimSpace(*updates.ImageVision)
	}
	if updates.TagExtractorEnabled != nil {
		saved.TagExtractor.Enabled = *updates.TagExtractorEnabled
	}
	if updates.TagExtractorProvider != nil {
		saved.TagExtractor.Provider = strings.TrimSpace(*updates.TagExtractorProvider)
	}
	if updates.TagExtractorModel != nil {
		saved.TagExtractor.Model = strings.TrimSpace(*updates.TagExtractorModel)
	}
	if updates.TagExtractorMaxTags != nil {
		saved.TagExtractor.MaxTags = *updates.TagExtractorMaxTags
	}
	if updates.TagExtractorSkipIfHasYAML != nil {
		saved.TagExtractor.SkipIfHasYAML = *updates.TagExtractorSkipIfHasYAML
	}
	if updates.Provider != nil && updates.TagExtractorProvider == nil {
		saved.TagExtractor.Provider = saved.Provider
		if updates.TagExtractorModel == nil {
			saved.TagExtractor.Model = defaultTagExtractorModel(saved.TagExtractor.Provider)
		}
	}
	if updates.TagExtractorProvider != nil && updates.TagExtractorModel == nil {
		saved.TagExtractor.Model = defaultTagExtractorModel(saved.TagExtractor.Provider)
	}
	return saved.Settings()
}

func hasConfigUpdates(updates ConfigUpdates) bool {
	return updates.Provider != nil ||
		updates.Model != nil ||
		updates.DataDir != nil ||
		updates.VectorDim != nil ||
		updates.OllamaURL != nil ||
		updates.OllamaBatchSize != nil ||
		updates.OllamaTimeout != nil ||
		updates.OpenAIBaseURL != nil ||
		updates.OpenAIAPIKey != nil ||
		updates.OpenAIBatchSize != nil ||
		updates.OpenAITimeout != nil ||
		updates.OpenAIDim != nil ||
		updates.OpenAIInputType != nil ||
		updates.ImageProvider != nil ||
		updates.ImageModel != nil ||
		updates.ImagePretrained != nil ||
		updates.ImageDim != nil ||
		updates.ImageDevice != nil ||
		updates.ImagePython != nil ||
		updates.ImageVision != nil ||
		updates.TagExtractorEnabled != nil ||
		updates.TagExtractorProvider != nil ||
		updates.TagExtractorModel != nil ||
		updates.TagExtractorMaxTags != nil ||
		updates.TagExtractorSkipIfHasYAML != nil
}

func renderConfig(writer io.Writer, configPath string, settings config.Settings, asJSON bool) error {
	payload := config.PersistedFromSettings(settings)
	if asJSON {
		envelope := struct {
			ConfigPath string                 `json:"config_path"`
			Config     config.PersistedConfig `json:"config"`
		}{ConfigPath: configPath, Config: payload}
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(envelope)
	}
	_, err := fmt.Fprintf(writer, "Config file: %s\n  data_dir: %s\n  provider: %s\n  model: %s\n  vector_dim: %d\n  ollama.url: %s\n  openai.base_url: %s\n  openai.dimensions: %d\n  image.provider: %s\n  image.model: %s\n  image.dimensions: %d\n  image.python: %s\n  image.vision_model: %s\n  tag_extractor.enabled: %t\n  tag_extractor.provider: %s\n  tag_extractor.model: %s\n  tag_extractor.max_tags: %d\n  tag_extractor.min_tag_len: %d\n  tag_extractor.max_tag_len: %d\n  tag_extractor.skip_if_has_yaml: %t\n  tag_extractor.timeout: %s\n",
		configPath,
		payload.DataDir,
		payload.Provider,
		payload.Model,
		payload.VectorDim,
		payload.Ollama.URL,
		payload.OpenAI.BaseURL,
		payload.OpenAI.Dimensions,
		payload.Image.Provider,
		payload.Image.Model,
		payload.Image.Dimensions,
		payload.Image.Python,
		payload.Image.VisionModel,
		payload.TagExtractor.Enabled,
		payload.TagExtractor.Provider,
		payload.TagExtractor.Model,
		payload.TagExtractor.MaxTags,
		payload.TagExtractor.MinTagLen,
		payload.TagExtractor.MaxTagLen,
		payload.TagExtractor.SkipIfHasYAML,
		payload.TagExtractor.Timeout,
	)
	return err
}

func cloneOptions(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func isZeroSettings(settings config.Settings) bool {
	return strings.TrimSpace(settings.DataDir) == "" &&
		strings.TrimSpace(settings.Provider) == "" &&
		strings.TrimSpace(settings.Model) == "" &&
		settings.VectorDim == 0 &&
		settings.Ollama == (config.OllamaConfig{}) &&
		settings.OpenAI == (config.OpenAIConfig{}) &&
		settings.Image == (config.ImageConfig{}) &&
		!settings.TagExtractor.Enabled &&
		strings.TrimSpace(settings.TagExtractor.Provider) == "" &&
		strings.TrimSpace(settings.TagExtractor.Model) == "" &&
		settings.TagExtractor.MaxTags == 0 &&
		settings.TagExtractor.MinTagLen == 0 &&
		settings.TagExtractor.MaxTagLen == 0 &&
		!settings.TagExtractor.SkipIfHasYAML &&
		settings.TagExtractor.Timeout == 0 &&
		len(settings.TagExtractor.Options) == 0
}

func promptOpenAISettings(reader *bufio.Reader, writer io.Writer, current config.OpenAIConfig, vectorDim int) (config.OpenAIConfig, error) {
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "OpenAI provider settings:")

	resolved := current

	baseURLDefault := strings.TrimSpace(current.BaseURL)
	if baseURLDefault == "" {
		baseURLDefault = "https://api.openai.com/v1"
	}
	baseURL, err := promptLine(reader, writer, "OpenAI base URL", baseURLDefault, func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("openai base URL is required")
		}
		return nil
	})
	if err != nil {
		return config.OpenAIConfig{}, err
	}
	resolved.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")

	apiKey, err := promptLine(reader, writer, "OpenAI API key", strings.TrimSpace(current.APIKey), func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("openai api key is required (or set OPENAI_API_KEY)")
		}
		return nil
	})
	if err != nil {
		return config.OpenAIConfig{}, err
	}
	resolved.APIKey = strings.TrimSpace(apiKey)

	dimDefault := current.Dimensions
	if dimDefault <= 0 {
		dimDefault = vectorDim
	}
	if dimDefault <= 0 {
		dimDefault = config.OpenAIDefaultDim
	}
	dimValue, err := promptLine(reader, writer, "OpenAI embedding dimensions", strconv.Itoa(dimDefault), func(value string) error {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || parsed <= 0 {
			return fmt.Errorf("openai dimensions must be a positive integer")
		}
		return nil
	})
	if err != nil {
		return config.OpenAIConfig{}, err
	}
	parsedDim, _ := strconv.Atoi(strings.TrimSpace(dimValue))
	resolved.Dimensions = parsedDim

	return resolved, nil
}

func promptImageSettings(request ConfigCommandRequest, reader *bufio.Reader, writer io.Writer, current config.ImageConfig) (string, string, int, string, error) {
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Image scan provider:")

	providerOptions, providerDefault := buildImageProviderOptions(current.Provider)
	provider, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Image provider",
		Options:      providerOptions,
		DefaultValue: providerDefault,
	})
	if err != nil {
		return "", "", 0, "", err
	}
	if provider == CustomImageProviderOptionLabel {
		customDefault := strings.TrimSpace(current.Provider)
		if customDefault == "" || isStandardImageProvider(customDefault) {
			customDefault = ""
		}
		provider, err = promptLine(reader, writer, "Custom image provider", customDefault, func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("custom image provider is required")
			}
			return nil
		})
		if err != nil {
			return "", "", 0, "", err
		}
		provider = strings.TrimSpace(provider)
	}

	modelOptions, modelDefault := buildImageModelOptions(provider, current.Model)
	model, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Image model",
		Options:      modelOptions,
		DefaultValue: modelDefault,
	})
	if err != nil {
		return "", "", 0, "", err
	}
	if model == CustomModelOptionLabel {
		customDefault := strings.TrimSpace(current.Model)
		if customDefault == "" || isStandardImageModel(provider, customDefault) {
			customDefault = ""
		}
		model, err = promptLine(reader, writer, "Custom image model name", customDefault, func(value string) error {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("custom image model name is required")
			}
			return nil
		})
		if err != nil {
			return "", "", 0, "", err
		}
		model = strings.TrimSpace(model)
	}

	dimDefault := current.Dimensions
	if dimDefault <= 0 {
		dimDefault = recommendedImageDimensions(provider, model)
	}
	dimValue, err := promptLine(reader, writer, "Image vector dimension", strconv.Itoa(dimDefault), func(value string) error {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil || parsed <= 0 {
			return fmt.Errorf("image vector dimension must be a positive integer")
		}
		return nil
	})
	if err != nil {
		return "", "", 0, "", err
	}
	dim, _ := strconv.Atoi(strings.TrimSpace(dimValue))

	visionDefault := strings.TrimSpace(current.VisionModel)
	if visionDefault == "" {
		visionDefault = "llava"
	}
	visionModel, err := promptLine(reader, writer, "Image vision model (for description metadata)", visionDefault, func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("image vision model is required")
		}
		return nil
	})
	if err != nil {
		return "", "", 0, "", err
	}

	return provider, model, dim, strings.TrimSpace(visionModel), nil
}

func promptLine(reader *bufio.Reader, writer io.Writer, label string, defaultValue string, validate func(string) error) (string, error) {
	for {
		_, _ = fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}

		value := strings.TrimSpace(line)
		if value == "" {
			value = defaultValue
		}

		if validate != nil {
			if validateErr := validate(value); validateErr != nil {
				if err == io.EOF {
					return "", validateErr
				}
				_, _ = fmt.Fprintf(writer, "  %s\n", validateErr)
				continue
			}
		}

		return value, nil
	}
}

func supportedProviders() []string {
	return []string{config.DefaultProviderName, config.OpenAIProviderName}
}

func promptSelect(request ConfigSelectRequest) (string, error) {
	if len(request.Options) == 0 {
		return "", fmt.Errorf("config: no selectable options for %s", strings.ToLower(request.Label))
	}

	inputFile, ok := request.In.(*os.File)
	if !ok {
		return "", errConfigRequiresTTY
	}
	if request.Reader == nil {
		request.Reader = bufio.NewReader(request.In)
	}

	selected := indexOfOption(request.Options, request.DefaultValue)
	state, err := term.MakeRaw(int(inputFile.Fd()))
	if err != nil {
		return "", fmt.Errorf("config: interactive selection requires terminal-backed stdin: %w", err)
	}
	defer func() {
		if err := term.Restore(int(inputFile.Fd()), state); err != nil {
			_, _ = fmt.Fprintf(request.Out, "config: failed to restore terminal: %v\n", err)
		}
	}()

	_, _ = fmt.Fprintf(request.Out, "%s (use ↑/↓ and Enter)\n", request.Label)
	renderSelectionOptions(request.Out, request.Options, selected, false)

	for {
		action, err := readSelectionAction(request.Reader)
		if err != nil {
			return "", err
		}

		if action == selectionActionConfirm {
			_, _ = fmt.Fprintln(request.Out)
			return request.Options[selected], nil
		}

		next := applySelectionAction(selected, action, len(request.Options))
		if next == selected {
			continue
		}
		selected = next
		renderSelectionOptions(request.Out, request.Options, selected, true)
	}
}

func renderSelectionOptions(writer io.Writer, options []string, selected int, moveUp bool) {
	if moveUp {
		_, _ = fmt.Fprintf(writer, "\x1b[%dA", len(options))
	}

	for i, option := range options {
		if i == selected {
			_, _ = fmt.Fprintf(writer, "\r\x1b[2K%s> %s%s\n", ansiSelectedPrefix, option, ansiReset)
			continue
		}
		_, _ = fmt.Fprintf(writer, "\r\x1b[2K  %s\n", option)
	}
}

func readSelectionAction(reader *bufio.Reader) (selectionAction, error) {
	for {
		input, err := reader.ReadByte()
		if err != nil {
			return selectionActionNone, err
		}

		switch input {
		case '\r', '\n':
			return selectionActionConfirm, nil
		case 0x1b:
			next, err := reader.ReadByte()
			if err != nil {
				return selectionActionNone, err
			}
			if next != '[' {
				continue
			}

			arrow, err := reader.ReadByte()
			if err != nil {
				return selectionActionNone, err
			}

			switch arrow {
			case 'A':
				return selectionActionUp, nil
			case 'B':
				return selectionActionDown, nil
			}
		}
	}
}

func applySelectionAction(selected int, action selectionAction, optionCount int) int {
	if optionCount <= 0 {
		return 0
	}

	switch action {
	case selectionActionUp:
		if selected > 0 {
			return selected - 1
		}
	case selectionActionDown:
		if selected < optionCount-1 {
			return selected + 1
		}
	}

	return selected
}

func supportedModels(provider string) []string {
	switch supportedProvider(provider) {
	case config.OpenAIProviderName:
		return []string{config.OpenAIDefaultModel}
	default:
		return []string{
			config.DefaultModelName,
			"qwen3-embedding:0.5b",
			"qwen3-embedding:4b",
		}
	}
}

func supportedTagExtractorModels(provider string) []string {
	switch supportedProvider(provider) {
	case config.OpenAIProviderName:
		return []string{config.OpenAITagExtractorDefaultModel}
	default:
		return []string{config.OllamaTagExtractorDefaultModel}
	}
}

func buildModelOptions(provider, current string) (options []string, defaultValue string) {
	standard := supportedModels(provider)
	options = append(options, standard...)
	current = strings.TrimSpace(current)
	if current != "" && !containsString(standard, current) && !isStandardModelForOtherProvider(provider, current) {
		options = append(options, current)
		defaultValue = current
	}
	options = append(options, CustomModelOptionLabel)
	if defaultValue == "" {
		if containsString(standard, current) {
			defaultValue = current
		} else if len(standard) > 0 {
			defaultValue = standard[0]
		} else {
			defaultValue = current
		}
	}
	return options, defaultValue
}

func isStandardModelForOtherProvider(provider, model string) bool {
	for _, p := range supportedProviders() {
		if p == provider {
			continue
		}
		if containsString(supportedModels(p), model) {
			return true
		}
	}
	return false
}

func isStandardModel(provider, model string) bool {
	return containsString(supportedModels(provider), strings.TrimSpace(model))
}

func buildTagExtractorModelOptions(provider, current string) (options []string, defaultValue string) {
	standard := supportedTagExtractorModels(provider)
	options = append(options, standard...)
	current = strings.TrimSpace(current)
	if current != "" && !containsString(standard, current) && !isStandardTagExtractorModelForOtherProvider(provider, current) {
		options = append(options, current)
		defaultValue = current
	}
	options = append(options, CustomModelOptionLabel)
	if defaultValue == "" {
		if containsString(standard, current) {
			defaultValue = current
		} else if len(standard) > 0 {
			defaultValue = standard[0]
		} else {
			defaultValue = current
		}
	}
	return options, defaultValue
}

func isStandardTagExtractorModelForOtherProvider(provider, model string) bool {
	for _, p := range supportedProviders() {
		if p == provider {
			continue
		}
		if containsString(supportedTagExtractorModels(p), model) {
			return true
		}
	}
	return false
}

func isStandardTagExtractorModel(provider, model string) bool {
	return containsString(supportedTagExtractorModels(provider), strings.TrimSpace(model))
}

func defaultTagExtractorModel(provider string) string {
	models := supportedTagExtractorModels(provider)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func supportedImageProviders() []string {
	return []string{"openclip", "jina-clip", "openai"}
}

func supportedImageModels(provider string) []string {
	switch strings.TrimSpace(provider) {
	case "openclip":
		return []string{"ViT-B-32", "ViT-L-14"}
	case "jina-clip", "jina":
		return []string{"jinaai/jina-clip-v2"}
	case "openai":
		return []string{"text-embedding-3-small", "text-embedding-3-large"}
	default:
		return nil
	}
}

func buildImageProviderOptions(current string) (options []string, defaultValue string) {
	standard := supportedImageProviders()
	options = append(options, standard...)
	current = strings.TrimSpace(current)
	if current != "" && !containsString(standard, current) {
		options = append(options, current)
		defaultValue = current
	}
	options = append(options, CustomImageProviderOptionLabel)
	if defaultValue == "" {
		if current != "" {
			defaultValue = current
		} else {
			defaultValue = standard[0]
		}
	}
	return options, defaultValue
}

func buildImageModelOptions(provider, current string) (options []string, defaultValue string) {
	standard := supportedImageModels(provider)
	options = append(options, standard...)
	current = strings.TrimSpace(current)
	if current != "" && !containsString(standard, current) {
		options = append(options, current)
		defaultValue = current
	}
	options = append(options, CustomModelOptionLabel)
	if defaultValue == "" {
		if current != "" {
			defaultValue = current
		} else if len(standard) > 0 {
			defaultValue = standard[0]
		}
	}
	return options, defaultValue
}

func isStandardImageProvider(provider string) bool {
	return containsString(supportedImageProviders(), strings.TrimSpace(provider))
}

func isStandardImageModel(provider, model string) bool {
	return containsString(supportedImageModels(provider), strings.TrimSpace(model))
}

func recommendedImageDimensions(provider, model string) int {
	switch strings.TrimSpace(provider) {
	case "openclip":
		switch strings.TrimSpace(model) {
		case "ViT-L-14":
			return 768
		default:
			return 512
		}
	case "jina-clip", "jina":
		return 1024
	case "openai":
		switch strings.TrimSpace(model) {
		case "text-embedding-3-large":
			return 3072
		default:
			return 1536
		}
	default:
		return 512
	}
}

func containsString(values []string, target string) bool {
	return slices.Contains(values, target)
}

func indexOfOption(options []string, current string) int {
	for i, option := range options {
		if option == strings.TrimSpace(current) {
			return i
		}
	}
	return 0
}

func supportedProvider(current string) string {
	trimmed := strings.TrimSpace(current)
	if containsString(supportedProviders(), trimmed) {
		return trimmed
	}
	return config.DefaultProviderName
}

func supportedModel(current string) string {
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		return config.DefaultModelName
	}
	return trimmed
}

func isTerminalReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
