package cmd

import (
	"bytes"
	"testing"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewDoctorCmd(t *testing.T) {
	cmd := NewDoctorCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "doctor", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("json"))
}

func TestDoctorCommandDelegatesToAppRunner(t *testing.T) {
	stdout := &bytes.Buffer{}
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{
		Path: "config-path.json",
		Settings: config.Settings{
			DataDir:   "data-dir",
			Provider:  config.DefaultProviderName,
			Model:     config.DefaultModelName,
			VectorDim: config.DefaultVectorDim,
		},
	}}

	called := false
	cmd := newDoctorCmd(bootstrap, func(request app.DoctorCommandRequest) (app.DoctorCommandResult, error) {
		called = true
		require.Same(t, stdout, request.Out)
		require.Equal(t, "config-path.json", request.ConfigPath)
		require.Equal(t, bootstrap.Config.Settings, request.Settings)
		require.True(t, request.JSON)
		return app.DoctorCommandResult{}, nil
	})
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{"--json"})

	require.NoError(t, cmd.Execute())
	require.True(t, called)
}
