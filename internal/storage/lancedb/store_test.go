package lancedb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestNewRegistersLanceDBFactory(t *testing.T) {
	t.Parallel()

	factory, err := registry.GetVectorStore(Name)
	require.NoError(t, err)
	require.NotNil(t, factory)
}

func TestStoreUpsertSearchRoundTrip(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	store := newTestStore(t, rootDir, 3)

	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go", "guide"}, []float32{1, 0, 0}),
		newVectorRecord("chunk-2", "docs/b.md", 0, []string{"go"}, []float32{0.7, 0.7, 0}),
		newVectorRecord("chunk-3", "notes/c.md", 0, []string{"ops"}, []float32{0, 1, 0}),
	}))
	require.NoError(t, store.Close())

	reopened := newTestStore(t, rootDir, 3)

	results, err := reopened.Search(context.Background(), domain.SearchQuery{
		Vector:     []float32{1, 0, 0},
		Limit:      2,
		Tags:       []string{"guide", "go"},
		PathPrefix: "docs",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "chunk-1", results[0].Chunk.ID)
	require.Equal(t, 1, results[0].Rank)
	require.InDelta(t, 1.0, results[0].Score, 1e-6)
}

func TestStoreSearchNormalizesWindowsStylePathPrefix(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	store := newTestStore(t, rootDir, 3)

	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/nested/guide.md", 0, []string{"go"}, []float32{1, 0, 0}),
		newVectorRecord("chunk-2", "docs/other.md", 0, []string{"go"}, []float32{1, 0, 0}),
	}))

	results, err := store.Search(context.Background(), domain.SearchQuery{
		Vector:     []float32{1, 0, 0},
		Limit:      10,
		PathPrefix: `DOCS\NESTED`,
	})
	require.NoError(t, err)
	if len(results) != 1 {
		t.Fatalf("expected one normalized match, got %d", len(results))
	}
	require.Equal(t, "docs/nested/guide.md", results[0].Chunk.Metadata.RelPath)
}

func TestStoreDeleteBySourceRemovesAllMatchingRecords(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, []float32{1, 0, 0}),
		newVectorRecord("chunk-2", "docs/a.md", 1, []string{"go"}, []float32{0.9, 0.1, 0}),
		newVectorRecord("chunk-3", "docs/b.md", 0, []string{"go"}, []float32{0, 1, 0}),
	}))

	require.NoError(t, store.DeleteBySource(context.Background(), "docs/a.md"))

	results, err := store.Search(context.Background(), domain.SearchQuery{
		Vector: []float32{1, 0, 0},
		Limit:  10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "chunk-3", results[0].Chunk.ID)
}

func TestStoreSearchPopulatesResultVector(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	original := []float32{1, 2, 3}
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, original),
	}))

	results, err := store.Search(context.Background(), domain.SearchQuery{
		Vector: []float32{1, 0, 0},
		Limit:  10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, original, results[0].Vector)

	results[0].Vector[0] = 99

	again, err := store.Search(context.Background(), domain.SearchQuery{
		Vector: []float32{1, 0, 0},
		Limit:  10,
	})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, original, again[0].Vector)
	if len(again[0].Vector) > 0 {
		require.NotSame(t, &results[0].Vector[0], &again[0].Vector[0])
	}
}

