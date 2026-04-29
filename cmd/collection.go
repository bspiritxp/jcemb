package cmd

import (
	"errors"
	"fmt"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/output"
	"github.com/spf13/cobra"
)

type collectionListRunner func(app.CollectionListRequest) (app.CollectionListResult, error)
type collectionDeleteRunner func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error)

func NewCollectionCmd() *cobra.Command {
	return newCollectionCmd(app.NewBootstrap(), app.RunCollectionList, app.RunCollectionDelete)
}

func newCollectionCmd(bootstrap app.Bootstrap, listRunner collectionListRunner, deleteRunner collectionDeleteRunner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage indexed collections",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(
		newCollectionListCmd(bootstrap, listRunner),
		newCollectionDelCmd(bootstrap, deleteRunner),
	)

	return cmd
}

func newCollectionListCmd(bootstrap app.Bootstrap, runner collectionListRunner) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all indexed collections with paths, file types, and embedding models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}

			result, err := runner(app.CollectionListRequest{
				DataDir: bootstrap.Config.Settings.DataDir,
			})
			if err != nil {
				return err
			}

			rows := make([]output.CollectionRow, 0, len(result.Collections))
			for _, info := range result.Collections {
				row := output.CollectionRow{
					CollectionID: info.CollectionID,
					RootDir:      info.RootDir,
					FileType:     info.FileType,
					Provider:     info.Provider,
					Model:        info.Model,
					VectorDim:    info.VectorDim,
					FileCount:    info.FileCount,
					UpdatedAt:    info.UpdatedAt,
					CreatedAt:    info.CreatedAt,
					Unreadable:   info.LoadError != nil,
				}
				if info.LoadError != nil {
					row.LoadError = info.LoadError.Error()
				}
				rows = append(rows, row)
			}
			if asJSON {
				return output.RenderCollectionListJSON(cmd.OutOrStdout(), result.DataDir, rows)
			}
			return output.RenderCollectionList(cmd.OutOrStdout(), result.DataDir, rows)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newCollectionDelCmd(bootstrap app.Bootstrap, runner collectionDeleteRunner) *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "del <collection_id>",
		Short: "Delete a collection by id (or unique id prefix)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}

			result, err := runner(app.CollectionDeleteRequest{
				DataDir:    bootstrap.Config.Settings.DataDir,
				IDOrPrefix: args[0],
				AssumeYes:  assumeYes,
				In:         cmd.InOrStdin(),
				Out:        cmd.OutOrStdout(),
			})
			if err != nil {
				if errors.Is(err, app.ErrCollectionDeleteAborted) {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted collection %s (%s)\n", result.Deleted.CollectionID, result.Deleted.RootDir)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
