package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/provider/openai"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	"github.com/stretchr/testify/require"
)

func TestServiceRunFailsWhenPathIsNotIndexed(t *testing.T) {
	t.Parallel()

	service := NewService(Dependencies{ResolveAppPaths: newTestAppPaths(t, t.TempDir())})
	_, err := service.Run(context.Background(), Request{Text: "hello", Path: t.TempDir()})
	require.Error(t, err)
	require.Contains(t, err.Error(), "path is not indexed")
}

func TestServiceRunFailsClearlyWhenLegacyLocalIndexExists(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	rootDir := filepath.Join(workspace, "collection")
	filePath := filepath.Join(rootDir, "docs", "guide.md")
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n"), 0o644))

	service := NewService(Dependencies{ResolveAppPaths: newTestAppPaths(t, dataRoot)})

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "directory input", path: rootDir},
		{name: "file input", path: filePath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Run(context.Background(), Request{Text: "hello", Path: tc.path})
			require.Error(t, err)
			require.Contains(t, err.Error(), "legacy local index unsupported")
			require.Contains(t, err.Error(), "run jcemb scan")
			require.Contains(t, err.Error(), tc.path)
		})
	}
}

func TestServiceRunIgnoresLegacyLocalStorageWhenRegistryResolvesGlobalCollection(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	config := testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	require.NoError(t, index.SaveCollection(dataRoot, index.CollectionEntry{RootDir: rootDir}))

	legacyStore, err := lancedb.New(domain.StoreConfig{
		RootDir:   rootDir,
		Namespace: lancedb.Name,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, legacyStore.Close()) })
	require.NoError(t, legacyStore.(interface {
		PutFileState(context.Context, domain.FileState) error
	}).PutFileState(context.Background(), domain.FileState{
		RelPath:       "docs/legacy.md",
		FileHash:      "legacy-hash",
		RecipeHash:    "legacy-recipe",
		ChunkIDs:      []string{"legacy-chunk"},
		ChunkCount:    1,
		LastIndexedAt: createdAt,
	}))

	service := NewService(Dependencies{ResolveAppPaths: newTestAppPaths(t, dataRoot)})
	_, err = service.Run(context.Background(), Request{Text: "hello", Path: rootDir})
	require.Error(t, err)
	require.Contains(t, err.Error(), filepath.Join(jcpaths.CollectionStorageDir(dataRoot, config.CollectionID), index.DirectoryName))
	require.NotContains(t, err.Error(), filepath.Join(rootDir, index.DirectoryName))
}

func TestServiceRunUsesManifestProviderAndStableSorting(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	config := testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	files := []domain.FileState{
		{RelPath: "docs/b.md", FileHash: "hash-b", RecipeHash: "recipe", ChunkIDs: []string{"chunk-b"}, ChunkCount: 1, LastIndexedAt: createdAt},
		{RelPath: "docs/a.md", FileHash: "hash-a", RecipeHash: "recipe", ChunkIDs: []string{"chunk-a"}, ChunkCount: 1, LastIndexedAt: createdAt},
		{RelPath: "docs/c.md", FileHash: "hash-c", RecipeHash: "recipe", ChunkIDs: []string{"chunk-c"}, ChunkCount: 1, LastIndexedAt: createdAt},
	}
	persistIndexedCollection(t, config, files)
	registerCollectionAt(t, dataRoot, rootDir)

	store, err := lancedb.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-b", "docs/b.md", []string{"go", "beta"}, []float32{1, 0, 0}, 0),
		newVectorRecord("chunk-a", "docs/a.md", []string{"go", "beta"}, []float32{1, 0, 0}, 0),
		newVectorRecord("chunk-c", "docs/c.md", []string{"go"}, []float32{1, 0, 0}, 0),
	}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				require.Equal(t, ollama.Name, config.Name)
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return lancedb.New, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:      "golang beta",
		Tags:      []string{"beta", "GO"},
		Limit:     10,
		Path:      rootDir,
		MMRLambda: 1.0,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"beta", "go"}, result.Tags)
	require.Equal(t, ollama.DefaultModel, provider.model.Name)
	require.Equal(t, ollama.Name, provider.model.Provider)
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, []string{result.Results[0].Chunk.Metadata.RelPath, result.Results[1].Chunk.Metadata.RelPath})
	require.Len(t, result.Results, 2)
	require.Equal(t, 1, result.Results[0].Rank)
	require.Equal(t, 2, result.Results[1].Rank)
	for _, entry := range result.Results {
		require.ElementsMatch(t, []string{"go", "beta"}, entry.Chunk.Metadata.Tags)
	}
}

