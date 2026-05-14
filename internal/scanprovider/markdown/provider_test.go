package markdown

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	splitmarkdown "github.com/bspiritxp/jcemb/internal/splitter/markdown"
	"github.com/stretchr/testify/require"
)

func TestBuildRecordsLeavesSemanticTagsDisabled(t *testing.T) {
	provider := Provider{}
	request, tracker := newMarkdownScanRequest(t, markdownFixture(t, "---\ntags: [go]\n---\n\n# Intro\n\nBody text."), nil, nil)

	result, err := provider.BuildRecords(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Records)
	require.Zero(t, tracker.getterCalls)
	for _, record := range result.Records {
		require.Nil(t, record.SemanticTags)
		require.Nil(t, record.TagVector)
		require.Equal(t, []string{"go"}, record.Chunk.Metadata.Tags)
	}
}

func TestBuildRecordsReusesYAMLTagsWhenSkipIfHasYAMLIsEnabled(t *testing.T) {
	provider := Provider{}
	request, tracker := newMarkdownScanRequest(t, markdownFixture(t, "---\ntags: [go, rust]\n---\n\n# Intro\n\nBody text."), &domain.TagExtractorRecipeSpec{
		Provider:      "fake",
		Model:         "fake-model",
		MaxTags:       8,
		MinTagLen:     2,
		MaxTagLen:     32,
		SkipIfHasYAML: true,
	}, &fakeTagExtractor{result: domain.TagExtractResult{Tags: []string{"should-not-appear"}}})

	result, err := provider.BuildRecords(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Records)
	require.Zero(t, tracker.getterCalls)
	for _, record := range result.Records {
		require.Equal(t, []string{"go", "rust"}, record.SemanticTags)
		requireVectorApprox(t, record.TagVector, []float32{0.70710677, 0.70710677})
	}
}

func TestBuildRecordsExtractsSemanticTagsAndPoolsTagVector(t *testing.T) {
	provider := Provider{}
	request, tracker := newMarkdownScanRequest(t, markdownFixture(t, "# Intro\n\nBody text."), &domain.TagExtractorRecipeSpec{
		Provider:      "fake",
		Model:         "fake-model",
		MaxTags:       8,
		MinTagLen:     2,
		MaxTagLen:     32,
		SkipIfHasYAML: true,
	}, &fakeTagExtractor{result: domain.TagExtractResult{Tags: []string{"semantic1", "semantic2"}}})

	result, err := provider.BuildRecords(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Records)
	require.Equal(t, 1, tracker.getterCalls)
	require.Equal(t, 1, tracker.extractCalls)
	for _, record := range result.Records {
		require.Equal(t, []string{"semantic1", "semantic2"}, record.SemanticTags)
		requireVectorApprox(t, record.TagVector, []float32{0.70710677, 0.70710677})
	}
}

func TestBuildRecordsPassesRuntimeTagExtractorOptionsToFactory(t *testing.T) {
	provider := Provider{}
	request, tracker := newMarkdownScanRequest(t, markdownFixture(t, "# Intro\n\nBody text."), &domain.TagExtractorRecipeSpec{
		Provider:      "openai",
		Model:         "gpt-4.1-mini",
		MaxTags:       8,
		MinTagLen:     2,
		MaxTagLen:     32,
		SkipIfHasYAML: true,
	}, &fakeTagExtractor{result: domain.TagExtractResult{Tags: []string{"semantic1"}}})
	request.Config.TagExtractor = domain.TagExtractorConfig{
		Provider:      "openai",
		Model:         "gpt-4.1-mini",
		Options:       map[string]string{"openai_api_key": "sk-test", "openai_base_url": "https://example.test/v1"},
		Timeout:       45 * time.Second,
		MaxTags:       6,
		MinTagLen:     3,
		MaxTagLen:     24,
		SkipIfHasYAML: true,
	}

	result, err := provider.BuildRecords(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Records)
	require.Equal(t, 1, tracker.getterCalls)
	require.Equal(t, "openai", tracker.lastConfig.Provider)
	require.Equal(t, "gpt-4.1-mini", tracker.lastConfig.Model)
	require.Equal(t, 45*time.Second, tracker.lastConfig.Timeout)
	require.Equal(t, "sk-test", tracker.lastConfig.Options["openai_api_key"])
	require.Equal(t, "https://example.test/v1", tracker.lastConfig.Options["openai_base_url"])
	require.Equal(t, 6, tracker.lastConfig.MaxTags)
	require.Equal(t, 3, tracker.lastConfig.MinTagLen)
	require.Equal(t, 24, tracker.lastConfig.MaxTagLen)
}

