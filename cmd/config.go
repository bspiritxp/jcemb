package cmd

import (
	"strconv"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type configCommandRunner func(app.ConfigCommandRequest) (app.ConfigCommandResult, error)

func NewConfigCmd() *cobra.Command {
	return newConfigCmd(app.NewBootstrap(), app.RunConfigCommand)
}

func newConfigCmd(bootstrap app.Bootstrap, runner configCommandRunner) *cobra.Command {
	var show bool
	var jsonOut bool
	updates := app.ConfigUpdates{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show, update, or interactively edit the persisted jcemb config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}

			_, err := runner(app.ConfigCommandRequest{
				In:         cmd.InOrStdin(),
				Out:        cmd.OutOrStdout(),
				ConfigPath: bootstrap.Config.Path,
				Settings:   bootstrap.Config.Settings,
				Show:       show,
				JSON:       jsonOut,
				Updates:    updates,
			})
			return err
		},
	}

	cmd.Flags().BoolVar(&show, "show", show, "show current config without editing")
	cmd.Flags().BoolVar(&jsonOut, "json", jsonOut, "output config as JSON")
	cmd.Flags().StringVar(ptrString(&updates.Provider), "set-provider", "", "set default embedding provider")
	cmd.Flags().StringVar(ptrString(&updates.Model), "set-model", "", "set default embedding model")
	cmd.Flags().StringVar(ptrString(&updates.DataDir), "set-data-dir", "", "set data directory")
	cmd.Flags().IntVar(ptrInt(&updates.VectorDim), "set-vector-dim", 0, "set default vector dimension")
	cmd.Flags().StringVar(ptrString(&updates.OllamaURL), "set-ollama-url", "", "set Ollama base URL")
	cmd.Flags().IntVar(ptrInt(&updates.OllamaBatchSize), "set-ollama-batch-size", 0, "set Ollama batch size")
	cmd.Flags().StringVar(ptrString(&updates.OllamaTimeout), "set-ollama-timeout", "", "set Ollama timeout duration")
	cmd.Flags().StringVar(ptrString(&updates.OpenAIBaseURL), "set-openai-base-url", "", "set OpenAI-compatible base URL")
	cmd.Flags().StringVar(ptrString(&updates.OpenAIAPIKey), "set-openai-api-key", "", "set OpenAI API key")
	cmd.Flags().IntVar(ptrInt(&updates.OpenAIBatchSize), "set-openai-batch-size", 0, "set OpenAI batch size")
	cmd.Flags().StringVar(ptrString(&updates.OpenAITimeout), "set-openai-timeout", "", "set OpenAI timeout duration")
	cmd.Flags().IntVar(ptrInt(&updates.OpenAIDim), "set-openai-dimensions", 0, "set OpenAI embedding dimensions")
	cmd.Flags().StringVar(ptrString(&updates.OpenAIInputType), "set-openai-input-type", "", "set OpenAI-compatible input_type override")
	cmd.Flags().StringVar(ptrString(&updates.ImageProvider), "set-image-provider", "", "set image embedding provider")
	cmd.Flags().StringVar(ptrString(&updates.ImageModel), "set-image-model", "", "set image embedding model")
	cmd.Flags().StringVar(ptrString(&updates.ImagePretrained), "set-image-pretrained", "", "set image OpenCLIP pretrained name")
	cmd.Flags().IntVar(ptrInt(&updates.ImageDim), "set-image-dimensions", 0, "set image vector dimensions")
	cmd.Flags().StringVar(ptrString(&updates.ImageDevice), "set-image-device", "", "set image embedding device")
	cmd.Flags().StringVar(ptrString(&updates.ImagePython), "set-image-python", "", "set image embedding Python executable")
	cmd.Flags().StringVar(ptrString(&updates.ImageVision), "set-image-vision-model", "", "set image captioning vision model")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		cmd.Flags().Visit(func(flag *pflag.Flag) {
			switch flag.Name {
			case "set-provider":
				updates.Provider = stringPtr(flag.Value.String())
			case "set-model":
				updates.Model = stringPtr(flag.Value.String())
			case "set-data-dir":
				updates.DataDir = stringPtr(flag.Value.String())
			case "set-vector-dim":
				updates.VectorDim = intPtr(flag.Value.String())
			case "set-ollama-url":
				updates.OllamaURL = stringPtr(flag.Value.String())
			case "set-ollama-batch-size":
				updates.OllamaBatchSize = intPtr(flag.Value.String())
			case "set-ollama-timeout":
				updates.OllamaTimeout = stringPtr(flag.Value.String())
			case "set-openai-base-url":
				updates.OpenAIBaseURL = stringPtr(flag.Value.String())
			case "set-openai-api-key":
				updates.OpenAIAPIKey = stringPtr(flag.Value.String())
			case "set-openai-batch-size":
				updates.OpenAIBatchSize = intPtr(flag.Value.String())
			case "set-openai-timeout":
				updates.OpenAITimeout = stringPtr(flag.Value.String())
			case "set-openai-dimensions":
				updates.OpenAIDim = intPtr(flag.Value.String())
			case "set-openai-input-type":
				updates.OpenAIInputType = stringPtr(flag.Value.String())
			case "set-image-provider":
				updates.ImageProvider = stringPtr(flag.Value.String())
			case "set-image-model":
				updates.ImageModel = stringPtr(flag.Value.String())
			case "set-image-pretrained":
				updates.ImagePretrained = stringPtr(flag.Value.String())
			case "set-image-dimensions":
				updates.ImageDim = intPtr(flag.Value.String())
			case "set-image-device":
				updates.ImageDevice = stringPtr(flag.Value.String())
			case "set-image-python":
				updates.ImagePython = stringPtr(flag.Value.String())
			case "set-image-vision-model":
				updates.ImageVision = stringPtr(flag.Value.String())
			}
		})
		return nil
	}
	return cmd
}

func ptrString(target **string) *string {
	value := ""
	*target = nil
	return &value
}

func ptrInt(target **int) *int {
	value := 0
	*target = nil
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value string) *int {
	parsed, _ := strconv.Atoi(value)
	return &parsed
}
