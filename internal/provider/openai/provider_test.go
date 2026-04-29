package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestProviderRegistersFactory(t *testing.T) {
	t.Parallel()

	factory, err := registry.GetProvider(Name)
	require.NoError(t, err)

	provider, err := factory(domain.ProviderConfig{Name: Name, Options: map[string]string{OptionAPIKey: "test-key"}})
	require.NoError(t, err)
	require.Equal(t, Name, provider.Name())

	embedder, err := provider.NewEmbedder(domain.ModelSpec{})
	require.NoError(t, err)
	require.Equal(t, DefaultModel, embedder.Model().Name)
	require.Equal(t, Name, embedder.Model().Provider)
	require.Equal(t, DefaultDim, embedder.Model().Dimensions)
}

func TestProviderAcceptsExplicitOptions(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(t, domain.ProviderConfig{Name: Name, Options: map[string]string{
		OptionBaseURL:   "https://example.test/v1/",
		OptionAPIKey:    "sk-test",
		OptionBatchSize: "2",
		OptionTimeout:   "45s",
		OptionDimension: "512",
	}}, fakeEmbeddingClient{})

	require.Equal(t, "https://example.test/v1", provider.baseURL)
	require.Equal(t, "sk-test", provider.apiKey)
	require.Equal(t, 2, provider.batchSize)
	require.Equal(t, 45*time.Second, provider.timeout)
	require.Equal(t, 512, provider.dimension)
}

func TestEmbedUsesFakeClientAndBatches(t *testing.T) {
	t.Parallel()

	var requests []embedAPIRequest
	provider := newTestProvider(t, domain.ProviderConfig{Name: Name, Options: map[string]string{
		OptionAPIKey:    "sk-test",
		OptionBatchSize: "2",
		OptionDimension: "3",
	}}, fakeEmbeddingClient{
		embedFunc: func(ctx context.Context, baseURL string, apiKey string, request embedAPIRequest) (embedAPIResponse, error) {
			requests = append(requests, request)
			response := embedAPIResponse{Data: make([]embeddingData, 0, len(request.Input))}
			for index := range request.Input {
				response.Data = append(response.Data, embeddingData{Index: index, Embedding: []float32{float32(index + len(requests)), float32(len(request.Input)), float32(request.Dimensions)}})
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
	require.Equal(t, 3, requests[0].Dimensions)
	require.Equal(t, []domain.Embedding{
		{ChunkID: "chunk-1", Vector: []float32{1, 2, 3}},
		{ChunkID: "chunk-2", Vector: []float32{2, 2, 3}},
		{ChunkID: "chunk-3", Vector: []float32{2, 1, 3}},
	}, result)
}

func TestHTTPEmbeddingClientMapsErrors(t *testing.T) {
	t.Parallel()

	client := &httpEmbeddingClient{client: fakeHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	}}}
	_, err := client.Embed(context.Background(), "https://api.openai.com/v1", "sk-test", embedAPIRequest{Model: DefaultModel, Input: []string{"hello"}})
	require.ErrorIs(t, err, ErrServiceUnavailable)

	unauthorized := &httpEmbeddingClient{client: fakeHTTPClient{doFunc: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusUnauthorized, `{"error":{"message":"bad key"}}`), nil
	}}}
	_, err = unauthorized.Embed(context.Background(), "https://api.openai.com/v1", "bad", embedAPIRequest{Model: DefaultModel, Input: []string{"hello"}})
	require.ErrorIs(t, err, ErrUnauthorized)
}

type fakeEmbeddingClient struct {
	embedFunc func(ctx context.Context, baseURL string, apiKey string, request embedAPIRequest) (embedAPIResponse, error)
}

func (f fakeEmbeddingClient) Embed(ctx context.Context, baseURL string, apiKey string, request embedAPIRequest) (embedAPIResponse, error) {
	if f.embedFunc == nil {
		return embedAPIResponse{}, nil
	}
	return f.embedFunc(ctx, baseURL, apiKey, request)
}

type fakeHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (f fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if f.doFunc == nil {
		return nil, errors.New("unexpected nil fakeHTTPClient.doFunc")
	}
	return f.doFunc(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
	}
}

func newTestProvider(t *testing.T, config domain.ProviderConfig, client embeddingClient) *Provider {
	t.Helper()
	provider, err := newProvider(config, client)
	require.NoError(t, err)
	return provider
}