func TestServiceRunUsesLongestIndexedRootAndPreservesRelativePathPrefix(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	rootDir := filepath.Join(workspace, "docs")
	nestedRoot := filepath.Join(rootDir, "nested")
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	config := testStoreConfig(nestedRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	require.NoError(t, os.MkdirAll(filepath.Join(nestedRoot, "deeper"), 0o755))
	guidePath := filepath.ToSlash(filepath.Join(nestedRoot, "deeper", "guide.md"))
	outsidePath := filepath.ToSlash(filepath.Join(nestedRoot, "outside.md"))
	require.NoError(t, os.WriteFile(filepath.Join(nestedRoot, "deeper", "guide.md"), []byte("# Guide\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(nestedRoot, "outside.md"), []byte("# Outside\n"), 0o644))
	persistIndexedCollection(t, config, []domain.FileState{{RelPath: guidePath, FileHash: "hash-guide", RecipeHash: "recipe", ChunkIDs: []string{"chunk-guide"}, ChunkCount: 1, LastIndexedAt: createdAt}, {RelPath: outsidePath, FileHash: "hash-outside", RecipeHash: "recipe", ChunkIDs: []string{"chunk-outside"}, ChunkCount: 1, LastIndexedAt: createdAt}})
	registerCollectionAt(t, dataRoot, rootDir)
	registerCollectionAt(t, dataRoot, nestedRoot)

	store, err := lancedb.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-guide", guidePath, []string{"go"}, []float32{1, 0, 0}, 0),
		newVectorRecord("chunk-outside", outsidePath, []string{"go"}, []float32{1, 0, 0}, 0),
	}))

	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return lancedb.New, nil
		},
	})

	result, err := service.Run(context.Background(), Request{Text: "guide", Path: nestedRoot, MMRLambda: 1.0})
	require.NoError(t, err)
	require.Equal(t, nestedRoot, result.RootDir)
	require.Equal(t, nestedRoot, result.PathRoot)
	require.Len(t, result.Results, 2)
	require.Equal(t, guidePath, result.Results[0].Chunk.Metadata.RelPath)
	require.Equal(t, outsidePath, result.Results[1].Chunk.Metadata.RelPath)

	dirResult, err := service.Run(context.Background(), Request{Text: "guide", Path: filepath.Join(nestedRoot, "deeper"), MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, dirResult.Results, 1)
	require.Equal(t, guidePath, dirResult.Results[0].Chunk.Metadata.RelPath)

	fileResult, err := service.Run(context.Background(), Request{Text: "guide", Path: filepath.Join(nestedRoot, "deeper", "guide.md"), MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, fileResult.Results, 1)
	require.Equal(t, guidePath, fileResult.Results[0].Chunk.Metadata.RelPath)
}

func TestNormalizeRequestResolvesRelativePath(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workspace))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWD)) })

	for _, tc := range []struct {
		name     string
		input    string
		expected string
	}{
		{name: "current dir dot slash", input: "./", expected: workspace},
		{name: "current dir dot", input: ".", expected: workspace},
		{name: "trailing slash trimmed", input: "./   ", expected: workspace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := normalizeRequest(Request{Text: "hello", Path: tc.input})
			require.NoError(t, err)
			expected, err := filepath.EvalSymlinks(tc.expected)
			require.NoError(t, err)
			actual, err := filepath.EvalSymlinks(normalized.Path)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}

	normalized, err := normalizeRequest(Request{Text: "hello", Path: ""})
	require.NoError(t, err)
	require.Empty(t, normalized.Path)
}

func TestServiceRunResolvesRelativePathFlag(t *testing.T) {
	dataRoot := t.TempDir()
	rootDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	createdAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	config := testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	relPath := "docs/guide.md"
	persistIndexedCollection(t, config, []domain.FileState{{
		RelPath: relPath, FileHash: "hash", RecipeHash: "recipe",
		ChunkIDs: []string{"chunk-guide"}, ChunkCount: 1, LastIndexedAt: createdAt,
	}})
	registerCollectionAt(t, dataRoot, rootDir)

	store, err := lancedb.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-guide", relPath, []string{"go"}, []float32{1, 0, 0}, 0),
	}))

	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(rootDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWD)) })

	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return lancedb.New, nil
		},
	})

	result, err := service.Run(context.Background(), Request{Text: "guide", Path: "./", MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, relPath, result.Results[0].Chunk.Metadata.RelPath)
}

