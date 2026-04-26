package cmd

import (
	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/spf13/cobra"
)

type configCommandRunner func(app.ConfigCommandRequest) (app.ConfigCommandResult, error)

func NewConfigCmd() *cobra.Command {
	return newConfigCmd(app.NewBootstrap(), app.RunConfigCommand)
}

func newConfigCmd(bootstrap app.Bootstrap, runner configCommandRunner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Interactively edit the persisted jcemb config",
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
			})
			return err
		},
	}

	return cmd
}
