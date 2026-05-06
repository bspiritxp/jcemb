package cmd

import (
	"errors"
	"fmt"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/output"
	"github.com/spf13/cobra"
)

type ShowOptions struct {
	JSON bool
}

func NewShowCmd() *cobra.Command {
	return newShowCmd(app.NewBootstrap())
}

func newShowCmd(bootstrap app.Bootstrap) *cobra.Command {
	options := ShowOptions{}

	cmd := &cobra.Command{
		Use:   "show <file-path>",
		Short: "Show vector store information for a file",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("file path is required")
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

			result, err := app.RunShow(app.ShowRequest{
				FilePath: args[0],
				DataDir:  bootstrap.Config.Settings.DataDir,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return output.RenderShowJSON(cmd.OutOrStdout(), result)
			}
			return output.RenderShowText(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().BoolVar(&options.JSON, "json", false, "output as JSON")
	return cmd
}
