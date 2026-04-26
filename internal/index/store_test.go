package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataRoot := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 10, 30, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 4, 22, 10, 31, 0, 0, time.UTC)
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	collectionID := CollectionIDForRoot(rootIdentity)

	config := domain.StoreConfig{
		RootDir:      rootDir,
		DataDir:      dataRoot,
		CollectionID: collectionID,
		RootIdentity: rootIdentity,
		Namespace:    lancedb.Name,
		Provider:     "ollama",
		Model:        "bge-m3",
		Splitter:     "markdown",
		VectorDim:    1024,
		DBVersion:    "lancedb-v1",
		CreatedAt:    createdAt,
	}

	files := []domain.FileState{
		{
			RelPath:       "docs/b.md",
			FileHash:      "file-b",
			RecipeHash:    "recipe-b",
			ChunkIDs:      []string{"b-0"},
			ChunkCount:    1,
			LastIndexedAt: indexedAt,
		},
		{
			RelPath:       "docs/a.md",
			FileHash:      "file-a",
			RecipeHash:    "recipe-a",
			ChunkIDs:      []string{"a-0", "a-1"},
			ChunkCount:    2,
			LastIndexedAt: indexedAt.Add(time.Minute),
		},
	}

	require.NoError(t, Save(rootDir, config, files))
	store, err := lancedb.New(config)
	require.NoError(t, err)
	for _, file := range files {
		require.NoError(t, store.PutFileState(context.Background(), file))
	}

	storageRoot := jcpaths.CollectionStorageDir(dataRoot, collectionID)
	snapshot, err := Load(storageRoot)
	require.NoError(t, err)

	require.Equal(t, "ollama", snapshot.Config.Provider)
	require.Equal(t, "bge-m3", snapshot.Config.Model)
	require.Equal(t, "markdown", snapshot.Config.Splitter)
	require.Equal(t, CollectionIDForRoot(snapshot.Config.RootIdentity), snapshot.Config.CollectionID)
	require.NotEmpty(t, snapshot.Config.RootIdentity)
	require.Equal(t, 1024, snapshot.Config.VectorDim)
	require.Equal(t, "lancedb-v1", snapshot.Config.DBVersion)
	require.Empty(t, snapshot.Config.ManifestVersion)
	require.Equal(t, createdAt, snapshot.Config.CreatedAt)

	require.Len(t, snapshot.Files, 2)
	require.Equal(t, "docs/a.md", snapshot.Files[0].RelPath)
	require.Equal(t, []string{"a-0", "a-1"}, snapshot.Files[0].ChunkIDs)
	require.Equal(t, 2, snapshot.Files[0].ChunkCount)
	require.Equal(t, indexedAt.Add(time.Minute), snapshot.Files[0].LastIndexedAt)
	require.Equal(t, "docs/b.md", snapshot.Files[1].RelPath)

	configPayload, err := os.ReadFile(filepath.Join(storageRoot, DirectoryName, ConfigFileName))
	require.NoError(t, err)
	require.Contains(t, string(configPayload), `"version": "v1"`)
	require.Contains(t, string(configPayload), `"provider": "ollama"`)

	indexPayload, err := os.ReadFile(filepath.Join(storageRoot, DirectoryName, IndexFileName))
	require.NoError(t, err)
	require.Contains(t, string(indexPayload), `"path": "docs/a.md"`)
	require.Contains(t, string(indexPayload), `"chunk_count": 2`)
}

func TestLoadReturnsStateNotFoundWhenManifestsAreAbsent(t *testing.T) {
	t.Parallel()

	_, err := Load(t.TempDir())
	require.ErrorIs(t, err, ErrStateNotFound)
}

func TestLoadIgnoresLegacyLocalCompatibilityManifestsWhenStorageMetadataIsAbsent(t *testing.T) {
	t.Parallel()

	t.Run("config only", func(t *testing.T) {
		rootDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, DirectoryName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, ConfigFileName), []byte(`{"version":"v1","generation":"1","provider":"ollama","model":"bge-m3","splitter":"markdown","vector_dim":1024,"db_version":"v1","created_at":"2026-04-22T10:30:00Z"}`), 0o644))

		_, err := Load(rootDir)
		require.ErrorIs(t, err, ErrStateNotFound)
	})

	t.Run("index only", func(t *testing.T) {
		rootDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, DirectoryName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, IndexFileName), []byte(`{"version":"v1","generation":"1","files":[]}`), 0o644))

		_, err := Load(rootDir)
		require.ErrorIs(t, err, ErrStateNotFound)
	})
}

