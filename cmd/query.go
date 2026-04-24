package cmd

import (
	"errors"
	"fmt"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/spf13/cobra"
)

type QueryOptions struct {
	Tags           []string
	Limit          int
	Path           string
	JSON           bool
	Unique         bool
	Full           bool
	ThresholdAlpha float64
	ThresholdDelta float64
	MMRLambda      float64
	SearchWindow   int
}

func NewQueryCmd() *cobra.Command {
	defaults := config.Defaults()
	options := QueryOptions{
		Limit: 10,
		Path:  defaults.DefaultPath,
	}

	cmd := &cobra.Command{
		Use:   "query <query-text>",
		Short: "Query the local vector store",
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
			return app.Query(app.QueryRequest{
				Text:           args[0],
				Tags:           options.Tags,
				Limit:          options.Limit,
				Path:           options.Path,
				JSON:           options.JSON,
				Unique:         options.Unique,
				Full:           options.Full,
				ThresholdAlpha: options.ThresholdAlpha,
				ThresholdDelta: options.ThresholdDelta,
				MMRLambda:      options.MMRLambda,
				SearchWindow:   options.SearchWindow,
			})
		},
	}

	cmd.Flags().StringSliceVarP(&options.Tags, "tags", "t", nil, "required tags filter")
	cmd.Flags().IntVarP(&options.Limit, "limit", "l", options.Limit, "maximum number of results")
	cmd.Flags().StringVar(&options.Path, "path", options.Path, "database root path")
	cmd.Flags().BoolVar(&options.JSON, "json", options.JSON, "output results as JSON")
	cmd.Flags().BoolVarP(&options.Unique, "unique", "u", options.Unique, "deduplicate results by document (keep highest-scoring chunk per file)")
	cmd.Flags().BoolVar(&options.Full, "full", options.Full, "show full chunk content instead of truncated preview")
	cmd.Flags().Float64Var(&options.ThresholdAlpha, "threshold-alpha", options.ThresholdAlpha, "relative-to-top1 cutoff (0=auto default, negative=disable)")
	cmd.Flags().Float64Var(&options.ThresholdDelta, "threshold-delta", options.ThresholdDelta, "absolute gap from top1 cutoff (0=auto default, negative=disable)")
	cmd.Flags().Float64Var(&options.MMRLambda, "mmr-lambda", options.MMRLambda, "MMR lambda; 1.0 disables MMR (pure score order), negative also disables")
	cmd.Flags().IntVar(&options.SearchWindow, "search-window", options.SearchWindow, "internal candidate window before threshold/dedup/MMR (0=auto)")

	return cmd
}
