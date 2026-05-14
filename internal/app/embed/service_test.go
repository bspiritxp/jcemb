package embed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
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
	service := newTestService(t, provider)

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
	globalDir := collectionDataDir(rootDir, service.deps.ResolveAppPaths)
	require.NoDirExists(t, filepath.Join(rootDir, index.DirectoryName))
	require.FileExists(t, filepath.Join(globalDir, index.DirectoryName, index.ConfigFileName))
	require.FileExists(t, filepath.Join(globalDir, index.DirectoryName, index.IndexFileName))
	require.FileExists(t, filepath.Join(globalDir, index.DirectoryName, "lancedb.records.json"))
	require.Greater(t, provider.CallCount(), 0)

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
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
	service := newTestService(t, provider)

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
	service := newTestService(t, newFakeProvider(nil))

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
	require.Equal(t, []string{filepath.ToSlash(filepath.Join(rootDir, "docs", "b.md"))}, result.Deleted)

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(rootDir, "docs", "a.md")), snapshot.Files[0].RelPath)

	store, openErr := lancedb.New(domain.StoreConfig{
		RootDir:      rootDir,
		DataDir:      snapshot.Config.DataDir,
		CollectionID: snapshot.Config.CollectionID,
		RootIdentity: snapshot.Config.RootIdentity,
		Namespace:    lancedb.Name,
		Provider:     ollama.Name,
		Model:        ollama.DefaultModel,
		Splitter:     markdown.Name,
		VectorDim:    snapshot.Config.VectorDim,
		DBVersion:    lancedb.DBVersion,
		CreatedAt:    snapshot.Config.CreatedAt,
	})
	require.NoError(t, openErr)
	results, searchErr := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0, 0}, Limit: 10})
	require.NoError(t, searchErr)
	require.Len(t, results, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(rootDir, "docs", "a.md")), results[0].Chunk.Metadata.RelPath)
}

func TestServiceRunReturnsNonZeroErrorWhenSingleFileFails(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"docs/good.md": "# Good\n\nGood body.",
		"docs/bad.md":  "# Bad\n\nThis file should fail.",
	})
	provider := newFakeProvider(map[string]error{filepath.ToSlash(filepath.Join(rootDir, "docs", "bad.md")): errors.New("boom")})
	service := newTestService(t, provider)

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
	require.Equal(t, filepath.ToSlash(filepath.Join(rootDir, "docs", "bad.md")), result.Failures[0].RelPath)
	require.Contains(t, result.Failures[0].Err.Error(), "boom")

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(rootDir, "docs", "good.md")), snapshot.Files[0].RelPath)
}

func TestServiceRunUsesStorageMetadataAuthorityWhenCompatibilityManifestsAreRemoved(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nHello world.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(t, provider)

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
	globalDir := collectionDataDir(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.ConfigFileName)))
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.IndexFileName)))

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Skipped: 1}, result.Summary)
	require.Equal(t, firstCalls, provider.CallCount())

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, loadErr)
	require.Len(t, snapshot.Files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(rootDir, "guide.md")), snapshot.Files[0].RelPath)
	require.False(t, snapshot.Files[0].ModTime.IsZero())
}

func TestServiceRunDetectsStoredCollectionMetadataMismatchWithoutCompatibilityManifests(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nHello world.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(t, provider)

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
	globalDir := collectionDataDir(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.ConfigFileName)))
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.IndexFileName)))

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       "custom-model",
		Recursive:   true,
	})
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.Equal(t, Summary{Processed: 1, Updated: 1}, result.Summary)
	require.Greater(t, provider.CallCount(), firstCalls)

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, loadErr)
	require.Equal(t, "custom-model", snapshot.Config.Model)
}