func TestServiceRunSearchesAllCollectionsWhenPathOmitted(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	firstRoot := filepath.Join(workspace, "first")
	secondRoot := filepath.Join(workspace, "second")
	require.NoError(t, os.MkdirAll(firstRoot, 0o755))
	require.NoError(t, os.MkdirAll(secondRoot, 0o755))
	createdAt := time.Date(2026, 4, 25, 8, 0, 0, 0, time.UTC)
	firstConfig := testStoreConfig(firstRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	secondConfig := testStoreConfig(secondRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, firstConfig, nil)
	persistIndexedCollection(t, secondConfig, nil)
	registerCollectionAt(t, dataRoot, firstRoot)
	registerCollectionAt(t, dataRoot, secondRoot)

	stores := map[string]*recordingVectorStore{
		firstConfig.CollectionID:  {results: []domain.SearchResult{newSearchResult("first-doc", "docs/first.md", 0.91)}},
		secondConfig.CollectionID: {results: []domain.SearchResult{newSearchResult("second-doc", "notes/second.md", 0.97)}},
	}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				store, ok := stores[config.CollectionID]
				require.True(t, ok, "unexpected collection id %q", config.CollectionID)
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "global",
		Limit:          10,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Empty(t, result.PathRoot)
	require.Empty(t, result.RootDir)
	require.Empty(t, result.Manifest.RootDir)
	require.Empty(t, result.Manifest.CollectionID)
	require.Equal(t, ollama.Name, result.Manifest.Provider)
	require.Equal(t, ollama.DefaultModel, result.Manifest.Model)
	require.Equal(t, 3, result.Manifest.VectorDim)
	require.Equal(t, []string{"notes/second.md", "docs/first.md"}, []string{result.Results[0].Chunk.Metadata.RelPath, result.Results[1].Chunk.Metadata.RelPath})
	require.Equal(t, []int{1, 2}, resultRanks(result.Results))

	for _, store := range stores {
		require.Len(t, store.searchQueries, 1)
		require.Empty(t, store.searchQueries[0].PathPrefix)
	}
}

func TestServiceRunMatchesDescendantCollectionsWhenPathIsAncestor(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	parent := filepath.Join(workspace, "project")
	memoryRoot := filepath.Join(parent, "memory")
	notesRoot := filepath.Join(parent, "notes")
	siblingRoot := filepath.Join(workspace, "other")
	require.NoError(t, os.MkdirAll(memoryRoot, 0o755))
	require.NoError(t, os.MkdirAll(notesRoot, 0o755))
	require.NoError(t, os.MkdirAll(siblingRoot, 0o755))

	createdAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	memoryConfig := testStoreConfig(memoryRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	notesConfig := testStoreConfig(notesRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	siblingConfig := testStoreConfig(siblingRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, memoryConfig, nil)
	persistIndexedCollection(t, notesConfig, nil)
	persistIndexedCollection(t, siblingConfig, nil)
	registerCollectionAt(t, dataRoot, memoryRoot)
	registerCollectionAt(t, dataRoot, notesRoot)
	registerCollectionAt(t, dataRoot, siblingRoot)

	stores := map[string]*recordingVectorStore{
		memoryConfig.CollectionID:  {results: []domain.SearchResult{newSearchResult("memory-doc", "memory/a.md", 0.9)}},
		notesConfig.CollectionID:   {results: []domain.SearchResult{newSearchResult("notes-doc", "notes/b.md", 0.95)}},
		siblingConfig.CollectionID: {results: []domain.SearchResult{newSearchResult("sibling-doc", "other/c.md", 0.99)}},
	}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				store, ok := stores[config.CollectionID]
				require.True(t, ok, "unexpected collection id %q", config.CollectionID)
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "vendor",
		Path:           parent,
		Limit:          10,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	relPaths := []string{result.Results[0].Chunk.Metadata.RelPath, result.Results[1].Chunk.Metadata.RelPath}
	require.ElementsMatch(t, []string{"memory/a.md", "notes/b.md"}, relPaths)
	require.NotContains(t, relPaths, "other/c.md")

	require.Len(t, stores[memoryConfig.CollectionID].searchQueries, 1)
	require.Empty(t, stores[memoryConfig.CollectionID].searchQueries[0].PathPrefix)
	require.Len(t, stores[notesConfig.CollectionID].searchQueries, 1)
	require.Empty(t, stores[notesConfig.CollectionID].searchQueries[0].PathPrefix)
	require.Empty(t, stores[siblingConfig.CollectionID].searchQueries)
}

func TestServiceRunFallsBackToContentOnlyForShortQuery(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	extractor := &fakeTagExtractor{tags: []string{"topic"}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:         "short",
		Path:         rootDir,
		TagWeight:    0.3,
		MMRLambda:    1.0,
		TagExtractor: domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Zero(t, extractor.calls)
	require.Len(t, store.searchQueries, 1)
	require.False(t, store.searchQueries[0].UseTagFusion)
	require.Nil(t, store.searchQueries[0].TagVector)
}

func TestServiceRunFallsBackToContentOnlyForImagePathQuery(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 5, 14, 12, 30, 0, 0, time.UTC)
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	imagePath := filepath.Join(t.TempDir(), "query.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("png"), 0o644))

	extractor := &fakeTagExtractor{tags: []string{"topic"}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:         imagePath,
		Path:         rootDir,
		TagWeight:    0.3,
		MMRLambda:    1.0,
		TagExtractor: domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Zero(t, extractor.calls)
	require.Len(t, store.searchQueries, 1)
	require.False(t, store.searchQueries[0].UseTagFusion)
	require.Nil(t, store.searchQueries[0].TagVector)
}

func TestServiceRunFallsBackToContentOnlyWhenNoTagEnabled(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	extractor := &fakeTagExtractor{tags: []string{"topic"}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:         "this is a long query about something",
		Path:         rootDir,
		NoTag:        true,
		TagWeight:    0.3,
		MMRLambda:    1.0,
		TagExtractor: domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Zero(t, extractor.calls)
	require.Len(t, store.searchQueries, 1)
	require.False(t, store.searchQueries[0].UseTagFusion)
	require.Nil(t, store.searchQueries[0].TagVector)
}

func TestServiceRunFallsBackToContentOnlyWhenTagExtractionFails(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 5, 14, 13, 30, 0, 0, time.UTC)
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	extractor := &fakeTagExtractor{err: errors.New("boom")}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:         "this is a long query about something",
		Path:         rootDir,
		TagWeight:    0.3,
		MMRLambda:    1.0,
		TagExtractor: domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, extractor.calls)
	require.Len(t, store.searchQueries, 1)
	require.False(t, store.searchQueries[0].UseTagFusion)
	require.Nil(t, store.searchQueries[0].TagVector)
}

func TestServiceRunUsesQueryTagVectorWhenExtractionSucceeds(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	extractor := &fakeTagExtractor{tags: []string{"topic1", "topic2"}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:         "this is a long query about something",
		Path:         rootDir,
		TagWeight:    0.35,
		MMRLambda:    1.0,
		TagExtractor: domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, extractor.calls)
	require.Len(t, store.searchQueries, 1)
	require.True(t, store.searchQueries[0].UseTagFusion)
	require.Equal(t, 0.35, store.searchQueries[0].TagWeight)
	require.Equal(t, []float32{1, 0, 0}, store.searchQueries[0].TagVector)
}

func TestServiceRunExtractsQueryTagsOnceAcrossMultipleCollections(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	firstRoot := filepath.Join(workspace, "first")
	secondRoot := filepath.Join(workspace, "second")
	require.NoError(t, os.MkdirAll(firstRoot, 0o755))
	require.NoError(t, os.MkdirAll(secondRoot, 0o755))
	createdAt := time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC)
	firstConfig := testStoreConfig(firstRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	secondConfig := testStoreConfig(secondRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, firstConfig, nil)
	persistIndexedCollection(t, secondConfig, nil)
	registerCollectionAt(t, dataRoot, firstRoot)
	registerCollectionAt(t, dataRoot, secondRoot)

	extractor := &fakeTagExtractor{tags: []string{"topic1", "topic2"}}
	stores := map[string]*recordingVectorStore{
		firstConfig.CollectionID:  {},
		secondConfig.CollectionID: {},
	}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				store, ok := stores[config.CollectionID]
				require.True(t, ok, "unexpected collection id %q", config.CollectionID)
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:           "this is a long query about something",
		TagWeight:      0.3,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
		TagExtractor:   domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, extractor.calls)
	for _, store := range stores {
		require.Len(t, store.searchQueries, 1)
		require.True(t, store.searchQueries[0].UseTagFusion)
		require.Equal(t, []float32{1, 0, 0}, store.searchQueries[0].TagVector)
	}
}

func TestServiceRunCachesQueryTagVectorsPerCompatibleCollectionGroup(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	firstRoot := filepath.Join(workspace, "first")
	secondRoot := filepath.Join(workspace, "second")
	require.NoError(t, os.MkdirAll(firstRoot, 0o755))
	require.NoError(t, os.MkdirAll(secondRoot, 0o755))
	createdAt := time.Date(2026, 5, 14, 14, 45, 0, 0, time.UTC)
	firstConfig := testStoreConfig(firstRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	secondConfig := testStoreConfig(secondRoot, dataRoot, openai.Name, openai.DefaultModel, 2, createdAt)
	persistIndexedCollection(t, firstConfig, nil)
	persistIndexedCollection(t, secondConfig, nil)
	registerCollectionAt(t, dataRoot, firstRoot)
	registerCollectionAt(t, dataRoot, secondRoot)

	extractor := &fakeTagExtractor{tags: []string{"topic1", "topic2"}}
	stores := map[string]*recordingVectorStore{
		firstConfig.CollectionID:  {},
		secondConfig.CollectionID: {},
	}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			switch name {
			case ollama.Name:
				return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
					return &fakeProvider{vector: []float32{1, 0, 0}}, nil
				}, nil
			case openai.Name:
				return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
					return &fakeProvider{vector: []float32{0, 1}}, nil
				}, nil
			default:
				return nil, fmt.Errorf("unexpected provider %q", name)
			}
		},
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				return extractor, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				store, ok := stores[config.CollectionID]
				require.True(t, ok, "unexpected collection id %q", config.CollectionID)
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:           "this is a long query about something",
		TagWeight:      0.3,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
		TagExtractor:   domain.TagExtractorConfig{Provider: "ollama", Model: "qwen2.5:7b"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, extractor.calls)
	require.Len(t, stores[firstConfig.CollectionID].searchQueries, 1)
	require.Len(t, stores[secondConfig.CollectionID].searchQueries, 1)
	require.True(t, stores[firstConfig.CollectionID].searchQueries[0].UseTagFusion)
	require.True(t, stores[secondConfig.CollectionID].searchQueries[0].UseTagFusion)
	require.Equal(t, []float32{1, 0, 0}, stores[firstConfig.CollectionID].searchQueries[0].TagVector)
	require.Equal(t, []float32{0, 1}, stores[secondConfig.CollectionID].searchQueries[0].TagVector)
}

func TestServiceRunAppliesBM25RerankAfterGlobalMerge(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	workspace := t.TempDir()
	firstRoot := filepath.Join(workspace, "first")
	secondRoot := filepath.Join(workspace, "second")
	require.NoError(t, os.MkdirAll(firstRoot, 0o755))
	require.NoError(t, os.MkdirAll(secondRoot, 0o755))
	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	firstConfig := testStoreConfig(firstRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	secondConfig := testStoreConfig(secondRoot, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, firstConfig, nil)
	persistIndexedCollection(t, secondConfig, nil)
	registerCollectionAt(t, dataRoot, firstRoot)
	registerCollectionAt(t, dataRoot, secondRoot)

	target := newSearchResult("target", "docs/bagua.md", 0.80)
	target.Chunk.Content = "离卦 代表 火 的 卦象"
	unrelated := newSearchResult("unrelated", "memory/workflow.md", 0.99)
	unrelated.Chunk.Content = "这个 workflow 记录 是 修改 说明"

	stores := map[string]*recordingVectorStore{
		firstConfig.CollectionID:  {results: []domain.SearchResult{target}},
		secondConfig.CollectionID: {results: []domain.SearchResult{unrelated}},
	}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return &fakeProvider{vector: []float32{1, 0, 0}}, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				store, ok := stores[config.CollectionID]
				require.True(t, ok, "unexpected collection id %q", config.CollectionID)
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "代表火的卦象是什么",
		Limit:          2,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		Rerank:         "bm25",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"target", "unrelated"}, resultIDs(result.Results))
	require.Equal(t, []int{1, 2}, resultRanks(result.Results))
	require.Greater(t, result.Results[0].Score, result.Results[1].Score)
	require.Less(t, result.Results[1].Score, 1.0)
}

