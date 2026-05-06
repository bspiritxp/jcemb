package show

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	reg "github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

type fakeVectorStore struct {
	records []domain.VectorRecord
	config  domain.StoreConfig
}

func (f *fakeVectorStore) Config() domain.StoreConfig                               { return f.config }
func (f *fakeVectorStore) Upsert(_ context.Context, _ []domain.VectorRecord) error  { return nil }
func (f *fakeVectorStore) DeleteBySource(_ context.Context, _ string) error         { return nil }
func (f *fakeVectorStore) PutFileState(_ context.Context, _ domain.FileState) error { return nil }
func (f *fakeVectorStore) DeleteFileState(_ context.Context, _ string) error        { return nil }
func (f *fakeVectorStore) Snapshot(_ context.Context) (domain.StoreConfig, []domain.FileState, error) {
	return f.config, nil, nil
}
func (f *fakeVectorStore) Search(_ context.Context, _ domain.SearchQuery) ([]domain.SearchResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) FindBySource(_ context.Context, source string) ([]domain.VectorRecord, error) {
	var out []domain.VectorRecord
	for _, r := range f.records {
		if r.Chunk.Metadata.Source == source {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeVectorStore) Close() error { return nil }

func TestShowReturnsFileInfoWhenFound(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	filePath := filepath.Join(root, "docs", "readme.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o644))

	registry := index.CollectionRegistry{
		Version: index.SchemaVersionV1,
		Collections: []index.CollectionEntry{
			{
				CollectionID: "abc123",
				RootIdentity: jcpaths.NormalizeStoredPath(root),
				RootDir:      root,
				FileType:     "markdown",
				UpdatedAt:    time.Now().UTC(),
			},
		},
	}

	store := &fakeVectorStore{
		config: domain.StoreConfig{
			CollectionID: "abc123",
			RootDir:      root,
			Provider:     "ollama",
			Model:        "bge-m3",
			VectorDim:    1024,
			FileType:     "markdown",
		},
		records: []domain.VectorRecord{
			{
				Chunk: domain.Chunk{
					ID: "chunk-1",
					Metadata: domain.ChunkMetadata{
						Source:   "docs/readme.md",
						RelPath:  "docs/readme.md",
						FileName: "readme.md",
						DocType:  "markdown",
						FileHash: "abc",
						Tags:     []string{"intro", "guide"},
						Title:    "Readme",
					},
					Content: "hello world",
				},
				Vector: make([]float32, 1024),
			},
		},
	}

	service := NewService(Dependencies{
		LoadCollections: func(_ string) (index.CollectionRegistry, error) { return registry, nil },
		LoadIndex: func(_ string) (index.Snapshot, error) {
			return index.Snapshot{
				Config: domain.StoreConfig{
					CollectionID: "abc123",
					RootDir:      root,
					Provider:     "ollama",
					Model:        "bge-m3",
					VectorDim:    1024,
					FileType:     "markdown",
				},
				Files: []domain.FileState{
					{
						RelPath:    "docs/readme.md",
						FilePath:   filePath,
						FileName:   "readme.md",
						DocType:    "markdown",
						FileHash:   "abc",
						ChunkCount: 1,
						ChunkIDs:   []string{"chunk-1"},
					},
				},
			}, nil
		},
		GetVectorStore: func(_ string) (reg.VectorStoreFactory, error) {
			return reg.VectorStoreFactory(func(_ domain.StoreConfig) (domain.VectorStore, error) { return store, nil }), nil
		},
		VectorStore: "lancedb",
	})

	result, err := service.Run(context.Background(), Request{FilePath: filePath, DataDir: dataDir})
	require.NoError(t, err)
	require.True(t, result.Found)
	require.Equal(t, "docs/readme.md", result.File.RelPath)
	require.Equal(t, "readme.md", result.File.FileName)
	require.Equal(t, "markdown", result.File.DocType)
	require.Equal(t, "abc123", result.Collection.CollectionID)
	require.Equal(t, "ollama", result.Collection.Provider)
	require.Equal(t, "bge-m3", result.Collection.Model)
	require.Equal(t, 1024, result.Collection.VectorDim)
	require.Len(t, result.Chunks, 1)
	require.Equal(t, "chunk-1", result.Chunks[0].ChunkID)
	require.Equal(t, 1024, result.Chunks[0].VectorLen)
	require.Equal(t, []string{"intro", "guide"}, result.Chunks[0].Tags)
	require.Equal(t, "Readme", result.Chunks[0].Title)
}

func TestShowReturnsNotFoundWhenFileMissing(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	registry := index.CollectionRegistry{
		Version: index.SchemaVersionV1,
		Collections: []index.CollectionEntry{
			{
				CollectionID: "abc123",
				RootIdentity: jcpaths.NormalizeStoredPath(root),
				RootDir:      root,
				FileType:     "markdown",
				UpdatedAt:    time.Now().UTC(),
			},
		},
	}

	store := &fakeVectorStore{records: nil}

	service := NewService(Dependencies{
		LoadCollections: func(_ string) (index.CollectionRegistry, error) { return registry, nil },
		GetVectorStore: func(_ string) (reg.VectorStoreFactory, error) {
			return reg.VectorStoreFactory(func(_ domain.StoreConfig) (domain.VectorStore, error) { return store, nil }), nil
		},
		VectorStore: "lancedb",
	})

	result, err := service.Run(context.Background(), Request{FilePath: filepath.Join(root, "missing.md"), DataDir: dataDir})
	require.NoError(t, err)
	require.False(t, result.Found)
}

func TestShowReturnsNotFoundWhenNoCollections(t *testing.T) {
	dataDir := t.TempDir()

	service := NewService(Dependencies{
		LoadCollections: func(_ string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{}, os.ErrNotExist
		},
		VectorStore: "lancedb",
	})

	result, err := service.Run(context.Background(), Request{FilePath: "/some/file.md", DataDir: dataDir})
	require.NoError(t, err)
	require.False(t, result.Found)
}

func TestShowReturnsErrorForDirectory(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()

	service := NewService(Dependencies{
		VectorStore: "lancedb",
	})

	_, err := service.Run(context.Background(), Request{FilePath: root, DataDir: dataDir})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is a directory")
}

func TestShowPropagatesLoadError(t *testing.T) {
	dataDir := t.TempDir()
	filePath := filepath.Join(t.TempDir(), "file.md")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	service := NewService(Dependencies{
		LoadCollections: func(_ string) (index.CollectionRegistry, error) {
			return index.CollectionRegistry{}, errors.New("boom")
		},
		VectorStore: "lancedb",
	})

	_, err := service.Run(context.Background(), Request{FilePath: filePath, DataDir: dataDir})
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}