func TestServiceRunReembedsChangedFileFromStorageMetadataWithoutCompatibilityManifests(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nHello world.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(t, provider)

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
	firstSnapshot, err := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, err)
	require.Len(t, firstSnapshot.Files, 1)

	globalDir := collectionDataDir(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.ConfigFileName)))
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.IndexFileName)))

	filePath := filepath.Join(rootDir, "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n\nHello storage metadata world."), 0o644))
	updatedModTime := firstSnapshot.Files[0].ModTime.Add(2 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, updatedModTime, updatedModTime))

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Updated: 1}, result.Summary)
	require.Greater(t, provider.CallCount(), firstCalls)
	require.NoDirExists(t, filepath.Join(rootDir, index.DirectoryName))

	secondSnapshot, err := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, err)
	require.Len(t, secondSnapshot.Files, 1)
	require.NotEqual(t, firstSnapshot.Files[0].FileHash, secondSnapshot.Files[0].FileHash)
	require.Equal(t, updatedModTime.UTC(), secondSnapshot.Files[0].ModTime)
}

func TestServiceRunSkipsTouchedFileWhenStoredHashIsUnchangedWithoutCompatibilityManifests(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nHello world.",
	})
	provider := newFakeProvider(nil)
	service := newTestService(t, provider)

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
	firstSnapshot, err := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, err)
	require.Len(t, firstSnapshot.Files, 1)

	globalDir := collectionDataDir(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.ConfigFileName)))
	require.NoError(t, os.Remove(filepath.Join(globalDir, index.DirectoryName, index.IndexFileName)))

	filePath := filepath.Join(rootDir, "guide.md")
	touchedModTime := firstSnapshot.Files[0].ModTime.Add(90 * time.Minute)
	require.NoError(t, os.Chtimes(filePath, touchedModTime, touchedModTime))

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		Recursive:   true,
	})
	require.NoError(t, err)
	require.Equal(t, Summary{Processed: 1, Skipped: 1}, result.Summary)
	require.Equal(t, firstCalls, provider.CallCount())
	require.NoDirExists(t, filepath.Join(rootDir, index.DirectoryName))

	secondSnapshot, err := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, err)
	require.Len(t, secondSnapshot.Files, 1)
	require.Equal(t, firstSnapshot.Files[0].FileHash, secondSnapshot.Files[0].FileHash)
	require.Equal(t, firstSnapshot.Files[0].ModTime, secondSnapshot.Files[0].ModTime)
}