func TestLoadIgnoresLegacyLocalCorruptedCompatibilityManifests(t *testing.T) {
	t.Parallel()

	t.Run("corrupted config json", func(t *testing.T) {
		rootDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, DirectoryName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, ConfigFileName), []byte(`{`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, IndexFileName), []byte(`{"version":"v1","generation":"1","files":[]}`), 0o644))

		_, err := Load(rootDir)
		require.ErrorIs(t, err, ErrStateNotFound)
	})

	t.Run("generation mismatch", func(t *testing.T) {
		rootDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, DirectoryName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, ConfigFileName), []byte(`{"version":"v1","generation":"1","provider":"ollama","model":"bge-m3","splitter":"markdown","vector_dim":1024,"db_version":"v1","created_at":"2026-04-22T10:30:00Z"}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, IndexFileName), []byte(`{"version":"v1","generation":"2","files":[]}`), 0o644))

		_, err := Load(rootDir)
		require.ErrorIs(t, err, ErrStateNotFound)
	})

	t.Run("invalid index payload", func(t *testing.T) {
		rootDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(rootDir, DirectoryName), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, ConfigFileName), []byte(`{"version":"v1","generation":"1","provider":"ollama","model":"bge-m3","splitter":"markdown","vector_dim":1024,"db_version":"v1","created_at":"2026-04-22T10:30:00Z"}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, DirectoryName, IndexFileName), []byte(`{"version":"v1","generation":"1","files":[{"path":"docs/a.md","file_hash":"file-a","recipe_hash":"recipe-a","chunk_ids":["a-0"],"chunk_count":2,"updated_at":"2026-04-22T10:31:00Z"}]}`), 0o644))

		_, err := Load(rootDir)
		require.ErrorIs(t, err, ErrStateNotFound)
	})
}

func TestFileNeedsReindexDetectsHashAndRecipeChanges(t *testing.T) {
	t.Parallel()

	recipe := domain.EmbedRecipe{
		Type:    "markdown",
		Version: "v1",
		Provider: domain.ProviderConfig{
			Name:    "ollama",
			Version: "v1",
		},
		Model: domain.ModelSpec{
			Provider:   "ollama",
			Name:       "bge-m3",
			Version:    "v1",
			Dimensions: 1024,
		},
		Splitter: domain.SplitterSpec{
			Name:    "markdown",
			Version: "v1",
		},
	}

	state := domain.FileState{
		RelPath:    "docs/a.md",
		FileHash:   "file-a",
		RecipeHash: recipe.Hash(),
	}

	reasons, needs := FileNeedsReindex(state, "file-a", recipe)
	require.False(t, needs)
	require.Empty(t, reasons)

	reasons, needs = FileNeedsReindex(state, "file-b", recipe)
	require.True(t, needs)
	require.Equal(t, []InvalidationReason{InvalidationFileHashChanged}, reasons)

	changedRecipe := recipe
	changedRecipe.Splitter.Version = "v2"
	reasons, needs = FileNeedsReindex(state, "file-a", changedRecipe)
	require.True(t, needs)
	require.Equal(t, []InvalidationReason{InvalidationRecipeChanged}, reasons)

	missingStateReasons, missingStateNeeds := FileNeedsReindex(domain.FileState{}, "file-a", recipe)
	require.True(t, missingStateNeeds)
	require.Equal(t, []InvalidationReason{InvalidationMissingState, InvalidationFileHashChanged, InvalidationRecipeChanged}, missingStateReasons)
}

func TestConfigNeedsRebuildDetectsFrozenManifestChanges(t *testing.T) {
	t.Parallel()

	stored := domain.StoreConfig{
		Provider:  "ollama",
		Model:     "bge-m3",
		Splitter:  "markdown",
		VectorDim: 1024,
		DBVersion: "v1",
	}

	reasons, rebuild := ConfigNeedsRebuild(stored, stored)
	require.False(t, rebuild)
	require.Empty(t, reasons)

	current := stored
	current.Provider = "openai"
	current.Model = "text-embedding-3-large"
	current.Splitter = "semantic"
	current.VectorDim = 3072
	current.DBVersion = "v2"

	reasons, rebuild = ConfigNeedsRebuild(stored, current)
	require.True(t, rebuild)
	require.Equal(t, []InvalidationReason{
		InvalidationProviderChanged,
		InvalidationModelChanged,
		InvalidationSplitterChanged,
		InvalidationVectorDimChanged,
		InvalidationDBVersionChanged,
	}, reasons)
}