func TestBuildRecordsKeepsContentVectorsWhenTagExtractionFails(t *testing.T) {
	provider := Provider{}
	request, tracker := newMarkdownScanRequest(t, markdownFixture(t, "# Intro\n\nBody text."), &domain.TagExtractorRecipeSpec{
		Provider:      "fake",
		Model:         "fake-model",
		MaxTags:       8,
		MinTagLen:     2,
		MaxTagLen:     32,
		SkipIfHasYAML: true,
	}, &fakeTagExtractor{err: errors.New("boom")})

	result, err := provider.BuildRecords(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Records)
	require.Equal(t, 1, tracker.getterCalls)
	require.Equal(t, 1, tracker.extractCalls)
	for _, record := range result.Records {
		require.NotNil(t, record.Vector)
		require.Nil(t, record.TagVector)
		require.Nil(t, record.SemanticTags)
	}
}

type requestTracker struct {
	getterCalls  int
	extractCalls int
	lastConfig   domain.TagExtractorConfig
}

type fakeEmbedderProvider struct{}

type fakeEmbedder struct{}

type fakeTagExtractor struct {
	result domain.TagExtractResult
	err    error
	onCall func()
}

func newMarkdownScanRequest(t *testing.T, path string, tagSpec *domain.TagExtractorRecipeSpec, extractor *fakeTagExtractor) (domain.ScanProviderRequest, *requestTracker) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	tracker := &requestTracker{}
	request := domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  filepath.Dir(path),
			FilePath: path,
			RelPath:  filepath.ToSlash(path),
			FileName: filepath.Base(path),
			DocType:  FileType,
			ModTime:  info.ModTime(),
		},
		Config: domain.ScanProviderConfig{
			Provider: "fake-provider",
			Model:    "fake-model",
		},
		Recipe: domain.EmbedRecipe{
			Type:         FileType,
			Version:      Version,
			Provider:     domain.ProviderConfig{Name: "fake-provider"},
			Model:        domain.ModelSpec{Provider: "fake-provider", Name: "fake-model"},
			Splitter:     domain.SplitterSpec{Name: Name, Version: Version, Options: map[string]string{splitmarkdown.ShortFileMaxCharsOption: "1000"}},
			TagExtractor: tagSpec,
		},
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
		GetProvider: func(name string) (func(domain.ProviderConfig) (domain.EmbedderProvider, error), error) {
			return func(domain.ProviderConfig) (domain.EmbedderProvider, error) {
				return fakeEmbedderProvider{}, nil
			}, nil
		},
		GetSplitter: func(name string) (func(domain.SplitterSpec) (domain.Splitter, error), error) {
			return func(spec domain.SplitterSpec) (domain.Splitter, error) {
				return splitmarkdown.New(spec)
			}, nil
		},
		GetTagExtractor: func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
			tracker.getterCalls++
			tracker.lastConfig = config
			if extractor == nil {
				return nil, errors.New("unexpected tag extractor request")
			}
			extractor.onCall = func() { tracker.extractCalls++ }
			return extractor, nil
		},
	}
	return request, tracker
}

func (fakeEmbedderProvider) Name() string {
	return "fake-provider"
}

func (fakeEmbedderProvider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	return fakeEmbedder{}, nil
}

func (fakeEmbedder) Provider() string {
	return "fake-provider"
}

func (fakeEmbedder) Model() domain.ModelSpec {
	return domain.ModelSpec{Provider: "fake-provider", Name: "fake-model"}
}

func (fakeEmbedder) Embed(_ context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	result := make([]domain.Embedding, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		result = append(result, domain.Embedding{ChunkID: input.ChunkID, Vector: fakeVectorForText(input.Text)})
	}
	return result, nil
}

func (e *fakeTagExtractor) Extract(_ context.Context, request domain.TagExtractRequest) (domain.TagExtractResult, error) {
	if e.onCall != nil {
		e.onCall()
	}
	_ = request
	if e.err != nil {
		return domain.TagExtractResult{}, e.err
	}
	return e.result, nil
}

func fakeVectorForText(text string) []float32 {
	switch strings.TrimSpace(text) {
	case "go", "semantic1":
		return []float32{1, 0}
	case "rust", "semantic2":
		return []float32{0, 1}
	default:
		return []float32{float32(len([]rune(text))), 1}
	}
}

func markdownFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func requireVectorApprox(t *testing.T, got []float32, want []float32) {
	t.Helper()
	require.Len(t, got, len(want))
	for index := range want {
		require.InDelta(t, want[index], got[index], 1e-6)
	}
}