func TestServiceRunSkipsStaleCollectionsWhenPathOmitted(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()
	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 25, 8, 30, 0, 0, time.UTC)
	config := testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, config, nil)
	registerCollectionAt(t, dataRoot, rootDir)
	require.NoError(t, os.RemoveAll(rootDir))

	service := NewService(Dependencies{ResolveAppPaths: newTestAppPaths(t, dataRoot)})
	_, err := service.Run(context.Background(), Request{Text: "global"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no usable indexed collections")
	require.Contains(t, err.Error(), "jcemb scan <path> -r")
}

func TestServiceRunUsesConfiguredDataDirForGlobalStorageLookup(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	configuredDataRoot := filepath.Join(t.TempDir(), "configured-data-root")
	defaultDataRoot := filepath.Join(t.TempDir(), "default-data-root")
	createdAt := time.Date(2026, 4, 24, 9, 0, 0, 0, time.UTC)
	config := testStoreConfig(rootDir, configuredDataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt)
	persistIndexedCollection(t, config, nil)
	require.NoError(t, index.SaveCollection(configuredDataRoot, index.CollectionEntry{RootDir: rootDir}))

	loadTargets := make([]string, 0, 1)
	storeConfigs := make([]domain.StoreConfig, 0, 1)
	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, defaultDataRoot),
		LoadIndex: func(root string) (index.Snapshot, error) {
			loadTargets = append(loadTargets, root)
			return index.Snapshot{Config: config}, nil
		},
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				storeConfigs = append(storeConfigs, config)
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{Text: "lookup", Path: rootDir, DataDir: configuredDataRoot, MMRLambda: 1.0})
	require.NoError(t, err)
	require.Equal(t, []string{jcpaths.CollectionStorageDir(configuredDataRoot, config.CollectionID)}, loadTargets)
	require.Len(t, storeConfigs, 1)
	require.Equal(t, configuredDataRoot, storeConfigs[0].DataDir)
	require.Equal(t, config.CollectionID, storeConfigs[0].CollectionID)
	require.Equal(t, rootDir, storeConfigs[0].RootDir)
	require.Equal(t, lancedb.Name, storeConfigs[0].Namespace)
	_, err = os.Stat(jcpaths.CollectionStorageDir(defaultDataRoot, config.CollectionID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestServiceRunUsesStoredMetadataAfterGlobalDefaultsChange(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataRoot := t.TempDir()
	createdAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	config := testStoreConfig(rootDir, dataRoot, testStoredProviderName, "embedded-model", 3, createdAt)
	persistIndexedCollection(t, config, nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, testStoredProviderName, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				require.Equal(t, testStoredProviderName, config.Name)
				require.Nil(t, config.Options)
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				require.Equal(t, testStoredProviderName, config.Provider)
				require.Equal(t, "embedded-model", config.Model)
				require.Equal(t, 3, config.VectorDim)
				return &recordingVectorStore{}, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:            "lookup",
		Path:            rootDir,
		Provider:        "new-default-provider",
		ProviderOptions: map[string]string{"url": "http://new-default"},
		MMRLambda:       1.0,
	})
	require.NoError(t, err)
	require.Equal(t, testStoredProviderName, provider.model.Provider)
	require.Equal(t, "embedded-model", provider.model.Name)
	require.Equal(t, testStoredProviderName, result.Manifest.Provider)
	require.Equal(t, "embedded-model", result.Manifest.Model)
	require.Equal(t, 3, result.Manifest.VectorDim)
}

func TestServiceRunFailsClearlyWhenManifestProviderUnavailable(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, "missing-provider", "missing-model", 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			return nil, errors.New("unknown provider")
		},
	})

	_, err := service.Run(context.Background(), Request{Text: "hello", Path: rootDir})
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider \"missing-provider\" is not available")
}

