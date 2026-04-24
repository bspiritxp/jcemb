package ollama

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistersFactory(t *testing.T) {
	t.Parallel()

	factory, err := registry.GetProvider(Name)
	require.NoError(t, err)

	provider, err := factory(domain.ProviderConfig{Name: Name})
	require.NoError(t, err)
	require.Equal(t, Name, provider.Name())

	embedder, err := provider.NewEmbedder(domain.ModelSpec{})
	require.NoError(t, err)
	require.Equal(t, DefaultModel, embedder.Model().Name)
	require.Equal(t, Name, embedder.Model().Provider)
}

func TestProviderUsesDefaultsAndNormalizesOptions(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{Name: Name}, fakeEmbeddingClient{})
	defaults := config.Defaults().Ollama

	require.Equal(t, defaults.URL, provider.baseURL)
	require.Equal(t, defaults.BatchSize, provider.batchSize)
	require.Equal(t, defaults.Timeout, provider.timeout)
	require.Equal(t, defaults.URL, provider.config.Options[OptionOllamaURL])
	require.Equal(t, fmt.Sprintf("%d", defaults.BatchSize), provider.config.Options[OptionBatchSize])
	require.Equal(t, defaults.Timeout.String(), provider.config.Options[OptionTimeout])
}

func TestProviderAcceptsExplicitOptions(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{
		Name: Name,
		Options: map[string]string{
			OptionOllamaURL: "http://example.test:11434/",
			OptionBatchSize: "2",
			OptionTimeout:   "45s",
		},
	}, fakeEmbeddingClient{})

	require.Equal(t, "http://example.test:11434", provider.baseURL)
	require.Equal(t, 2, provider.batchSize)
	require.Equal(t, 45*time.Second, provider.timeout)
	assertProviderConfig(t, provider.config, map[string]string{
		OptionOllamaURL: "http://example.test:11434",
		OptionBatchSize: "2",
		OptionTimeout:   "45s",
	})
	_, err := provider.NewEmbedder(domain.ModelSpec{Provider: "openai", Name: "text-embedding-3-large"})
	require.EqualError(t, err, `ollama: model provider must be "ollama"`)
}

func TestEmbedUsesFakeClientAndBatches(t *testing.T) {
	t.Parallel()

	var requests []embedAPIRequest
	provider := newTestProvider(t, domain.ProviderConfig{
		Name: Name,
		Options: map[string]string{
			OptionBatchSize: "2",
		},
	}, fakeEmbeddingClient{
		embedFunc: func(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
			requests = append(requests, request)
			response := embedAPIResponse{Embeddings: make([][]float32, 0, len(request.Input))}
			for index := range request.Input {
				response.Embeddings = append(response.Embeddings, []float32{float32(index + len(requests)), float32(len(request.Input))})
			}
			return response, nil
		},
	})

	embedder, err := provider.NewEmbedder(domain.ModelSpec{})
	require.NoError(t, err)

	result, err := embedder.Embed(context.Background(), domain.EmbedRequest{
		Recipe: domain.EmbedRecipe{
			Provider: domain.ProviderConfig{Name: Name},
			Model:    domain.ModelSpec{Provider: Name, Name: DefaultModel},
		},
		Inputs: []domain.EmbedInput{{ChunkID: "chunk-1", Text: "alpha"}, {ChunkID: "chunk-2", Text: "beta"}, {ChunkID: "chunk-3", Text: "gamma"}},
	})
	require.NoError(t, err)
	require.Len(t, requests, 2)
	require.Equal(t, []string{"alpha", "beta"}, requests[0].Input)
	require.Equal(t, []string{"gamma"}, requests[1].Input)
	require.Equal(t, []domain.Embedding{
		{ChunkID: "chunk-1", Vector: []float32{1, 2}},
		{ChunkID: "chunk-2", Vector: []float32{2, 2}},
		{ChunkID: "chunk-3", Vector: []float32{2, 1}},
	}, result)
}

