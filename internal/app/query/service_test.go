package query

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	"github.com/stretchr/testify/require"
)

func TestServiceRunRequiresVectorDBDirectory(t *testing.T) {
	t.Parallel()

	service := NewService(Dependencies{})
	_, err := service.Run(context.Background(), Request{Text: "hello", Path: t.TempDir()})
	require.Error(t, err)
	require.Contains(t, err.Error(), ".vectordb not found")
}

func TestServiceRunUsesManifestProviderAndStableSorting(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	config := domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}
	files := []domain.FileState{
		{RelPath: "docs/b.md", FileHash: "hash-b", RecipeHash: "recipe", ChunkIDs: []string{"chunk-b"}, ChunkCount: 1, LastIndexedAt: createdAt},
		{RelPath: "docs/a.md", FileHash: "hash-a", RecipeHash: "recipe", ChunkIDs: []string{"chunk-a"}, ChunkCount: 1, LastIndexedAt: createdAt},
		{RelPath: "docs/c.md", FileHash: "hash-c", RecipeHash: "recipe", ChunkIDs: []string{"chunk-c"}, ChunkCount: 1, LastIndexedAt: createdAt},
	}
	require.NoError(t, index.Save(rootDir, config, files))

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

func TestServiceRunFindsNearestVectorDBAndUsesRelativePathPrefix(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	config := domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "docs", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "docs", "nested", "guide.md"), []byte("# Guide\n"), 0o644))
	require.NoError(t, index.Save(rootDir, config, []domain.FileState{{RelPath: "docs/nested/guide.md", FileHash: "hash-guide", RecipeHash: "recipe", ChunkIDs: []string{"chunk-guide"}, ChunkCount: 1, LastIndexedAt: createdAt}, {RelPath: "docs/other.md", FileHash: "hash-other", RecipeHash: "recipe", ChunkIDs: []string{"chunk-other"}, ChunkCount: 1, LastIndexedAt: createdAt}}))

	store, err := lancedb.New(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(context.Background(), []domain.VectorRecord{
		newVectorRecord("chunk-guide", "docs/nested/guide.md", []string{"go"}, []float32{1, 0, 0}, 0),
		newVectorRecord("chunk-other", "docs/other.md", []string{"go"}, []float32{1, 0, 0}, 0),
	}))

	service := NewService(Dependencies{
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

	result, err := service.Run(context.Background(), Request{Text: "guide", Path: filepath.Join(rootDir, "docs", "nested"), MMRLambda: 1.0})
	require.NoError(t, err)
	require.Equal(t, rootDir, result.RootDir)
	require.Equal(t, filepath.Join(rootDir, "docs", "nested"), result.PathRoot)
	require.Len(t, result.Results, 1)
	require.Equal(t, "docs/nested/guide.md", result.Results[0].Chunk.Metadata.RelPath)

	fileResult, err := service.Run(context.Background(), Request{Text: "guide", Path: filepath.Join(rootDir, "docs", "nested", "guide.md"), MMRLambda: 1.0})
	require.NoError(t, err)
	require.Len(t, fileResult.Results, 1)
	require.Equal(t, "docs/nested/guide.md", fileResult.Results[0].Chunk.Metadata.RelPath)
}

func TestServiceRunFailsClearlyWhenManifestProviderUnavailable(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	createdAt := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  "missing-provider",
		Model:     "missing-model",
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{}
	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{results: []domain.SearchResult{
		newSearchResult("chunk-high", "docs/high.md", 1.0),
		newSearchResult("chunk-low-a", "docs/low-a.md", 0.7),
		newSearchResult("chunk-low-b", "docs/low-b.md", 0.6),
		newSearchResult("chunk-low-c", "docs/high.md", 0.5),
	}}
	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0, 0}}
	store := &recordingVectorStore{results: []domain.SearchResult{
		newSearchResult("chunk-1", "docs/a.md", 1.0),
		newSearchResult("chunk-2", "docs/b.md", 0.7),
		newSearchResult("chunk-3", "docs/c.md", 0.4),
	}}
	service := NewService(Dependencies{
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 3,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 2,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, index.DirectoryName), 0o755))
	require.NoError(t, index.Save(rootDir, domain.StoreConfig{
		RootDir:   rootDir,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  "markdown",
		VectorDim: 2,
		DBVersion: lancedb.DBVersion,
		CreatedAt: createdAt,
	}, []domain.FileState{}))

	provider := &fakeProvider{vector: []float32{1, 0}}
	store := &recordingVectorStore{results: buildSearchResults(
		testResultSpec{id: "score-a", relPath: "docs/a.md", score: 0.97, vector: []float32{1, 0}},
		testResultSpec{id: "score-b", relPath: "docs/b.md", score: 0.95, vector: []float32{0, 1}},
		testResultSpec{id: "score-c", relPath: "docs/c.md", score: 0.93, vector: []float32{-1, 0}},
	)}
	service := NewService(Dependencies{
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

type fakeProvider struct {
	vector []float32
	model  domain.ModelSpec
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

func (s *recordingVectorStore) Search(_ context.Context, query domain.SearchQuery) ([]domain.SearchResult, error) {
	s.searchQueries = append(s.searchQueries, query)
	return append([]domain.SearchResult(nil), s.results...), nil
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
	if len(request.Inputs) != 1 {
		return nil, errors.New("expected exactly one query input")
	}
	return []domain.Embedding{{ChunkID: request.Inputs[0].ChunkID, Vector: append([]float32(nil), e.vector...)}}, nil
}
