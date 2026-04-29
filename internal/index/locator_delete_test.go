package index

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeleteCollectionRemovesEntry(t *testing.T) {
	dataRoot := t.TempDir()
	rootA := filepath.Join(t.TempDir(), "a")
	rootB := filepath.Join(t.TempDir(), "b")
	require.NoError(t, mkAllDir(rootA))
	require.NoError(t, mkAllDir(rootB))

	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{
		RootDir:   rootA,
		FileType:  "markdown",
		UpdatedAt: time.Now(),
	}))
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{
		RootDir:   rootB,
		FileType:  "markdown",
		UpdatedAt: time.Now(),
	}))

	registry, err := LoadCollectionRegistry(dataRoot)
	require.NoError(t, err)
	require.Len(t, registry.Collections, 2)
	targetID := registry.Collections[0].CollectionID

	deleted, err := DeleteCollection(dataRoot, targetID)
	require.NoError(t, err)
	require.Equal(t, targetID, deleted.CollectionID)

	registry, err = LoadCollectionRegistry(dataRoot)
	require.NoError(t, err)
	require.Len(t, registry.Collections, 1)
	require.NotEqual(t, targetID, registry.Collections[0].CollectionID)
}

func TestDeleteCollectionReturnsNotFound(t *testing.T) {
	dataRoot := t.TempDir()
	root := filepath.Join(t.TempDir(), "x")
	require.NoError(t, mkAllDir(root))
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{
		RootDir:   root,
		FileType:  "markdown",
		UpdatedAt: time.Now(),
	}))

	_, err := DeleteCollection(dataRoot, "deadbeef0000000000000000000000ff")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCollectionNotFound))
}

func TestDeleteCollectionRequiresID(t *testing.T) {
	_, err := DeleteCollection(t.TempDir(), "   ")
	require.Error(t, err)
}

func mkAllDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