func TestServiceUsesSearchWindowWhenCallingStore(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				require.Equal(t, ollama.Name, config.Name)
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				require.Equal(t, lancedb.Name, config.Namespace)
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{Text: "window", Path: rootDir, Limit: 3, MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, store.searchQueries, 1)
	require.Equal(t, 20, store.searchQueries[0].Limit)
}

func TestServiceHonorsExplicitSearchWindow(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 12, 30, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:           "window",
		Path:           rootDir,
		Limit:          5,
		SearchWindow:   50,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Len(t, store.searchQueries, 1)
	require.Equal(t, 50, store.searchQueries[0].Limit)
}

func TestServiceSearchWindowBelowLimitClampedToLimit(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 12, 45, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	_, err := service.Run(context.Background(), Request{
		Text:           "window",
		Path:           rootDir,
		Limit:          5,
		SearchWindow:   2,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Len(t, store.searchQueries, 1)
	require.Equal(t, 5, store.searchQueries[0].Limit)
}

func TestApplyDynamicThresholdAlphaCutoff(t *testing.T) {
	t.Parallel()

	results := applyDynamicThreshold(searchResultsWithScores(1.0, 0.95, 0.9, 0.7, 0.4), 0.92, 0)

	require.Equal(t, []float64{1.0, 0.95}, resultScores(results))
	require.Equal(t, []int{1, 2}, resultRanks(results))
}

func TestApplyDynamicThresholdDeltaCutoff(t *testing.T) {
	t.Parallel()

	results := applyDynamicThreshold(searchResultsWithScores(0.8, 0.78, 0.74, 0.5), 0, 0.05)

	require.Equal(t, []float64{0.8, 0.78}, resultScores(results))
	require.Equal(t, []int{1, 2}, resultRanks(results))
}

func TestApplyDynamicThresholdBothRules(t *testing.T) {
	t.Parallel()

	results := applyDynamicThreshold(searchResultsWithScores(1.0, 0.961, 0.93, 0.5), 0.94, 0.04)

	require.Equal(t, []float64{1.0, 0.961}, resultScores(results))
	require.Equal(t, []int{1, 2}, resultRanks(results))
}

func TestApplyDynamicThresholdEdgeCases(t *testing.T) {
	t.Parallel()

	empty := applyDynamicThreshold(nil, defaultThresholdAlpha, defaultThresholdDelta)
	require.Empty(t, empty)

	single := applyDynamicThreshold(searchResultsWithScores(0.77), defaultThresholdAlpha, defaultThresholdDelta)
	require.Equal(t, []float64{0.77}, resultScores(single))
	require.Equal(t, []int{1}, resultRanks(single))

	equal := applyDynamicThreshold(searchResultsWithScores(0.6, 0.6, 0.6), defaultThresholdAlpha, defaultThresholdDelta)
	require.Equal(t, []float64{0.6, 0.6, 0.6}, resultScores(equal))
	require.Equal(t, []int{1, 2, 3}, resultRanks(equal))
}

func TestServiceAppliesThresholdBeforeDedup(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 11, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{results: []domain.SearchResult{
		newSearchResult("chunk-high", "docs/high.md", 1.0),
		newSearchResult("chunk-low-a", "docs/low-a.md", 0.7),
		newSearchResult("chunk-low-b", "docs/low-b.md", 0.6),
		newSearchResult("chunk-low-c", "docs/high.md", 0.5),
	}}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{Text: "threshold", Path: rootDir, Limit: 5, Unique: true, MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, []string{"docs/high.md"}, []string{result.Results[0].Chunk.Metadata.RelPath})
	require.Equal(t, 1, result.Results[0].Rank)
}

func TestServiceThresholdDisabledWhenAlphaDeltaNegative(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{results: []domain.SearchResult{
		newSearchResult("chunk-1", "docs/a.md", 1.0),
		newSearchResult("chunk-2", "docs/b.md", 0.7),
		newSearchResult("chunk-3", "docs/c.md", 0.4),
	}}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "threshold disabled",
		Path:           rootDir,
		Limit:          5,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Equal(t, []float64{1.0, 0.7, 0.4}, resultScores(result.Results))
	require.Equal(t, []int{1, 2, 3}, resultRanks(result.Results))
}

func TestServiceTruncatesToUserLimitAfterDedup(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 3, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	results := make([]domain.SearchResult, 0, 30)
	for i := range 30 {
		relPath := filepath.ToSlash(filepath.Join("docs", "doc-"+string(rune('a'+(i%10)))+".md"))
		results = append(results, domain.SearchResult{
			Chunk: domain.Chunk{
				ID:      "chunk-" + relPath + "-" + string(rune('0'+(i/10))),
				Content: "content for " + relPath,
				Metadata: domain.ChunkMetadata{
					Source:     relPath,
					FilePath:   relPath,
					RelPath:    relPath,
					FileName:   filepath.Base(relPath),
					DocType:    "md",
					FileHash:   "hash-" + relPath,
					ChunkIndex: i,
					Tags:       []string{"go"},
					YAML:       map[string]any{},
				},
			},
			Score: 1 - float64(i)/100,
			Rank:  i + 1,
		})
	}

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{results: results}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{Text: "dedup", Path: rootDir, Limit: 5, Unique: true, ThresholdAlpha: -1, ThresholdDelta: -1, MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, result.Results, 5)

	seen := make(map[string]struct{}, len(result.Results))
	for _, entry := range result.Results {
		_, exists := seen[entry.Chunk.Metadata.RelPath]
		require.False(t, exists)
		seen[entry.Chunk.Metadata.RelPath] = struct{}{}
	}
	require.Len(t, seen, 5)
}

func TestServiceAppliesMMRBeforeFinalLimit(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 13, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 2, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0.3}}
	store := &recordingVectorStore{results: buildSearchResults(
		testResultSpec{id: "doc-a-1", relPath: "docs/a.md", score: 0.98, vector: []float32{1, 0}, chunkIndex: 0},
		testResultSpec{id: "doc-a-2", relPath: "docs/a.md", score: 0.97, vector: []float32{0.9, -0.1}, chunkIndex: 1},
		testResultSpec{id: "doc-a-3", relPath: "docs/a.md", score: 0.96, vector: []float32{0.88, -0.08}, chunkIndex: 2},
		testResultSpec{id: "doc-b", relPath: "docs/b.md", score: 0.9, vector: []float32{0, 1}, chunkIndex: 0},
		testResultSpec{id: "doc-c", relPath: "docs/c.md", score: 0.89, vector: []float32{-1, 0}, chunkIndex: 0},
		testResultSpec{id: "doc-d", relPath: "docs/d.md", score: 0.88, vector: []float32{0, -1}, chunkIndex: 0},
	)}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "diverse",
		Path:           rootDir,
		Limit:          3,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      0.5,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"doc-a-1", "doc-b", "doc-a-3"}, resultIDs(result.Results))
	require.Equal(t, []int{1, 2, 3}, resultRanks(result.Results))
	require.Equal(t, []float64{0.98, 0.9, 0.96}, resultScores(result.Results))
}

