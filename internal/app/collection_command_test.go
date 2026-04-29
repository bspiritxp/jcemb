package app

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/stretchr/testify/require"
)

func TestRunCollectionListReturnsEnrichedInfo(t *testing.T) {
	dataDir := t.TempDir()
	registry := index.CollectionRegistry{
		Version: index.SchemaVersionV1,
		Collections: []index.CollectionEntry{
			{
				CollectionID: "abc1234567890000",
				RootIdentity: "/home/u/notes",
				RootDir:      "/home/u/notes",
				FileType:     "markdown",
				UpdatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	loadCalls := 0
	result, err := RunCollectionList(CollectionListRequest{
		DataDir: dataDir,
		LoadRegistry: func(root string) (index.CollectionRegistry, error) {
			require.Equal(t, dataDir, root)
			return registry, nil
		},
		LoadSnapshot: func(storageRoot string) (index.Snapshot, error) {
			loadCalls++
			require.Contains(t, storageRoot, "abc1234567890000")
			return index.Snapshot{
				Config: domain.StoreConfig{
					Provider:  "ollama",
					Model:     "bge-m3",
					VectorDim: 1024,
					CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
				},
				Files: []domain.FileState{{RelPath: "a.md"}, {RelPath: "b.md"}},
			}, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, dataDir, result.DataDir)
	require.Len(t, result.Collections, 1)
	require.Equal(t, 1, loadCalls)
	info := result.Collections[0]
	require.Equal(t, "ollama", info.Provider)
	require.Equal(t, "bge-m3", info.Model)
	require.Equal(t, 1024, info.VectorDim)
	require.Equal(t, 2, info.FileCount)
}

func TestRunCollectionListReturnsEmptyWhenRegistryMissing(t *testing.T) {
	result, err := RunCollectionList(CollectionListRequest{
		DataDir: t.TempDir(),
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{}, os.ErrNotExist
		},
	})
	require.NoError(t, err)
	require.Empty(t, result.Collections)
}

func TestRunCollectionListMarksUnreadableSnapshot(t *testing.T) {
	registry := index.CollectionRegistry{
		Version: index.SchemaVersionV1,
		Collections: []index.CollectionEntry{
			{CollectionID: "deadbeef00000000", RootDir: "/x", FileType: "markdown"},
		},
	}
	result, err := RunCollectionList(CollectionListRequest{
		DataDir: t.TempDir(),
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return registry, nil
		},
		LoadSnapshot: func(string) (index.Snapshot, error) {
			return index.Snapshot{}, errors.New("boom")
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Collections, 1)
	require.Error(t, result.Collections[0].LoadError)
}

func TestRunCollectionDeleteHappyPathWithAssumeYes(t *testing.T) {
	dataDir := t.TempDir()
	entry := index.CollectionEntry{CollectionID: "abc1234567890000xyz", RootDir: "/p", FileType: "markdown"}

	deleteCalled := false
	removed := ""
	result, err := RunCollectionDelete(CollectionDeleteRequest{
		DataDir:    dataDir,
		IDOrPrefix: "abc1234567890000xyz",
		AssumeYes:  true,
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{Version: index.SchemaVersionV1, Collections: []index.CollectionEntry{entry}}, nil
		},
		DeleteEntry: func(root, id string) (index.CollectionEntry, error) {
			deleteCalled = true
			require.Equal(t, "abc1234567890000xyz", id)
			return entry, nil
		},
		RemoveAll: func(path string) error {
			removed = path
			return nil
		},
	})
	require.NoError(t, err)
	require.True(t, deleteCalled)
	require.Contains(t, removed, filepath.Join("collections", "abc1234567890000xyz"))
	require.Equal(t, entry.CollectionID, result.Deleted.CollectionID)
}

func TestRunCollectionDeleteResolvesUniquePrefix(t *testing.T) {
	entries := []index.CollectionEntry{
		{CollectionID: "abc1234567890000xyz", RootDir: "/a", FileType: "markdown"},
		{CollectionID: "ffffeeeeddddccccbb", RootDir: "/b", FileType: "markdown"},
	}
	resolvedID := ""
	_, err := RunCollectionDelete(CollectionDeleteRequest{
		DataDir:    t.TempDir(),
		IDOrPrefix: "abc",
		AssumeYes:  true,
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{Version: index.SchemaVersionV1, Collections: entries}, nil
		},
		DeleteEntry: func(_ string, id string) (index.CollectionEntry, error) {
			resolvedID = id
			return entries[0], nil
		},
		RemoveAll: func(string) error { return nil },
	})
	require.NoError(t, err)
	require.Equal(t, "abc1234567890000xyz", resolvedID)
}

func TestRunCollectionDeleteRejectsAmbiguousPrefix(t *testing.T) {
	entries := []index.CollectionEntry{
		{CollectionID: "abc1234567890000xyz", RootDir: "/a", FileType: "markdown"},
		{CollectionID: "abcdef9999999999", RootDir: "/b", FileType: "markdown"},
	}
	_, err := RunCollectionDelete(CollectionDeleteRequest{
		DataDir:    t.TempDir(),
		IDOrPrefix: "abc",
		AssumeYes:  true,
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{Version: index.SchemaVersionV1, Collections: entries}, nil
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCollectionAmbiguousID))
}

func TestRunCollectionDeleteReturnsAbortedOnNo(t *testing.T) {
	entry := index.CollectionEntry{CollectionID: "abc1234567890000xyz", RootDir: "/a", FileType: "markdown"}
	output := &bytes.Buffer{}
	_, err := RunCollectionDelete(CollectionDeleteRequest{
		DataDir:    t.TempDir(),
		IDOrPrefix: "abc1234567890000xyz",
		AssumeYes:  false,
		In:         strings.NewReader("n\n"),
		Out:        output,
		LoadRegistry: func(string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{Version: index.SchemaVersionV1, Collections: []index.CollectionEntry{entry}}, nil
		},
		Confirm: func(reader *bufio.Reader, writer io.Writer, label string) (bool, error) {
			return false, nil
		},
		DeleteEntry: func(string, string) (index.CollectionEntry, error) {
			t.Fatal("DeleteEntry must not be called when user declines")
			return index.CollectionEntry{}, nil
		},
		RemoveAll: func(string) error {
			t.Fatal("RemoveAll must not be called when user declines")
			return nil
		},
	})
	require.ErrorIs(t, err, ErrCollectionDeleteAborted)
	require.Contains(t, output.String(), "About to delete collection")
}

func TestRunCollectionDeleteRequiresID(t *testing.T) {
	_, err := RunCollectionDelete(CollectionDeleteRequest{DataDir: t.TempDir(), IDOrPrefix: "  "})
	require.Error(t, err)
}

func TestPromptConfirmAcceptsYAndDefaultsToNo(t *testing.T) {
	t.Run("default no on empty", func(t *testing.T) {
		ok, err := promptConfirm(bufio.NewReader(strings.NewReader("\n")), io.Discard, "ok?")
		require.NoError(t, err)
		require.False(t, ok)
	})
	t.Run("yes accepts", func(t *testing.T) {
		ok, err := promptConfirm(bufio.NewReader(strings.NewReader("y\n")), io.Discard, "ok?")
		require.NoError(t, err)
		require.True(t, ok)
	})
	t.Run("retries on garbage then no", func(t *testing.T) {
		out := &bytes.Buffer{}
		ok, err := promptConfirm(bufio.NewReader(strings.NewReader("maybe\nno\n")), out, "ok?")
		require.NoError(t, err)
		require.False(t, ok)
		require.Contains(t, out.String(), "Please answer 'y' or 'n'")
	})
}