func TestEmbedMapsServiceUnavailableError(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{Name: Name}, fakeEmbeddingClient{
		embedFunc: func(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
			return embedAPIResponse{}, fmt.Errorf("%w: dial tcp 127.0.0.1:11434: connect: connection refused", ErrServiceUnavailable)
		},
	})

	embedder, err := provider.NewEmbedder(domain.ModelSpec{})
	require.NoError(t, err)

	_, err = embedder.Embed(context.Background(), domain.EmbedRequest{Inputs: []domain.EmbedInput{{ChunkID: "chunk-1", Text: "alpha"}}})
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Contains(t, err.Error(), "connection refused")
}

func TestEmbedMapsModelNotFoundError(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{Name: Name}, fakeEmbeddingClient{
		embedFunc: func(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
			return embedAPIResponse{}, fmt.Errorf("%w: model %q was not found", ErrModelNotFound, request.Model)
		},
	})

	embedder, err := provider.NewEmbedder(domain.ModelSpec{Name: "missing-model"})
	require.NoError(t, err)

	_, err = embedder.Embed(context.Background(), domain.EmbedRequest{Inputs: []domain.EmbedInput{{ChunkID: "chunk-1", Text: "alpha"}}})
	require.ErrorIs(t, err, ErrModelNotFound)
	require.Contains(t, err.Error(), "missing-model")
}

func TestEmbedRejectsInvalidResponse(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{Name: Name}, fakeEmbeddingClient{
		embedFunc: func(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
			return embedAPIResponse{Embeddings: [][]float32{{1, 2, 3}}}, nil
		},
	})

	embedder, err := provider.NewEmbedder(domain.ModelSpec{})
	require.NoError(t, err)

	_, err = embedder.Embed(context.Background(), domain.EmbedRequest{Inputs: []domain.EmbedInput{{ChunkID: "chunk-1", Text: "alpha"}, {ChunkID: "chunk-2", Text: "beta"}}})
	require.ErrorIs(t, err, ErrInvalidResponse)
	require.EqualError(t, err, "ollama: invalid response: expected=2 actual=1")
}

func TestHTTPEmbeddingClientMapsErrors(t *testing.T) {
	t.Parallel()

	client := &httpEmbeddingClient{client: fakeHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
		},
	}}

	_, err := client.Embed(context.Background(), "http://localhost:11434", embedAPIRequest{Model: DefaultModel, Input: []string{"hello"}})
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Contains(t, err.Error(), "connection refused")
	_ = err
}

func TestHTTPEmbeddingClientMapsModelNotFoundAndInvalidResponse(t *testing.T) {
	t.Parallel()

	notFoundClient := &httpEmbeddingClient{client: fakeHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(t, http.StatusNotFound, `{"error":"model 'missing' not found"}`), nil
		},
	}}

	_, err := notFoundClient.Embed(context.Background(), "http://localhost:11434", embedAPIRequest{Model: "missing", Input: []string{"hello"}})
	require.ErrorIs(t, err, ErrModelNotFound)
	require.Contains(t, err.Error(), "model 'missing' not found")

	invalidClient := &httpEmbeddingClient{client: fakeHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(t, http.StatusOK, `{"embeddings":[]}`), nil
		},
	}}

	_, err = invalidClient.Embed(context.Background(), "http://localhost:11434", embedAPIRequest{Model: DefaultModel, Input: []string{"hello"}})
	require.ErrorIs(t, err, ErrInvalidResponse)
	require.Contains(t, err.Error(), "embeddings field is empty")
}

type fakeEmbeddingClient struct {
	embedFunc func(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error)
}

type fakeHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (f fakeEmbeddingClient) Embed(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
	if f.embedFunc == nil {
		return embedAPIResponse{}, nil
	}
	return f.embedFunc(ctx, baseURL, request)
}

func (f fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if f.doFunc == nil {
		return nil, errors.New("unexpected nil fakeHTTPClient.doFunc")
	}
	return f.doFunc(req)
}

func newTestProvider(t *testing.T, providerConfig domain.ProviderConfig, client embeddingClient) *Provider {
	t.Helper()

	provider, err := newProvider(providerConfig, client)
	require.NoError(t, err)
	return provider
}

func assertProviderConfig(t *testing.T, config domain.ProviderConfig, expectedOptions map[string]string) {
	t.Helper()

	require.Equal(t, Name, config.Name)
	require.Equal(t, expectedOptions, config.Options)
}

func jsonResponse(t *testing.T, statusCode int, body string) *http.Response {
	t.Helper()

	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