func TestServiceRunWiresTagExtractorIntoMarkdownRuntimePath(t *testing.T) {
	t.Parallel()

	rootDir := writeDocs(t, map[string]string{
		"guide.md": "# Guide\n\nSemantic topic document.",
	})
	provider := newFakeProvider(nil)
	extractor := &capturingTagExtractor{tags: []string{"topic-a", "topic-b"}}
	service := newTestServiceWithTagExtractor(t, provider, extractor)

	result, err := service.Run(context.Background(), Request{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    ollama.Name,
		Model:       ollama.DefaultModel,
		TagExtractor: domain.TagExtractorConfig{
			Provider:      "openai",
			Model:         "gpt-4.1-mini",
			Options:       map[string]string{"openai_api_key": "sk-test", "openai_base_url": "https://example.test/v1"},
			Timeout:       45 * time.Second,
			MaxTags:       6,
			MinTagLen:     2,
			MaxTagLen:     32,
			SkipIfHasYAML: true,
		},
		Recursive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Recipe.TagExtractor)
	require.Equal(t, "openai", result.Recipe.TagExtractor.Provider)
	require.Equal(t, "gpt-4.1-mini", result.Recipe.TagExtractor.Model)
	require.Equal(t, 6, result.Recipe.TagExtractor.MaxTags)
	require.Equal(t, 45*time.Second, extractor.config.Timeout)
	require.Equal(t, "sk-test", extractor.config.Options["openai_api_key"])
	require.Equal(t, "https://example.test/v1", extractor.config.Options["openai_base_url"])

	snapshot, loadErr := loadSnapshotForTest(rootDir, service.deps.ResolveAppPaths)
	require.NoError(t, loadErr)
	store, openErr := lancedb.New(domain.StoreConfig{
		RootDir:         rootDir,
		DataDir:         snapshot.Config.DataDir,
		CollectionID:    snapshot.Config.CollectionID,
		RootIdentity:    snapshot.Config.RootIdentity,
		Namespace:       lancedb.Name,
		Provider:        snapshot.Config.Provider,
		ProviderOptions: snapshot.Config.ProviderOptions,
		Model:           snapshot.Config.Model,
		Splitter:        snapshot.Config.Splitter,
		VectorDim:       snapshot.Config.VectorDim,
		DBVersion:       lancedb.DBVersion,
		CreatedAt:       snapshot.Config.CreatedAt,
	})
	require.NoError(t, openErr)
	defer func() { _ = store.Close() }()
	records, findErr := store.FindBySource(context.Background(), filepath.ToSlash(filepath.Join(rootDir, "guide.md")))
	require.NoError(t, findErr)
	require.NotEmpty(t, records)
	require.Equal(t, []string{"topic-a", "topic-b"}, records[0].SemanticTags)
	require.NotNil(t, records[0].TagVector)
}

func TestFilterExtensionMapNormalizesAndValidatesRequestedExtensions(t *testing.T) {
	t.Parallel()

	registered := map[string]string{
		".jpg": "image",
		".md":  "markdown",
		".png": "image",
	}

	filtered, err := filterExtensionMap(registered, []string{"md,.PNG", ".jpg", "md"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		".jpg": "image",
		".md":  "markdown",
		".png": "image",
	}, filtered)

	_, err = filterExtensionMap(registered, []string{".txt"})
	require.ErrorContains(t, err, `unsupported extension ".txt"`)
}

func newTestService(t *testing.T, provider *fakeProvider) *Service {
	t.Helper()
	dataRoot := t.TempDir()
	return NewService(Dependencies{
		ResolveAppPaths: func() (jcpaths.AppPaths, error) {
			return jcpaths.AppPaths{DataRoot: dataRoot, ConfigFile: filepath.Join(dataRoot, "jcemb.json")}, nil
		},
		LoadIndex: func(rootDir string) (index.Snapshot, error) {
			return loadSnapshotFromDataRoot(rootDir, dataRoot)
		},
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

func newTestServiceWithTagExtractor(t *testing.T, provider *fakeProvider, extractor *capturingTagExtractor) *Service {
	t.Helper()
	dataRoot := t.TempDir()
	return NewService(Dependencies{
		ResolveAppPaths: func() (jcpaths.AppPaths, error) {
			return jcpaths.AppPaths{DataRoot: dataRoot, ConfigFile: filepath.Join(dataRoot, "jcemb.json")}, nil
		},
		LoadIndex: func(rootDir string) (index.Snapshot, error) {
			return loadSnapshotFromDataRoot(rootDir, dataRoot)
		},
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
		GetTagExtractor: func(name string) (domain.TagExtractorFactory, error) {
			return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
				extractor.config = config
				return extractor, nil
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

type capturingTagExtractor struct {
	config domain.TagExtractorConfig
	tags   []string
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

func (e *capturingTagExtractor) Extract(_ context.Context, _ domain.TagExtractRequest) (domain.TagExtractResult, error) {
	return domain.TagExtractResult{Tags: append([]string(nil), e.tags...)}, nil
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

func collectionDataDir(rootDir string, resolve func() (jcpaths.AppPaths, error)) string {
	paths, err := resolve()
	if err != nil {
		panic(err)
	}
	return jcpaths.CollectionStorageDir(paths.DataRoot, jcpaths.CollectionIDForRoot(jcpaths.NormalizeStoredPath(rootDir)))
}

func loadSnapshotForTest(rootDir string, resolve func() (jcpaths.AppPaths, error)) (index.Snapshot, error) {
	paths, err := resolve()
	if err != nil {
		return index.Snapshot{}, err
	}
	return loadSnapshotFromDataRoot(rootDir, paths.DataRoot)
}

func loadSnapshotFromDataRoot(rootDir string, dataRoot string) (index.Snapshot, error) {
	storageRoot := jcpaths.CollectionStorageDir(dataRoot, jcpaths.CollectionIDForRoot(jcpaths.NormalizeStoredPath(rootDir)))
	snapshot, err := index.Load(storageRoot)
	if err != nil {
		return index.Snapshot{}, err
	}
	snapshot.Config.RootDir = rootDir
	snapshot.Config.RootIdentity = jcpaths.NormalizeStoredPath(rootDir)
	snapshot.Config.CollectionID = jcpaths.CollectionIDForRoot(snapshot.Config.RootIdentity)
	snapshot.Config.DataDir = dataRoot
	return snapshot, nil
}
