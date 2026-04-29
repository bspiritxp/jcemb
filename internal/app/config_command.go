package app

import (
	"bufio"
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
	Save       func(config.PersistedConfig) error
	IsTerminal func(io.Reader) bool
	Select     func(ConfigSelectRequest) (string, error)
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
	if request.Save == nil {
		request.Save = config.Save
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
	if request.Settings == (config.Settings{}) {
		request.Settings = config.DefaultSettings()
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
			BaseURL:    current.OpenAI.BaseURL,
			APIKey:     current.OpenAI.APIKey,
			BatchSize:  current.OpenAI.BatchSize,
			Timeout:    current.OpenAI.Timeout.String(),
			Dimensions: current.OpenAI.Dimensions,
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

	return ConfigCommandResult{ConfigPath: request.ConfigPath, Saved: saved}, nil
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
	return []string{config.DefaultProviderName}
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

func buildModelOptions(provider, current string) (options []string, defaultValue string) {
	standard := supportedModels(provider)
	options = append(options, standard...)
	current = strings.TrimSpace(current)
	if current != "" && !containsString(standard, current) {
		options = append(options, current)
		defaultValue = current
	}
	options = append(options, CustomModelOptionLabel)
	if defaultValue == "" {
		defaultValue = current
	}
	return options, defaultValue
}

func isStandardModel(provider, model string) bool {
	return containsString(supportedModels(provider), strings.TrimSpace(model))
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