func TestLoadRebuildErrorsRemainClassifiable(t *testing.T) {
	t.Parallel()

	storageRoot := filepath.Join(t.TempDir(), "collections", "abc123")
	require.NoError(t, os.MkdirAll(filepath.Join(storageRoot, DirectoryName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storageRoot, DirectoryName, "lancedb.records.json"), []byte(`{`), 0o644))

	_, err := Load(storageRoot)
	require.True(t, errors.Is(err, ErrRebuildRequired))
	require.Contains(t, err.Error(), "storage metadata unreadable")
}

func TestLoadUsesStorageMetadataWhenCompatibilityManifestsAreMissing(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataRoot := t.TempDir()
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	storeConfig := domain.StoreConfig{
		RootDir:      rootDir,
		DataDir:      dataRoot,
		CollectionID: CollectionIDForRoot(rootIdentity),
		RootIdentity: rootIdentity,
		Namespace:    lancedb.Name,
		Provider:     "ollama",
		Model:        "bge-m3",
		Splitter:     "markdown",
		VectorDim:    3,
		DBVersion:    lancedb.DBVersion,
		CreatedAt:    time.Date(2026, 4, 22, 10, 30, 0, 0, time.UTC),
		Flags:        map[string]bool{"recursive": true},
	}
	store, err := lancedb.New(storeConfig)
	require.NoError(t, err)
	require.NoError(t, store.PutFileState(context.Background(), domain.FileState{
		RelPath:       "docs/a.md",
		FileHash:      "hash-a",
		ModTime:       time.Date(2026, 4, 22, 10, 31, 0, 0, time.UTC),
		RecipeHash:    "recipe-a",
		ChunkIDs:      []string{"chunk-a"},
		ChunkCount:    1,
		LastIndexedAt: time.Date(2026, 4, 22, 10, 32, 0, 0, time.UTC),
	}))

	storageRoot := jcpaths.CollectionStorageDir(dataRoot, storeConfig.CollectionID)
	snapshot, err := Load(storageRoot)
	require.NoError(t, err)
	require.Equal(t, "ollama", snapshot.Config.Provider)
	require.Equal(t, dataRoot, snapshot.Config.DataDir)
	require.Equal(t, "bge-m3", snapshot.Config.Model)
	require.Equal(t, "markdown", snapshot.Config.Splitter)
	require.Equal(t, 3, snapshot.Config.VectorDim)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, "docs/a.md", snapshot.Files[0].RelPath)
	require.Equal(t, "recipe-a", snapshot.Files[0].RecipeHash)
	require.Equal(t, time.Date(2026, 4, 22, 10, 31, 0, 0, time.UTC), snapshot.Files[0].ModTime)
}

func TestLoadPrefersStorageMetadataWhenCompatibilityManifestsAreCorrupted(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataRoot := t.TempDir()
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	config := domain.StoreConfig{
		RootDir:      rootDir,
		DataDir:      dataRoot,
		CollectionID: CollectionIDForRoot(rootIdentity),
		RootIdentity: rootIdentity,
		Namespace:    lancedb.Name,
		Provider:     "ollama",
		Model:        "bge-m3",
		Splitter:     "markdown",
		VectorDim:    3,
		DBVersion:    lancedb.DBVersion,
		CreatedAt:    time.Date(2026, 4, 22, 10, 30, 0, 0, time.UTC),
	}
	store, err := lancedb.New(config)
	require.NoError(t, err)
	require.NoError(t, store.PutFileState(context.Background(), domain.FileState{
		RelPath:       "docs/a.md",
		FileHash:      "hash-a",
		RecipeHash:    "recipe-a",
		ChunkIDs:      []string{"chunk-a"},
		ChunkCount:    1,
		LastIndexedAt: time.Date(2026, 4, 22, 10, 32, 0, 0, time.UTC),
	}))
	storageRoot := jcpaths.CollectionStorageDir(dataRoot, config.CollectionID)
	require.NoError(t, os.MkdirAll(filepath.Join(storageRoot, DirectoryName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(storageRoot, DirectoryName, ConfigFileName), []byte(`{`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(storageRoot, DirectoryName, IndexFileName), []byte(`{`), 0o644))

	snapshot, err := Load(storageRoot)
	require.NoError(t, err)
	require.Equal(t, "ollama", snapshot.Config.Provider)
	require.Equal(t, dataRoot, snapshot.Config.DataDir)
	require.Len(t, snapshot.Files, 1)
}
