package embed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/splitter/markdown"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	"github.com/stretchr/testify/require"
)

func TestServiceRunCreatesVectorDBOnFirstEmbed(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"docs/a.md": "---\ntitle: Alpha\ntags: [go]\n---\n\n# Intro\n\nAlpha body.",
		"docs/b.md": "# Beta\n\nBeta body.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(provider)

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 2,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 2, Updated: 2}, result.Summary)
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, index.ConfigFileName))
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, index.IndexFileName))
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, "lancedb.records.json"))
	require.Greater(t, provider.CallCount(), 0)

	snapshot, loadErr := index.Load(rootDir)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 2)
	require.Equal(t, 3, snapshot.Config.VectorDim)
}

func TestServiceRunSkipsUnchangedThenReembedsOnRecipeChangeAndForce(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nHello world.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(provider)

	_, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	firstCalls := provider.CallCount()

	second, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Skipped: 1}, second.Summary)
	require.Equal(t, firstCalls, provider.CallCount())

	third, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       "custom-model",
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Updated: 1}, third.Summary)
	require.Greater(t, provider.CallCount(), firstCalls)
	thirdCalls := provider.CallCount()

	fourth, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       "custom-model",
		Recursive:   true,
		Force:       true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Updated: 1}, fourth.Summary)
	require.Greater(t, provider.CallCount(), thirdCalls)
}

func TestServiceRunReconcilesDeletedFiles(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"docs/a.md": "# Alpha\n\nAlpha body.",
		"docs/b.md": "# Beta\n\nBeta body.",
	})
	service := newTestService(newFakeProvider(nil))

	_, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 2,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(rootDir, "docs", "b.md")))
	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 2,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Skipped: 1, Deleted: 1}, result.Summary)
	require.Equal(t, []string{"docs/b.md"}, result.Deleted)

	snapshot, loadErr := index.Load(rootDir)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, "docs/a.md", snapshot.Files[0].RelPath)

	store, openErr := lancedb.New(domain.StoreConfig{
		RootDir:   rootDir,
		Namespace: lancedb.Name,
		Provider:  ollama.Name,
		Model:     ollama.DefaultModel,
		Splitter:  markdown.Name,
		VectorDim: snapshot.Config.VectorDim,
		DBVersion: lancedb.DBVersion,
		CreatedAt: snapshot.Config.CreatedAt,
	})
	require.NoError(t, openErr)
	results, searchErr := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0, 0}, Limit: 10})
	require.NoError(t, searchErr)
	require.Len(t, results, 1)
	require.Equal(t, "docs/a.md", results[0].Chunk.Metadata.RelPath)
}

func TestServiceRunReturnsNonZeroErrorWhenSingleFileFails(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"docs/good.md": "# Good\n\nGood body.",
		"docs/bad.md":  "# Bad\n\nThis file should fail.",
	})
	provider := newFakeProvider(map[string]error{"docs/bad.md": errors.New("boom")})
	service := newTestService(provider)

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 2,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.Error(t, err)
	runErr := &RunError{}
	require.ErrorAs(t, err, &runErr)
	require.Equal(t, 1, result.Summary.Errors)
	require.Equal(t, 1, result.Summary.Updated)
	require.Len(t, result.Failures, 1)
	require.Equal(t, "docs/bad.md", result.Failures[0].RelPath)
	require.Contains(t, result.Failures[0].Err.Error(), "boom")

	snapshot, loadErr := index.Load(rootDir)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, "docs/good.md", snapshot.Files[0].RelPath)
}

func newTestService(provider *fakeProvider) *Service {
	return NewService(Dependencies{
		GetProvider: func(name string) (registry.ProviderFactory, error) {
			if name != ollama.Name {
				return nil, errors.New("unknown provider")
			}
			return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
				provider.name = config.Name
				return provider, nil
			}, nil
		},
		GetSplitter: func(name string) (registry.SplitterFactory, error) {
			return func(spec domain.SplitterSpec) (domain.Splitter, error) {
				return markdown.New(spec)
			}, nil
		},
		GetVectorStore: func(name string) (registry.VectorStoreFactory, error) {
			return func(config domain.StoreConfig) (domain.VectorStore, error) {
				return lancedb.New(config)
			}, nil
		},
	})
}

type fakeProvider struct {
	name     string
	failures map[string]error
	mu       sync.Mutex
	calls    int
}

type fakeEmbedder struct {
	provider *fakeProvider
	model    domain.ModelSpec
}

func newFakeProvider(failures map[string]error) *fakeProvider {
	return &fakeProvider{failures: failures}
}

func (p *fakeProvider) Name() string {
	return ollama.Name
}

func (p *fakeProvider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	if strings.TrimSpace(model.Provider) == "" {
		model.Provider = ollama.Name
	}
	if strings.TrimSpace(model.Name) == "" {
		model.Name = ollama.DefaultModel
	}
	return &fakeEmbedder{provider: p, model: model}, nil
}

func (p *fakeProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (e *fakeEmbedder) Provider() string {
	return ollama.Name
}

func (e *fakeEmbedder) Model() domain.ModelSpec {
	return e.model
}

func (e *fakeEmbedder) Embed(_ context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	e.provider.mu.Lock()
	defer e.provider.mu.Unlock()
	e.provider.calls++
	for _, input := range request.Inputs {
		if failure, ok := e.provider.failures[input.Metadata.RelPath]; ok {
			return nil, failure
		}
	}
	result := make([]domain.Embedding, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		length := float32(len([]rune(input.Text)))
		result = append(result, domain.Embedding{ChunkID: input.ChunkID, Vector: []float32{1, float32(input.Metadata.ChunkIndex), length}})
	}
	return result, nil
}

func writeDocs(t *testing.T, files map[string]string) string {
	t.Helper()
	rootDir := t.TempDir()
	for relPath, content := range files {
		absolutePath := filepath.Join(rootDir, filepath.FromSlash(relPath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), 0o755))
		require.NoError(t, os.WriteFile(absolutePath, []byte(content), 0o644))
	}
	return rootDir
}
