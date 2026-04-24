package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	configpkg "github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/domain"
)

const (
	Name         = "ollama"
	DefaultModel = "bge-m3"

	OptionOllamaURL = "ollama_url"
	OptionBatchSize = "batch_size"
	OptionTimeout   = "timeout"
)

var (
	ErrServiceUnavailable = errors.New("ollama: service unavailable")
	ErrModelNotFound      = errors.New("ollama: model not found")
	ErrInvalidResponse    = errors.New("ollama: invalid response")
)

type Provider struct {
	config    domain.ProviderConfig
	client    embeddingClient
	baseURL   string
	batchSize int
	timeout   time.Duration
}

type Embedder struct {
	provider  string
	model     domain.ModelSpec
	client    embeddingClient
	baseURL   string
	batchSize int
}

type embeddingClient interface {
	Embed(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error)
}

type embedAPIRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedAPIResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type apiErrorResponse struct {
	Error string `json:"error"`
}

type httpEmbeddingClient struct {
	client httpClient
}

func New(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
	return newProvider(config, newHTTPEmbeddingClient(defaultsTimeout(config)))
}

func newProvider(config domain.ProviderConfig, client embeddingClient) (*Provider, error) {
	normalized, baseURL, batchSize, timeout, err := normalizeProviderConfig(config)
	if err != nil {
		return nil, err
	}

	return &Provider{
		config:    normalized,
		client:    client,
		baseURL:   baseURL,
		batchSize: batchSize,
		timeout:   timeout,
	}, nil
}

func (p *Provider) Name() string {
	return Name
}

func (p *Provider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	normalized, err := normalizeModel(model)
	if err != nil {
		return nil, err
	}

	return &Embedder{
		provider:  Name,
		model:     normalized,
		client:    p.client,
		baseURL:   p.baseURL,
		batchSize: p.batchSize,
	}, nil
}

func (e *Embedder) Provider() string {
	return e.provider
}

func (e *Embedder) Model() domain.ModelSpec {
	return cloneModelSpec(e.model)
}

func (e *Embedder) Embed(ctx context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := validateRecipe(request.Recipe, e.model); err != nil {
		return nil, err
	}

	if len(request.Inputs) == 0 {
		return nil, nil
	}

	batchSize := e.batchSize
	if batchSize <= 0 {
		batchSize = configpkg.Defaults().Ollama.BatchSize
	}

	results := make([]domain.Embedding, 0, len(request.Inputs))
	for start := 0; start < len(request.Inputs); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		end := start + batchSize
		if end > len(request.Inputs) {
			end = len(request.Inputs)
		}

		batch := request.Inputs[start:end]
		texts := make([]string, 0, len(batch))
		for _, input := range batch {
			texts = append(texts, input.Text)
		}

		response, err := e.client.Embed(ctx, e.baseURL, embedAPIRequest{
			Model: e.model.Name,
			Input: texts,
		})
		if err != nil {
			return nil, err
		}
		if len(response.Embeddings) != len(batch) {
			return nil, fmt.Errorf("%w: expected=%d actual=%d", ErrInvalidResponse, len(batch), len(response.Embeddings))
		}

		for index, input := range batch {
			vector := append([]float32(nil), response.Embeddings[index]...)
			if len(vector) == 0 {
				return nil, fmt.Errorf("%w: empty embedding at batch_index=%d", ErrInvalidResponse, index)
			}
			results = append(results, domain.Embedding{ChunkID: input.ChunkID, Vector: vector})
		}
	}

	return results, nil
}

