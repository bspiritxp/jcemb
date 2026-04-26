package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/bspiritxp/jcemb/internal/config"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"golang.org/x/term"
)

var errConfigRequiresTTY = fmt.Errorf("config: interactive mode requires a terminal on stdin; run `jcemb config` in a terminal")

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

	fmt.Fprintf(writer, "Config file: %s\n", request.ConfigPath)
	fmt.Fprintln(writer, "Press Enter to keep the current value.")

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

	model, err := request.Select(ConfigSelectRequest{
		In:           request.In,
		Reader:       reader,
		Out:          writer,
		Label:        "Model",
		Options:      supportedModels(provider),
		DefaultValue: modelDefault,
	})
	if err != nil {
		return ConfigCommandResult{}, err
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
	}

	if err := request.Save(saved); err != nil {
		return ConfigCommandResult{}, err
	}

	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Config saved.")
	fmt.Fprintf(writer, "  data_dir: %s\n", saved.DataDir)
	fmt.Fprintf(writer, "  provider: %s\n", saved.Provider)
	fmt.Fprintf(writer, "  model: %s\n", saved.Model)
	fmt.Fprintf(writer, "  vector_dim: %d\n", saved.VectorDim)

	return ConfigCommandResult{ConfigPath: request.ConfigPath, Saved: saved}, nil
}

func promptLine(reader *bufio.Reader, writer io.Writer, label string, defaultValue string, validate func(string) error) (string, error) {
	for {
		fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue)
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
				fmt.Fprintf(writer, "  %s\n", validateErr)
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
			fmt.Fprintf(request.Out, "config: failed to restore terminal: %v\n", err)
		}
	}()

	fmt.Fprintf(request.Out, "%s (use ↑/↓ and Enter)\n", request.Label)
	renderSelectionOptions(request.Out, request.Options, selected, false)

	for {
		action, err := readSelectionAction(request.Reader)
		if err != nil {
			return "", err
		}

		if action == selectionActionConfirm {
			fmt.Fprintln(request.Out)
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
		fmt.Fprintf(writer, "\x1b[%dA", len(options))
	}

	for i, option := range options {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		fmt.Fprintf(writer, "\r\x1b[2K%s%s\n", prefix, option)
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
	if supportedProvider(provider) != config.DefaultProviderName {
		return []string{config.DefaultModelName}
	}
	return []string{config.DefaultModelName}
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
	if strings.TrimSpace(current) == config.DefaultProviderName {
		return config.DefaultProviderName
	}
	return config.DefaultProviderName
}

func supportedModel(current string) string {
	if strings.TrimSpace(current) == config.DefaultModelName {
		return config.DefaultModelName
	}
	return config.DefaultModelName
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