func TestServiceMMRDisabledWhenLambdaOne(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 23, 14, 0, 0, 0, time.UTC)
	dataRoot := t.TempDir()
	persistIndexedCollection(t, testStoreConfig(rootDir, dataRoot, ollama.Name, ollama.DefaultModel, 2, createdAt), nil)
	registerCollectionAt(t, dataRoot, rootDir)

	provider := &fakeProvider{vector: []float32{1, 0}}
	store := &recordingVectorStore{results: buildSearchResults(
		testResultSpec{id: "score-a", relPath: "docs/a.md", score: 0.97, vector: []float32{1, 0}},
		testResultSpec{id: "score-b", relPath: "docs/b.md", score: 0.95, vector: []float32{0, 1}},
		testResultSpec{id: "score-c", relPath: "docs/c.md", score: 0.93, vector: []float32{-1, 0}},
	)}
	service := NewService(Dependencies{
		ResolveAppPaths: newTestAppPaths(t, dataRoot),
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			require.Equal(t, ollama.Name, name)
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return provider, nil
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			require.Equal(t, lancedb.Name, name)
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return store, nil
			}, nil
		},
	})

	result, err := service.Run(context.Background(), Request{
		Text:           "score order",
		Path:           rootDir,
		Limit:          3,
		ThresholdAlpha: -1,
		ThresholdDelta: -1,
		MMRLambda:      1.0,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"score-a", "score-b", "score-c"}, resultIDs(result.Results))
	require.Equal(t, []int{1, 2, 3}, resultRanks(result.Results))
	require.Equal(t, []float64{0.97, 0.95, 0.93}, resultScores(result.Results))
}