func normalizeProviderConfig(input domain.ProviderConfig) (domain.ProviderConfig, string, int, time.Duration, error) {
	defaults := configpkg.Defaults().Ollama
	normalized := domain.ProviderConfig{
		Name:    Name,
		Version: strings.TrimSpace(input.Version),
		Options: cloneStringMap(input.Options),
	}

	if name := strings.TrimSpace(input.Name); name != "" && name != Name {
		return domain.ProviderConfig{}, "", 0, 0, fmt.Errorf("ollama: provider name must be %q", Name)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(normalized.Options[OptionOllamaURL]), "/")
	if baseURL == "" {
		baseURL = defaults.URL
	}
	if baseURL == "" {
		return domain.ProviderConfig{}, "", 0, 0, fmt.Errorf("ollama: %s is required", OptionOllamaURL)
	}

	batchSize, err := intOption(normalized.Options, OptionBatchSize, defaults.BatchSize)
	if err != nil {
		return domain.ProviderConfig{}, "", 0, 0, err
	}
	if batchSize <= 0 {
		return domain.ProviderConfig{}, "", 0, 0, fmt.Errorf("ollama: %s must be > 0", OptionBatchSize)
	}

	timeout, err := durationOption(normalized.Options, OptionTimeout, defaults.Timeout)
	if err != nil {
		return domain.ProviderConfig{}, "", 0, 0, err
	}
	if timeout <= 0 {
		return domain.ProviderConfig{}, "", 0, 0, fmt.Errorf("ollama: %s must be > 0", OptionTimeout)
	}

	normalized.Options[OptionOllamaURL] = baseURL
	normalized.Options[OptionBatchSize] = fmt.Sprintf("%d", batchSize)
	normalized.Options[OptionTimeout] = timeout.String()

	return normalized, baseURL, batchSize, timeout, nil
}

func normalizeModel(input domain.ModelSpec) (domain.ModelSpec, error) {
	normalized := cloneModelSpec(input)
	provider := strings.TrimSpace(normalized.Provider)
	if provider == "" {
		provider = Name
	}
	if provider != Name {
		return domain.ModelSpec{}, fmt.Errorf("ollama: model provider must be %q", Name)
	}

	name := strings.TrimSpace(normalized.Name)
	if name == "" {
		name = DefaultModel
	}

	normalized.Provider = provider
	normalized.Name = name
	normalized.Version = strings.TrimSpace(normalized.Version)
	normalized.Options = cloneStringMap(normalized.Options)
	return normalized, nil
}

func validateRecipe(recipe domain.EmbedRecipe, model domain.ModelSpec) error {
	if strings.TrimSpace(recipe.Provider.Name) != "" && strings.TrimSpace(recipe.Provider.Name) != Name {
		return fmt.Errorf("ollama: recipe provider must be %q", Name)
	}
	if provider := strings.TrimSpace(recipe.Model.Provider); provider != "" && provider != Name {
		return fmt.Errorf("ollama: recipe model provider must be %q", Name)
	}
	if name := strings.TrimSpace(recipe.Model.Name); name != "" && name != model.Name {
		return fmt.Errorf("ollama: recipe model must be %q", model.Name)
	}
	return nil
}

func defaultsTimeout(config domain.ProviderConfig) time.Duration {
	defaults := configpkg.Defaults().Ollama
	_, _, _, timeout, err := normalizeProviderConfig(config)
	if err != nil {
		return defaults.Timeout
	}
	return timeout
}

func newHTTPEmbeddingClient(timeout time.Duration) embeddingClient {
	return &httpEmbeddingClient{client: &http.Client{Timeout: timeout}}
}

func (c *httpEmbeddingClient) Embed(ctx context.Context, baseURL string, request embedAPIRequest) (embedAPIResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: encode request: %v", ErrInvalidResponse, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: build request: %v", ErrInvalidResponse, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.client.Do(httpRequest)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: %v", ErrServiceUnavailable, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: read response: %v", ErrInvalidResponse, err)
	}

	if response.StatusCode == http.StatusNotFound {
		message := decodeAPIError(body)
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return embedAPIResponse{}, fmt.Errorf("%w: %s", ErrModelNotFound, message)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := decodeAPIError(body)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return embedAPIResponse{}, fmt.Errorf("%w: status=%d message=%s", ErrServiceUnavailable, response.StatusCode, message)
	}

	var decoded embedAPIResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: decode response: %v", ErrInvalidResponse, err)
	}
	if len(decoded.Embeddings) == 0 {
		return embedAPIResponse{}, fmt.Errorf("%w: embeddings field is empty", ErrInvalidResponse)
	}

	return decoded, nil
}

func decodeAPIError(body []byte) string {
	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil {
		return strings.TrimSpace(apiErr.Error)
	}
	return ""
}

func intOption(options map[string]string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(options[key])
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("ollama: invalid %s %q", key, raw)
	}
	return value, nil
}

func durationOption(options map[string]string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(options[key])
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("ollama: invalid %s %q", key, raw)
	}
	return value, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneModelSpec(model domain.ModelSpec) domain.ModelSpec {
	model.Options = cloneStringMap(model.Options)
	return model
}
