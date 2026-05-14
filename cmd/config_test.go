package cmd

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewConfigCmd(t *testing.T) {
	cmd := NewConfigCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "config", cmd.Use)
}

func TestConfigHelpShowsInteractiveDescription(t *testing.T) {
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"config", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	require.Contains(t, output, "interactively edit the persisted jcemb config")
	require.Contains(t, output, "config")
}

func TestConfigCommandDelegatesToAppRunner(t *testing.T) {
	stdin := bytes.NewBufferString("")
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
	cmd := newConfigCmd(bootstrap, func(request app.ConfigCommandRequest) (app.ConfigCommandResult, error) {
		called = true
		require.Same(t, stdin, request.In)
		require.Same(t, stdout, request.Out)
		require.Equal(t, "config-path.json", request.ConfigPath)
		require.Equal(t, bootstrap.Config.Settings, request.Settings)
		return app.ConfigCommandResult{}, nil
	})
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)

	err := cmd.Execute()
	require.NoError(t, err)
	require.True(t, called)
}

func TestConfigCommandReturnsBootstrapValidationError(t *testing.T) {
	expectedErr := errors.New("load failed")
	cmd := newConfigCmd(app.Bootstrap{Err: expectedErr}, func(request app.ConfigCommandRequest) (app.ConfigCommandResult, error) {
		t.Fatal("runner should not be called when bootstrap is invalid")
		return app.ConfigCommandResult{}, nil
	})
	cmd.SetIn(bytes.NewBufferString(""))
	cmd.SetOut(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, expectedErr)
}

func TestConfigCommandMapsTagExtractorFlags(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{Path: "config-path.json", Settings: config.DefaultSettings()}}
	cmd := newConfigCmd(bootstrap, func(request app.ConfigCommandRequest) (app.ConfigCommandResult, error) {
		require.NotNil(t, request.Updates.TagExtractorEnabled)
		require.False(t, *request.Updates.TagExtractorEnabled)
		require.NotNil(t, request.Updates.TagExtractorProvider)
		require.Equal(t, "openai", *request.Updates.TagExtractorProvider)
		require.NotNil(t, request.Updates.TagExtractorModel)
		require.Equal(t, "gpt-4.1-mini", *request.Updates.TagExtractorModel)
		require.NotNil(t, request.Updates.TagExtractorMaxTags)
		require.Equal(t, 6, *request.Updates.TagExtractorMaxTags)
		require.NotNil(t, request.Updates.TagExtractorSkipIfHasYAML)
		require.False(t, *request.Updates.TagExtractorSkipIfHasYAML)
		return app.ConfigCommandResult{}, nil
	})
	cmd.SetIn(bytes.NewBufferString(""))
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"--set-tag-extractor-enabled=false",
		"--set-tag-extractor-provider=openai",
		"--set-tag-extractor-model=gpt-4.1-mini",
		"--set-tag-extractor-max-tags=6",
		"--set-tag-extractor-skip-if-has-yaml=false",
	})

	require.NoError(t, cmd.Execute())
}
