package cmd

import (
	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	defaults := config.Defaults()
	bootstrap := app.NewBootstrap()
	rootCmd := &cobra.Command{
		Use:   defaults.AppName,
		Short: "A local-first Go CLI for Markdown vector search",
	}

	rootCmd.AddCommand(
		newScanCmd(bootstrap),
		newQueryCmd(bootstrap, app.Query),
		newShowCmd(bootstrap),
		newConfigCmd(bootstrap, app.RunConfigCommand),
		newDoctorCmd(bootstrap, app.RunDoctorCommand),
		newCollectionCmd(bootstrap, app.RunCollectionList, app.RunCollectionDelete),
		NewVersionCmd(),
	)

	return rootCmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