func TestStoreSnapshotRoundTripsCollectionAndFileMetadata(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataRoot := t.TempDir()
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	storeConfig := domain.StoreConfig{
		RootDir:      rootDir,
		DataDir:      dataRoot,
		CollectionID: jcpaths.CollectionIDForRoot(rootIdentity),
		RootIdentity: rootIdentity,
		Namespace:    Name,
		Provider:     "ollama",
		Model:        "bge-m3",
		Splitter:     "markdown",
		VectorDim:    3,
		DBVersion:    "lancedb-v1",
		CreatedAt:    time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
	}
	vectorStore, err := New(storeConfig)
	require.NoError(t, err)
	store, ok := vectorStore.(*Store)
	require.True(t, ok)
	state := domain.FileState{
		Source:        "docs/a.md",
		FilePath:      filepath.Join(rootDir, "docs", "a.md"),
		RelPath:       "docs/a.md",
		FileName:      "a.md",
		DocType:       "md",
		FileHash:      "hash-a",
		ModTime:       time.Date(2026, 4, 22, 12, 2, 0, 0, time.UTC),
		RecipeHash:    "recipe-a",
		ChunkIDs:      []string{"chunk-1"},
		ChunkCount:    1,
		LastIndexedAt: time.Date(2026, 4, 22, 12, 3, 0, 0, time.UTC),
	}

	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, []float32{1, 0, 0}),
	}))
	require.NoError(t, store.PutFileState(context.Background(), state))

	config, files, err := store.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "ollama", config.Provider)
	require.Equal(t, "bge-m3", config.Model)
	require.Equal(t, "markdown", config.Splitter)
	require.Equal(t, 3, config.VectorDim)
	require.Equal(t, DBVersion, config.DBVersion)
	require.Len(t, files, 1)
	require.Equal(t, state.RelPath, files[0].RelPath)
	require.Equal(t, state.ModTime, files[0].ModTime)
	require.Equal(t, state.RecipeHash, files[0].RecipeHash)
	require.Equal(t, state.ChunkIDs, files[0].ChunkIDs)

	reopenedVectorStore, err := New(storeConfig)
	require.NoError(t, err)
	reopened, ok := reopenedVectorStore.(*Store)
	require.True(t, ok)
	reopenedConfig, reopenedFiles, err := reopened.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, config.Provider, reopenedConfig.Provider)
	require.Equal(t, config.Model, reopenedConfig.Model)
	require.Equal(t, files, reopenedFiles)

	loadedConfig, loadedFiles, err := LoadSnapshot(jcpaths.CollectionStorageDir(dataRoot, storeConfig.CollectionID))
	require.NoError(t, err)
	require.Equal(t, dataRoot, loadedConfig.DataDir)
	require.Equal(t, reopenedConfig.Provider, loadedConfig.Provider)
	require.Equal(t, reopenedConfig.Model, loadedConfig.Model)
	require.Equal(t, reopenedConfig.Splitter, loadedConfig.Splitter)
	require.Equal(t, reopenedConfig.VectorDim, loadedConfig.VectorDim)
	require.Equal(t, reopenedFiles, loadedFiles)
}

func TestLoadSnapshotDoesNotFallbackToLegacyLocalStore(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	store := newTestStore(t, rootDir, 3)
	require.NoError(t, store.PutFileState(context.Background(), domain.FileState{
		RelPath:       "docs/a.md",
		FileHash:      "hash-a",
		RecipeHash:    "recipe-a",
		ChunkIDs:      []string{"chunk-a"},
		ChunkCount:    1,
		LastIndexedAt: time.Date(2026, 4, 22, 12, 3, 0, 0, time.UTC),
	}))

	_, _, err := LoadSnapshot(rootDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestStoreDeleteFileStateRemovesPersistedMetadata(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	store := newTestStore(t, rootDir, 3)
	require.NoError(t, store.PutFileState(context.Background(), domain.FileState{
		RelPath:       "docs/a.md",
		FileHash:      "hash-a",
		RecipeHash:    "recipe-a",
		ChunkIDs:      []string{"chunk-1"},
		ChunkCount:    1,
		LastIndexedAt: time.Date(2026, 4, 22, 12, 3, 0, 0, time.UTC),
	}))
	require.NoError(t, store.DeleteFileState(context.Background(), "docs/a.md"))

	_, files, err := store.Snapshot(context.Background())
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestStoreSearchHonorsMinScore(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, []float32{1, 0, 0}),
		newVectorRecord("chunk-2", "docs/b.md", 0, []string{"go"}, []float32{0.8, 0.6, 0}),
		newVectorRecord("chunk-3", "docs/c.md", 0, []string{"go"}, []float32{0.4, 0.9, 0}),
	}))

	results, err := store.Search(context.Background(), domain.SearchQuery{
		Vector:   []float32{1, 0, 0},
		Limit:    10,
		MinScore: 0.75,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "chunk-1", results[0].Chunk.ID)
	require.Equal(t, "chunk-2", results[1].Chunk.ID)
	for _, result := range results {
		require.GreaterOrEqual(t, result.Score, 0.75)
	}
}

