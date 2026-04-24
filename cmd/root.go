package cmd

import (
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	defaults := config.Defaults()
	rootCmd := &cobra.Command{
		Use:   defaults.AppName,
		Short: "A local-first Go CLI for Markdown vector search",
	}

	rootCmd.AddCommand(NewEmbedCmd(), NewQueryCmd())

	return rootCmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
