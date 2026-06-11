package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewQueryCmd(t *testing.T) {
	cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
		return nil
	})

	require.NotNil(t, cmd)
	require.Equal(t, "query <query-text>", cmd.Use)

	flags := cmd.Flags()
	require.NotNil(t, flags.Lookup("tags"))
	require.NotNil(t, flags.Lookup("file-type"))
	require.NotNil(t, flags.Lookup("limit"))
	require.NotNil(t, flags.Lookup("path"))
	require.NotNil(t, flags.Lookup("format"))
	require.NotNil(t, flags.Lookup("json"))
	require.NotNil(t, flags.Lookup("unique"))
	require.NotNil(t, flags.Lookup("full"))
	require.NotNil(t, flags.Lookup("no-tag"))
	require.NotNil(t, flags.Lookup("tag-weight"))
	require.NotNil(t, flags.Lookup("threshold-alpha"))
	require.NotNil(t, flags.Lookup("threshold-delta"))
	require.NotNil(t, flags.Lookup("mmr-lambda"))
	require.NotNil(t, flags.Lookup("search-window"))
	require.NotNil(t, flags.Lookup("rerank"))
	require.NotNil(t, flags.Lookup("explain"))
	require.Equal(t, "0.3", flags.Lookup("tag-weight").DefValue)
}

func TestQueryHelpShowsFlags(t *testing.T) {
	cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
		return nil
	})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	require.Contains(t, output, "--tags")
	require.Contains(t, output, "--file-type")
	require.Contains(t, output, "-t, --file-type")
	require.Contains(t, output, "--limit")
	require.Contains(t, output, "--path")
	require.Contains(t, output, "optional indexed file or directory path to restrict results")
	require.NotContains(t, output, `default "."`)
	require.Contains(t, output, "--format")
	require.Contains(t, output, "text, json, table, tsv, or tsv-z")
	require.Contains(t, output, "--json")
	require.Contains(t, output, "--unique")
	require.Contains(t, output, "--full")
	require.Contains(t, output, "--no-tag")
	require.Contains(t, output, "--tag-weight")
	require.Contains(t, output, "--threshold-alpha")
	require.Contains(t, output, "--threshold-delta")
	require.Contains(t, output, "--mmr-lambda")
	require.Contains(t, output, "--search-window")
	require.Contains(t, output, "--rerank")
	require.Contains(t, output, "--explain")
}

func TestQueryCommandPassesExplainFlag(t *testing.T) {
	called := false
	cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
		called = true
		require.True(t, request.Explain)
		require.True(t, request.JSON)
		return nil
	})
	cmd.SetArgs([]string{"--json", "--explain", "query"})

	require.NoError(t, cmd.Execute())
	require.True(t, called)
}

func TestQueryTagWeightValidationRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name string
		arg  string
	}{
		{name: "below zero", arg: "-0.1"},
		{name: "above one", arg: "1.1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
				called = true
				return nil
			})
			cmd.SetArgs([]string{"--tag-weight=" + tc.arg, "query"})

			err := cmd.Execute()
			require.Error(t, err)
			require.Contains(t, err.Error(), "tag-weight must be between 0 and 1")
			require.False(t, called)
		})
	}
}

func TestQueryNoTagOverridesNonZeroTagWeightWithoutError(t *testing.T) {
	called := false
	cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
		called = true
		require.True(t, request.NoTag)
		require.Equal(t, 0.7, request.TagWeight)
		return nil
	})
	cmd.SetArgs([]string{"--no-tag", "--tag-weight=0.7", "query"})

	require.NoError(t, cmd.Execute())
	require.True(t, called)
}

func TestQueryCommandPassesBootstrapTagExtractorConfig(t *testing.T) {
	settings := config.DefaultSettings()
	settings.TagExtractor = config.TagExtractorConfig{
		Enabled:       true,
		Provider:      config.OpenAIProviderName,
		Model:         "gpt-4.1-mini",
		MaxTags:       6,
		MinTagLen:     2,
		MaxTagLen:     24,
		SkipIfHasYAML: true,
		Timeout:       45 * time.Second,
		Options: map[string]string{
			"custom_tag_option": "cmd",
		},
	}

	called := false
	cmd := newQueryCmd(app.Bootstrap{Config: config.RuntimeConfig{Settings: settings}}, func(request app.QueryRequest) error {
		called = true
		require.Equal(t, config.OpenAIProviderName, request.TagExtractor.Provider)
		require.Equal(t, "gpt-4.1-mini", request.TagExtractor.Model)
		require.Equal(t, 45*time.Second, request.TagExtractor.Timeout)
		require.Equal(t, "cmd", request.TagExtractor.Options["custom_tag_option"])
		require.Equal(t, settings.OpenAI.BaseURL, request.TagExtractor.Options["openai_base_url"])
		return nil
	})
	cmd.SetArgs([]string{"semantic query for retrieval"})

	require.NoError(t, cmd.Execute())
	require.True(t, called)
}

func TestQueryMissingTextReturnsClearError(t *testing.T) {
	cmd := newQueryCmd(app.Bootstrap{}, func(request app.QueryRequest) error {
		return nil
	})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "query text is required")
}