func TestStoreSearchReturnsClearErrorWhenVectorDBMissing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)

	_, err := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0, 0}})
	require.ErrorIs(t, err, ErrVectorDBNotFound)
	require.Contains(t, err.Error(), ".vectordb")
}

func TestStoreRejectsDimensionMismatchOnSearch(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, []float32{1, 0, 0}),
	}))

	_, err := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0}})
	require.ErrorIs(t, err, ErrVectorDimMismatch)
	require.EqualError(t, err, "lancedb: vector dimension mismatch: expected=3 actual=2")
}

func TestStoreRejectsDimensionMismatchOnUpsert(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	err := store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-1", "docs/a.md", 0, []string{"go"}, []float32{1, 0}),
	})
	require.ErrorIs(t, err, ErrVectorDimMismatch)
	require.EqualError(t, err, "lancedb: vector dimension mismatch: expected=3 actual=2")
}

func TestStoreDeleteBySourceRequiresSource(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	err := store.DeleteBySource(context.Background(), "  ")
	require.EqualError(t, err, "lancedb: source is required")
}

func TestStoreSearchErrorRemainsClassifiable(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, t.TempDir(), 3)
	_, err := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0}})
	require.True(t, errors.Is(err, ErrVectorDimMismatch))
}

func newTestStore(t *testing.T, rootDir string, vectorDim int) *Store {
	t.Helper()

	createdAt := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	vectorStore, err := New(domain.StoreConfig{
		RootDir:   rootDir,
		Namespace: Name,
		Provider:  "ollama",
		Model:     "bge-m3",
		Splitter:  "markdown",
		VectorDim: vectorDim,
		DBVersion: "lancedb-v1",
		CreatedAt: createdAt,
	})
	require.NoError(t, err)

	store, ok := vectorStore.(*Store)
	require.True(t, ok)
	require.Equal(t, filepath.Join(rootDir, ".vectordb", storageFileName), store.storagePath)
	return store
}

func newVectorRecord(chunkID string, relPath string, chunkIndex int, tags []string, vector []float32) domain.VectorRecord {
	metadata := domain.ChunkMetadata{
		Source:     relPath,
		FilePath:   filepath.Join("/tmp", filepath.FromSlash(relPath)),
		RelPath:    relPath,
		FileName:   filepath.Base(relPath),
		DocType:    "md",
		FileHash:   "hash-" + chunkID,
		ChunkIndex: chunkIndex,
		TitlePath:  []string{"Guide"},
		Tags:       domain.NormalizeTags(tags),
		YAML:       map[string]any{"title": "Guide"},
	}

	chunk := domain.Chunk{
		ID:        chunkID,
		Content:   "content for " + chunkID,
		Metadata:  metadata,
		CreatedAt: time.Date(2026, 4, 22, 12, 1, 0, 0, time.UTC),
		Document: domain.Document{
			Source:    relPath,
			FilePath:  metadata.FilePath,
			RelPath:   relPath,
			FileName:  metadata.FileName,
			DocType:   metadata.DocType,
			FileHash:  metadata.FileHash,
			Content:   "document content",
			TitlePath: []string{"Guide"},
			Tags:      append([]string(nil), metadata.Tags...),
			YAML:      map[string]any{"title": "Guide"},
		},
	}

	return domain.VectorRecord{Chunk: chunk, Vector: vector}
}
