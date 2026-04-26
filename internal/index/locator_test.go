package index

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/stretchr/testify/require"
)

func TestResolveCollectionLongestRootMatchWins(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	projectRoot := filepath.Join(workspace, "docs")
	nestedRoot := filepath.Join(projectRoot, "nested")
	deepDir := filepath.Join(nestedRoot, "deep")
	require.NoError(t, os.MkdirAll(deepDir, 0o755))
	filePath := filepath.Join(deepDir, "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n"), 0o644))

	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: projectRoot}))
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: nestedRoot}))

	match, err := ResolveCollection(dataRoot, filePath)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(nestedRoot), match.RootDir)
	require.Equal(t, "deep/guide.md", match.PathPrefix)
	require.Equal(t, CollectionIDForRoot(match.RootIdentity), match.CollectionID)
}

func TestResolveCollectionPreservesDirectoryAndFilePrefixSemantics(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	nestedDir := filepath.Join(rootDir, "docs", "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))
	filePath := filepath.Join(nestedDir, "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n"), 0o644))
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: rootDir}))

	dirMatch, err := ResolveCollection(dataRoot, nestedDir)
	require.NoError(t, err)
	require.Equal(t, "docs/nested", dirMatch.PathPrefix)
	require.True(t, dirMatch.PathIsDir)

	fileMatch, err := ResolveCollection(dataRoot, filePath)
	require.NoError(t, err)
	require.Equal(t, "docs/nested/guide.md", fileMatch.PathPrefix)
	require.False(t, fileMatch.PathIsDir)
}

func TestResolveCollectionUsesEmptyPrefixForExactCollectionRoot(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	rootDir := filepath.Join(workspace, "docs")
	nestedRoot := filepath.Join(rootDir, "nested")
	require.NoError(t, os.MkdirAll(nestedRoot, 0o755))

	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: rootDir}))
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: nestedRoot}))

	rootMatch, err := ResolveCollection(dataRoot, rootDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(rootDir), rootMatch.RootDir)
	require.Equal(t, "", rootMatch.PathPrefix)
	require.True(t, rootMatch.PathIsDir)

	nestedMatch, err := ResolveCollection(dataRoot, nestedRoot)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(nestedRoot), nestedMatch.RootDir)
	require.Equal(t, "", nestedMatch.PathPrefix)
	require.True(t, nestedMatch.PathIsDir)
}

func TestResolveCollectionReturnsNotFoundForUnindexedPath(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755))
	filePath := filepath.Join(rootDir, "docs", "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n"), 0o644))

	_, err := ResolveCollection(dataRoot, filePath)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

func TestSaveCollectionPersistsRegistryUnderFixedDataRoot(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: rootDir}))

	registryPath := filepath.Join(dataRoot, collectionRegistryFileName)
	require.FileExists(t, registryPath)

	registry, err := LoadCollectionRegistry(dataRoot)
	require.NoError(t, err)
	require.Len(t, registry.Collections, 1)
	require.Equal(t, filepath.Clean(rootDir), registry.Collections[0].RootDir)
	require.Equal(t, CollectionIDForRoot(registry.Collections[0].RootIdentity), registry.Collections[0].CollectionID)

	_, err = LoadCollectionRegistry(filepath.Join(dataRoot, "missing"))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestLoadCollectionRegistryAllowsDeletedRootDirectories(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: rootDir}))
	require.NoError(t, os.RemoveAll(rootDir))

	registry, err := LoadCollectionRegistry(dataRoot)
	require.NoError(t, err)
	require.Len(t, registry.Collections, 1)
	require.Equal(t, filepath.Clean(rootDir), registry.Collections[0].RootDir)
}

func TestSaveCollectionCanonicalizesRootIdentityViaSharedPathPrimitive(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	rootDir := filepath.Join(workspace, "docs")
	require.NoError(t, os.MkdirAll(rootDir, 0o755))

	require.NoError(t, SaveCollection(dataRoot, CollectionEntry{RootDir: filepath.Join(rootDir, ".")}))

	registry, err := LoadCollectionRegistry(dataRoot)
	require.NoError(t, err)
	require.Len(t, registry.Collections, 1)
	require.Equal(t, filepath.Clean(rootDir), registry.Collections[0].RootDir)
	require.Equal(t, CollectionIDForRoot(registry.Collections[0].RootIdentity), registry.Collections[0].CollectionID)
	require.Equal(t, jcpaths.NormalizeStoredPath(rootDir), registry.Collections[0].RootIdentity)
}
