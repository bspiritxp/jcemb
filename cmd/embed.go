package cmd

import (
	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/output"
	"github.com/spf13/cobra"
)

type EmbedOptions struct {
	Type        string
	Concurrency int
	Provider    string
	Model       string
	Recursive   bool
	Force       bool
}

func NewEmbedCmd() *cobra.Command {
	return newEmbedCmd(app.NewBootstrap())
}

func newEmbedCmd(bootstrap app.Bootstrap) *cobra.Command {
	defaults := config.Defaults()
	options := EmbedOptions{
		Type:        "md",
		Concurrency: 2,
		Provider:    bootstrap.Config.Settings.Provider,
		Model:       bootstrap.Config.Settings.Model,
	}

	cmd := &cobra.Command{
		Use:   "embed [path]",
		Short: "Embed Markdown files into the unified vector store",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}

			path := defaults.DefaultPath
			if len(args) == 1 {
				path = args[0]
			}

			progress := output.NewEmbedProgressBar(cmd.OutOrStdout())

			result, err := app.RunEmbed(cmd.Context(), app.EmbedRequest{
				Path:            path,
				Type:            options.Type,
				Concurrency:     options.Concurrency,
				DataDir:         bootstrap.Config.Settings.DataDir,
				Provider:        options.Provider,
				ProviderOptions: bootstrap.Config.Settings.ProviderOptions(options.Provider),
				Model:           options.Model,
				Recursive:       options.Recursive,
				Force:           options.Force,
				OnProgress:      progress.Update,
			})
			if err != nil {
				return err
			}

			progress.Finish(result.Summary)
			return nil
		},
	}

	cmd.Flags().StringVarP(&options.Type, "type", "t", options.Type, "document type to embed")
	cmd.Flags().IntVarP(&options.Concurrency, "concurccy", "c", options.Concurrency, "number of concurrent workers")
	cmd.Flags().StringVarP(&options.Provider, "provider", "p", options.Provider, "embedding provider")
	cmd.Flags().StringVarP(&options.Model, "model", "m", options.Model, "embedding model")
	cmd.Flags().BoolVarP(&options.Recursive, "recursive", "r", options.Recursive, "scan subdirectories recursively")
	cmd.Flags().BoolVar(&options.Force, "force", options.Force, "force re-embed all documents")

	return cmd
}
