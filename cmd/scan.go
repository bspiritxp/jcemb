package cmd

import (
	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/output"
	"github.com/spf13/cobra"
)

type ScanOptions struct {
	Type            string
	Extensions      []string
	Concurrency     int
	Provider        string
	Model           string
	Recursive       bool
	Force           bool
	ExcludePatterns []string
}

func NewScanCmd() *cobra.Command {
	return newScanCmd(app.NewBootstrap())
}

func newScanCmd(bootstrap app.Bootstrap) *cobra.Command {
	defaults := config.Defaults()
	options := ScanOptions{
		Concurrency: 2,
		Provider:    bootstrap.Config.Settings.Provider,
		Model:       bootstrap.Config.Settings.Model,
	}

	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan Markdown files into the unified vector store",
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
				Extensions:      append([]string(nil), options.Extensions...),
				Concurrency:     options.Concurrency,
				DataDir:         bootstrap.Config.Settings.DataDir,
				Provider:        options.Provider,
				ProviderOptions: bootstrap.Config.Settings.ProviderOptions(options.Provider),
				Model:           options.Model,
				Recursive:       options.Recursive,
				Force:           options.Force,
				ExcludePatterns: append([]string(nil), options.ExcludePatterns...),
				OnProgress:      progress.Update,
			})
			if err != nil {
				return err
			}

			progress.Finish(result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&options.Type, "type", "t", options.Type, "document type to scan")
	_ = cmd.Flags().MarkHidden("type")
	cmd.Flags().StringSliceVarP(&options.Extensions, "ext", "e", nil, "file extensions to scan (repeatable, comma-separated)")
	cmd.Flags().IntVarP(&options.Concurrency, "concurccy", "c", options.Concurrency, "number of concurrent workers")
	cmd.Flags().StringVarP(&options.Provider, "provider", "p", options.Provider, "embedding provider")
	cmd.Flags().StringVarP(&options.Model, "model", "m", options.Model, "embedding model")
	cmd.Flags().BoolVarP(&options.Recursive, "recursive", "r", options.Recursive, "scan subdirectories recursively")
	cmd.Flags().BoolVar(&options.Force, "force", options.Force, "force rescan all documents")
	cmd.Flags().StringSliceVar(&options.ExcludePatterns, "exclude-pattern", nil, "gitignore-style path pattern to exclude from scan (repeatable, comma-separated)")

	return cmd
}
