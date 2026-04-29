package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/app"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/output"
	"github.com/stretchr/testify/require"
)

func TestNewCollectionCmd(t *testing.T) {
	cmd := NewCollectionCmd()
	require.NotNil(t, cmd)
	require.Equal(t, "collection", cmd.Use)
	require.Len(t, cmd.Commands(), 2)
}

func TestCollectionListCommandDelegatesToRunner(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{
		Settings: config.Settings{DataDir: "/tmp/data"},
	}}

	called := false
	listRunner := func(req app.CollectionListRequest) (app.CollectionListResult, error) {
		called = true
		require.Equal(t, "/tmp/data", req.DataDir)
		return app.CollectionListResult{
			DataDir: "/tmp/data",
			Collections: []app.CollectionInfo{
				{
					CollectionID: "abc1234567890000",
					RootDir:      "/u/notes",
					FileType:     "markdown",
					Provider:     "ollama",
					Model:        "bge-m3",
					VectorDim:    1024,
					FileCount:    7,
					UpdatedAt:    time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
				},
			},
		}, nil
	}
	deleteRunner := func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		t.Fatal("delete runner must not be called for list")
		return app.CollectionDeleteResult{}, nil
	}

	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	require.NoError(t, cmd.Execute())
	require.True(t, called)

	out := buf.String()
	require.Contains(t, out, "abc123456789")
	require.Contains(t, out, "/u/notes")
	require.Contains(t, out, "ollama")
	require.Contains(t, out, "bge-m3")
}

func TestCollectionListCommandRendersJSON(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{
		Settings: config.Settings{DataDir: "/tmp/data"},
	}}
	updated := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	listRunner := func(app.CollectionListRequest) (app.CollectionListResult, error) {
		return app.CollectionListResult{
			DataDir: "/tmp/data",
			Collections: []app.CollectionInfo{
				{
					CollectionID: "abc1234567890000xyz",
					RootDir:      "/u/notes",
					FileType:     "markdown",
					Provider:     "ollama",
					Model:        "bge-m3",
					VectorDim:    1024,
					FileCount:    7,
					UpdatedAt:    updated,
					CreatedAt:    created,
				},
				{
					CollectionID: "deadbeef00000000",
					RootDir:      "/u/broken",
					FileType:     "markdown",
					LoadError:    errors.New("snapshot unreadable"),
					UpdatedAt:    updated,
				},
			},
		}, nil
	}
	deleteRunner := func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		return app.CollectionDeleteResult{}, nil
	}

	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list", "--json"})
	require.NoError(t, cmd.Execute())

	var envelope output.CollectionListJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	require.Equal(t, output.CollectionListSchemaVersionV1, envelope.Version)
	require.Equal(t, "/tmp/data", envelope.DataDir)
	require.Len(t, envelope.Collections, 2)

	first := envelope.Collections[0]
	require.Equal(t, "abc1234567890000xyz", first.CollectionID)
	require.Equal(t, "/u/notes", first.RootDir)
	require.Equal(t, "markdown", first.FileType)
	require.Equal(t, "ollama", first.Provider)
	require.Equal(t, "bge-m3", first.Model)
	require.Equal(t, 1024, first.VectorDim)
	require.Equal(t, 7, first.FileCount)
	require.True(t, first.UpdatedAt.Equal(updated))
	require.True(t, first.CreatedAt.Equal(created))
	require.False(t, first.Unreadable)
	require.Empty(t, first.LoadError)

	second := envelope.Collections[1]
	require.True(t, second.Unreadable)
	require.Equal(t, "snapshot unreadable", second.LoadError)
}

func TestCollectionListCommandShowsEmptyHint(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{Settings: config.Settings{DataDir: "/tmp/data"}}}
	listRunner := func(app.CollectionListRequest) (app.CollectionListResult, error) {
		return app.CollectionListResult{DataDir: "/tmp/data"}, nil
	}
	deleteRunner := func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		return app.CollectionDeleteResult{}, nil
	}
	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, buf.String(), "(no collections)")
}

func TestCollectionDelCommandPassesYesAndID(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{Settings: config.Settings{DataDir: "/tmp/data"}}}
	captured := app.CollectionDeleteRequest{}
	listRunner := func(app.CollectionListRequest) (app.CollectionListResult, error) { return app.CollectionListResult{}, nil }
	deleteRunner := func(req app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		captured = req
		return app.CollectionDeleteResult{
			Deleted: index.CollectionEntry{CollectionID: "abc1234567890000xyz", RootDir: "/u/notes"},
		}, nil
	}
	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"del", "abc", "--yes"})
	require.NoError(t, cmd.Execute())

	require.True(t, captured.AssumeYes)
	require.Equal(t, "abc", captured.IDOrPrefix)
	require.Equal(t, "/tmp/data", captured.DataDir)
	require.Contains(t, buf.String(), "Deleted collection abc1234567890000xyz")
}

func TestCollectionDelCommandReportsAbortGracefully(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{Settings: config.Settings{DataDir: "/tmp/data"}}}
	listRunner := func(app.CollectionListRequest) (app.CollectionListResult, error) { return app.CollectionListResult{}, nil }
	deleteRunner := func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		return app.CollectionDeleteResult{}, app.ErrCollectionDeleteAborted
	}
	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"del", "abc"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, buf.String(), "Aborted.")
}

func TestCollectionDelCommandPropagatesOtherErrors(t *testing.T) {
	bootstrap := app.Bootstrap{Config: config.RuntimeConfig{Settings: config.Settings{DataDir: "/tmp/data"}}}
	listRunner := func(app.CollectionListRequest) (app.CollectionListResult, error) { return app.CollectionListResult{}, nil }
	deleteRunner := func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
		return app.CollectionDeleteResult{}, errors.New("disk full")
	}
	cmd := newCollectionCmd(bootstrap, listRunner, deleteRunner)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"del", "abc", "--yes"})
	require.Error(t, cmd.Execute())
}

func TestCollectionCommandHonorsBootstrapValidationError(t *testing.T) {
	cmd := newCollectionCmd(
		app.Bootstrap{Err: errors.New("config bad")},
		func(app.CollectionListRequest) (app.CollectionListResult, error) {
			t.Fatal("list runner must not be called")
			return app.CollectionListResult{}, nil
		},
		func(app.CollectionDeleteRequest) (app.CollectionDeleteResult, error) {
			t.Fatal("delete runner must not be called")
			return app.CollectionDeleteResult{}, nil
		},
	)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list"})
	require.Error(t, cmd.Execute())
}
