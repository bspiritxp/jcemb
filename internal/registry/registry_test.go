package registry

import (
	"context"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSplitterRegistryRegisterGetListAndReset(t *testing.T) {
	t.Parallel()

	registry := NewSplitterRegistry()
	splitterA := SplitterFactory(func(spec domain.SplitterSpec) (domain.Splitter, error) {
		return stubSplitter{name: spec.Name}, nil
	})
	splitterB := SplitterFactory(func(spec domain.SplitterSpec) (domain.Splitter, error) {
		return stubSplitter{name: spec.Version}, nil
	})

	require.NoError(t, registry.Register("zeta", splitterA))
	require.NoError(t, registry.Register("alpha", splitterB))

	require.Equal(t, []string{"alpha", "zeta"}, registry.List())

	resolved, err := registry.Get("zeta")
	require.NoError(t, err)
	require.NotNil(t, resolved)

	splitter, err := resolved(domain.SplitterSpec{Name: "zeta"})
	require.NoError(t, err)
	require.IsType(t, stubSplitter{}, splitter)

	registry.Reset()
	require.Empty(t, registry.List())
	_, err = registry.Get("zeta")
	require.EqualError(t, err, `registry: unknown splitter "zeta"`)
}

func TestSplitterRegistryRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()

	registry := NewSplitterRegistry()
	factory := SplitterFactory(func(spec domain.SplitterSpec) (domain.Splitter, error) {
		return stubSplitter{name: spec.Name}, nil
	})

	require.NoError(t, registry.Register("markdown", factory))
	err := registry.Register("markdown", factory)
	require.EqualError(t, err, `registry: splitter "markdown" already registered`)
}

func TestProviderRegistryUnknownKeyReturnsClearError(t *testing.T) {
	t.Parallel()

	registry := NewProviderRegistry()
	_, err := registry.Get("ollama")
	require.EqualError(t, err, `registry: unknown provider "ollama"`)
}

func TestVectorStoreRegistryMustRegisterPanicsOnDuplicate(t *testing.T) {
	t.Parallel()

	registry := NewVectorStoreRegistry()
	factory := VectorStoreFactory(func(config domain.StoreConfig) (domain.VectorStore, error) {
		return stubVectorStore{config: config}, nil
	})

	registry.MustRegister("lancedb", factory)

	require.PanicsWithError(t, `registry: vector store "lancedb" already registered`, func() {
		registry.MustRegister("lancedb", factory)
	})
}

func TestPackageLevelRegistriesCanBeResetForIsolation(t *testing.T) {
	t.Cleanup(func() {
		ResetSplitters()
		ResetProviders()
		ResetVectorStores()
	})

	ResetSplitters()
	ResetProviders()
	ResetVectorStores()

	require.NoError(t, RegisterSplitter("markdown", func(spec domain.SplitterSpec) (domain.Splitter, error) {
		return stubSplitter{name: spec.Name}, nil
	}))
	require.NoError(t, RegisterProvider("ollama", func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
		return stubProvider{name: config.Name}, nil
	}))
	require.NoError(t, RegisterVectorStore("lancedb", func(config domain.StoreConfig) (domain.VectorStore, error) {
		return stubVectorStore{config: config}, nil
	}))

	require.Equal(t, []string{"markdown"}, ListSplitters())
	require.Equal(t, []string{"ollama"}, ListProviders())
	require.Equal(t, []string{"lancedb"}, ListVectorStores())

	ResetSplitters()
	ResetProviders()
	ResetVectorStores()

	require.Empty(t, ListSplitters())
	require.Empty(t, ListProviders())
	require.Empty(t, ListVectorStores())

	_, err := GetSplitter("markdown")
	require.EqualError(t, err, `registry: unknown splitter "markdown"`)
	_, err = GetProvider("ollama")
	require.EqualError(t, err, `registry: unknown provider "ollama"`)
	_, err = GetVectorStore("lancedb")
	require.EqualError(t, err, `registry: unknown vector store "lancedb"`)
}

func TestRegistriesTrimNamesButDoNotSilentlyOverwrite(t *testing.T) {
	t.Parallel()

	providers := NewProviderRegistry()
	providerFactory := ProviderFactory(func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
		return stubProvider{name: config.Name}, nil
	})

	require.NoError(t, providers.Register(" ollama ", providerFactory))
	require.Equal(t, []string{"ollama"}, providers.List())

	resolved, err := providers.Get("ollama")
	require.NoError(t, err)

	provider, err := resolved(domain.ProviderConfig{Name: "ollama"})
	require.NoError(t, err)
	require.Equal(t, "ollama", provider.Name())

	err = providers.Register("ollama", providerFactory)
	require.EqualError(t, err, `registry: provider "ollama" already registered`)
}

type stubSplitter struct {
	name string
}

func (s stubSplitter) Split(ctx context.Context, document domain.Document) ([]domain.Chunk, error) {
	return nil, nil
}

type stubProvider struct {
	name string
}

func (p stubProvider) Name() string {
	return p.name
}

func (p stubProvider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	return stubEmbedder{provider: p.name, model: model}, nil
}

type stubEmbedder struct {
	provider string
	model    domain.ModelSpec
}

func (e stubEmbedder) Provider() string {
	return e.provider
}

func (e stubEmbedder) Model() domain.ModelSpec {
	return e.model
}

func (e stubEmbedder) Embed(ctx context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	return nil, nil
}

type stubVectorStore struct {
	config domain.StoreConfig
}

func (s stubVectorStore) Config() domain.StoreConfig {
	return s.config
}

func (s stubVectorStore) Upsert(ctx context.Context, chunks []domain.VectorRecord) error {
	return nil
}

func (s stubVectorStore) DeleteBySource(ctx context.Context, source string) error {
	return nil
}

func (s stubVectorStore) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchResult, error) {
	return nil, nil
}

func (s stubVectorStore) Close() error {
	return nil
}