func newVectorRecord(chunkID string, relPath string, tags []string, vector []float32, chunkIndex int) domain.VectorRecord {
	return domain.VectorRecord{
		Chunk: domain.Chunk{
			ID:      chunkID,
			Content: "content for " + relPath,
			Metadata: domain.ChunkMetadata{
				Source:     relPath,
				FilePath:   relPath,
				RelPath:    relPath,
				FileName:   filepath.Base(relPath),
				DocType:    "md",
				FileHash:   "hash-" + strings.ReplaceAll(relPath, "/", "-"),
				ChunkIndex: chunkIndex,
				TitlePath:  []string{"Guide", strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))},
				Tags:       domain.NormalizeTags(tags),
				YAML:       map[string]any{},
			},
		},
		Vector: append([]float32(nil), vector...),
	}
}

func newSearchResult(chunkID string, relPath string, score float64) domain.SearchResult {
	return domain.SearchResult{
		Chunk: domain.Chunk{
			ID:      chunkID,
			Content: "content for " + relPath,
			Metadata: domain.ChunkMetadata{
				Source:     relPath,
				FilePath:   relPath,
				RelPath:    relPath,
				FileName:   filepath.Base(relPath),
				DocType:    "md",
				FileHash:   "hash-" + strings.ReplaceAll(relPath, "/", "-"),
				ChunkIndex: 0,
				Tags:       []string{"go"},
				YAML:       map[string]any{},
			},
		},
		Score: score,
	}
}

func searchResultsWithScores(scores ...float64) []domain.SearchResult {
	results := make([]domain.SearchResult, 0, len(scores))
	for index, score := range scores {
		relPath := filepath.ToSlash(filepath.Join("docs", "score-"+string(rune('a'+index))+".md"))
		results = append(results, domain.SearchResult{
			Chunk: domain.Chunk{
				ID:      "chunk-" + relPath,
				Content: "content for " + relPath,
				Metadata: domain.ChunkMetadata{
					Source:     relPath,
					FilePath:   relPath,
					RelPath:    relPath,
					FileName:   filepath.Base(relPath),
					DocType:    "md",
					FileHash:   "hash-" + strings.ReplaceAll(relPath, "/", "-"),
					ChunkIndex: index,
					Tags:       []string{"go"},
					YAML:       map[string]any{},
				},
			},
			Score: score,
			Rank:  index + 1,
		})
	}
	return results
}

type testResultSpec struct {
	id         string
	relPath    string
	score      float64
	vector     []float32
	chunkIndex int
}

