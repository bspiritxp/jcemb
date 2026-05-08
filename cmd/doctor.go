package cmd

import (
	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/spf13/cobra"
)

type doctorCommandRunner func(app.DoctorCommandRequest) (app.DoctorCommandResult, error)

func NewDoctorCmd() *cobra.Command {
	return newDoctorCmd(app.NewBootstrap(), app.RunDoctorCommand)
}

func newDoctorCmd(bootstrap app.Bootstrap, runner doctorCommandRunner) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics for jcemb configuration and external runtimes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Validate(); err != nil {
				return err
			}
			_, err := runner(app.DoctorCommandRequest{
				Out:        cmd.OutOrStdout(),
				ConfigPath: bootstrap.Config.Path,
				Settings:   bootstrap.Config.Settings,
				JSON:       jsonOut,
			})
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", jsonOut, "output diagnostics as JSON")
	return cmd
}
