package cmd

import (
	"errors"
	"fmt"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/spf13/cobra"
)

type QueryOptions struct {
	Tags           []string
	Limit          int
	Path           string
	FileType       string
	JSON           bool
	Unique         bool
	Full           bool
	ThresholdAlpha float64
	ThresholdDelta float64
	MMRLambda      float64
	SearchWindow   int
}

func NewQueryCmd() *cobra.Command {
	return newQueryCmd(app.NewBootstrap())
}

func newQueryCmd(bootstrap app.Bootstrap) *cobra.Command {
	options := QueryOptions{
		Limit:    10,
		FileType: "markdown",
	}

	cmd := &cobra.Command{
		Use:   "query <query-text>",
		Short: "Query the unified vector store",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("query text is required")
			}
			if len(args) > 1 {
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}

			return app.Query(app.QueryRequest{
				Text:            args[0],
				Tags:            options.Tags,
				Limit:           options.Limit,
				Path:            options.Path,
				DataDir:         bootstrap.Config.Settings.DataDir,
				Provider:        bootstrap.Config.Settings.Provider,
				ProviderOptions: bootstrap.Config.Settings.ProviderOptions(bootstrap.Config.Settings.Provider),
				FileType:        options.FileType,
				JSON:            options.JSON,
				Unique:          options.Unique,
				Full:            options.Full,
				ThresholdAlpha:  options.ThresholdAlpha,
				ThresholdDelta:  options.ThresholdDelta,
				MMRLambda:       options.MMRLambda,
				SearchWindow:    options.SearchWindow,
			})
		},
	}

	cmd.Flags().StringSliceVar(&options.Tags, "tags", nil, "required tags filter")
	cmd.Flags().StringVarP(&options.FileType, "file-type", "t", options.FileType, "file type to query")
	cmd.Flags().IntVarP(&options.Limit, "limit", "l", options.Limit, "maximum number of results")
	cmd.Flags().StringVar(&options.Path, "path", options.Path, "optional indexed file or directory path to restrict results")
	cmd.Flags().BoolVar(&options.JSON, "json", options.JSON, "output results as JSON")
	cmd.Flags().BoolVarP(&options.Unique, "unique", "u", options.Unique, "deduplicate results by document (keep highest-scoring chunk per file)")
	cmd.Flags().BoolVar(&options.Full, "full", options.Full, "show full chunk content instead of truncated preview")
	cmd.Flags().Float64Var(&options.ThresholdAlpha, "threshold-alpha", options.ThresholdAlpha, "relative-to-top1 cutoff (0=auto default, negative=disable)")
	cmd.Flags().Float64Var(&options.ThresholdDelta, "threshold-delta", options.ThresholdDelta, "absolute gap from top1 cutoff (0=auto default, negative=disable)")
	cmd.Flags().Float64Var(&options.MMRLambda, "mmr-lambda", options.MMRLambda, "MMR lambda; 1.0 disables MMR (pure score order), negative also disables")
	cmd.Flags().IntVar(&options.SearchWindow, "search-window", options.SearchWindow, "internal candidate window before threshold/dedup/MMR (0=auto)")

	return cmd
}
