package registry

import (
	"context"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTagExtractorRegistryRegisterGetListAndReset(t *testing.T) {
	t.Parallel()

	registry := NewTagExtractorRegistry()
	factoryA := domain.TagExtractorFactory(func(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
		return fakeTagExtractor{name: cfg.Provider}, nil
	})
	factoryB := domain.TagExtractorFactory(func(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
		return fakeTagExtractor{name: cfg.Model}, nil
	})

	require.NoError(t, registry.Register("zeta", factoryA))
	require.NoError(t, registry.Register("alpha", factoryB))

	require.Equal(t, []string{"alpha", "zeta"}, registry.List())

	resolved, err := registry.Get("zeta")
	require.NoError(t, err)
	require.NotNil(t, resolved)

	extractor, err := resolved(domain.TagExtractorConfig{Provider: "zeta"})
	require.NoError(t, err)
	require.IsType(t, fakeTagExtractor{}, extractor)

	registry.Reset()
	require.Empty(t, registry.List())
	_, err = registry.Get("zeta")
	require.EqualError(t, err, `registry: unknown tag extractor "zeta"`)
}

func TestTagExtractorRegistryRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()

	registry := NewTagExtractorRegistry()
	factory := domain.TagExtractorFactory(func(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
		return fakeTagExtractor{name: cfg.Provider}, nil
	})

	require.NoError(t, registry.Register("ollama", factory))
	err := registry.Register("ollama", factory)
	require.EqualError(t, err, `registry: tag extractor "ollama" already registered`)
}

func TestPackageLevelTagExtractorRegistryCanBeResetForIsolation(t *testing.T) {
	t.Cleanup(func() {
		ResetTagExtractors()
	})

	ResetTagExtractors()

	require.NoError(t, RegisterTagExtractor("ollama", func(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
		return fakeTagExtractor{name: cfg.Provider}, nil
	}))

	require.Equal(t, []string{"ollama"}, ListTagExtractors())

	resolved, err := GetTagExtractor("ollama")
	require.NoError(t, err)

	extractor, err := resolved(domain.TagExtractorConfig{Provider: "ollama"})
	require.NoError(t, err)
	require.Equal(t, "ollama", extractor.(fakeTagExtractor).name)

	ResetTagExtractors()
	require.Empty(t, ListTagExtractors())
	_, err = GetTagExtractor("ollama")
	require.EqualError(t, err, `registry: unknown tag extractor "ollama"`)
}

func TestTagExtractorRegistryMustRegisterPanicsOnDuplicate(t *testing.T) {
	t.Parallel()

	registry := NewTagExtractorRegistry()
	factory := domain.TagExtractorFactory(func(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
		return fakeTagExtractor{name: cfg.Provider}, nil
	})

	registry.MustRegister("openai", factory)

	require.PanicsWithError(t, `registry: tag extractor "openai" already registered`, func() {
		registry.MustRegister("openai", factory)
	})
}

type fakeTagExtractor struct {
	name string
}

func (e fakeTagExtractor) Extract(context.Context, domain.TagExtractRequest) (domain.TagExtractResult, error) {
	return domain.TagExtractResult{}, nil
}