func buildSearchResults(specs ...testResultSpec) []domain.SearchResult {
	results := make([]domain.SearchResult, 0, len(specs))
	for index, spec := range specs {
		results = append(results, domain.SearchResult{
			Chunk: domain.Chunk{
				ID:      spec.id,
				Content: "content for " + spec.relPath,
				Metadata: domain.ChunkMetadata{
					Source:     spec.relPath,
					FilePath:   spec.relPath,
					RelPath:    spec.relPath,
					FileName:   filepath.Base(spec.relPath),
					DocType:    "md",
					FileHash:   "hash-" + strings.ReplaceAll(spec.relPath, "/", "-"),
					ChunkIndex: spec.chunkIndex,
					Tags:       []string{"go"},
					YAML:       map[string]any{},
				},
			},
			Score:  spec.score,
			Rank:   index + 1,
			Vector: append([]float32(nil), spec.vector...),
		})
	}
	return results
}

func resultIDs(results []domain.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.Chunk.ID)
	}
	return ids
}

func resultScores(results []domain.SearchResult) []float64 {
	scores := make([]float64, 0, len(results))
	for _, result := range results {
		scores = append(scores, result.Score)
	}
	return scores
}

func resultRanks(results []domain.SearchResult) []int {
	ranks := make([]int, 0, len(results))
	for _, result := range results {
		ranks = append(ranks, result.Rank)
	}
	return ranks
}

func newTestAppPaths(t *testing.T, dataRoot string) func() (jcpaths.AppPaths, error) {
	t.Helper()
	return func() (jcpaths.AppPaths, error) {
		return jcpaths.AppPaths{DataRoot: dataRoot, ConfigFile: filepath.Join(t.TempDir(), "jcemb.json")}, nil
	}
}

func registerCollectionAt(t *testing.T, dataRoot string, rootDir string) {
	t.Helper()
	require.NoError(t, index.SaveCollection(dataRoot, index.CollectionEntry{RootDir: rootDir}))
}

func persistIndexedCollection(t *testing.T, config domain.StoreConfig, files []domain.FileState) {
	t.Helper()
	require.NoError(t, index.Save(config.RootDir, config, files))
	store, err := lancedb.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(context.Background(), nil))
	for _, file := range files {
		require.NoError(t, store.PutFileState(context.Background(), file))
	}
}

func testStoreConfig(rootDir string, dataRoot string, provider string, model string, vectorDim int, createdAt time.Time) domain.StoreConfig {
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	return domain.StoreConfig{
		CollectionID: index.CollectionIDForRoot(rootIdentity),
		RootIdentity: rootIdentity,
		RootDir:      rootDir,
		DataDir:      dataRoot,
		Provider:     provider,
		Model:        model,
		Splitter:     "markdown",
		VectorDim:    vectorDim,
		DBVersion:    lancedb.DBVersion,
		CreatedAt:    createdAt,
	}
}

const testStoredProviderName = "stored-provider"

type fakeProvider struct {
	vector []float32
	model  domain.ModelSpec
}

type fakeTagExtractor struct {
	tags  []string
	err   error
	calls int
}

type recordingVectorStore struct {
	results       []domain.SearchResult
	searchQueries []domain.SearchQuery
}

func (s *recordingVectorStore) Config() domain.StoreConfig {
	return domain.StoreConfig{}
}

func (s *recordingVectorStore) Upsert(_ context.Context, _ []domain.VectorRecord) error {
	return nil
}

func (s *recordingVectorStore) DeleteBySource(_ context.Context, _ string) error {
	return nil
}

func (s *recordingVectorStore) PutFileState(_ context.Context, _ domain.FileState) error {
	return nil
}

func (s *recordingVectorStore) DeleteFileState(_ context.Context, _ string) error {
	return nil
}

func (s *recordingVectorStore) Snapshot(_ context.Context) (domain.StoreConfig, []domain.FileState, error) {
	return domain.StoreConfig{}, nil, nil
}

func (s *recordingVectorStore) Search(_ context.Context, query domain.SearchQuery) ([]domain.SearchResult, error) {
	s.searchQueries = append(s.searchQueries, query)
	return append([]domain.SearchResult(nil), s.results...), nil
}

func (s *recordingVectorStore) FindBySource(_ context.Context, _ string) ([]domain.VectorRecord, error) {
	return nil, nil
}

func (s *recordingVectorStore) Close() error {
	return nil
}

type fakeEmbedder struct {
	vector []float32
	model  domain.ModelSpec
}

func (p *fakeProvider) Name() string {
	return ollama.Name
}

func (p *fakeProvider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	p.model = model
	return &fakeEmbedder{vector: append([]float32(nil), p.vector...), model: model}, nil
}

func (e *fakeEmbedder) Provider() string {
	return e.model.Provider
}

func (e *fakeEmbedder) Model() domain.ModelSpec {
	return e.model
}

func (e *fakeEmbedder) Embed(_ context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	if len(request.Inputs) == 0 {
		return nil, errors.New("expected at least one query input")
	}
	results := make([]domain.Embedding, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		results = append(results, domain.Embedding{ChunkID: input.ChunkID, Vector: append([]float32(nil), e.vector...)})
	}
	return results, nil
}

func (e *fakeTagExtractor) Extract(_ context.Context, _ domain.TagExtractRequest) (domain.TagExtractResult, error) {
	e.calls++
	if e.err != nil {
		return domain.TagExtractResult{}, e.err
	}
	return domain.TagExtractResult{Tags: append([]string(nil), e.tags...)}, nil
}
