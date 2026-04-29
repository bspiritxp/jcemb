package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
)

const (
	Name         = "openai"
	DefaultModel = "text-embedding-3-small"
	DefaultDim   = 1536

	OptionBaseURL   = "openai_base_url"
	OptionAPIKey    = "openai_api_key"
	OptionBatchSize = "openai_batch_size"
	OptionTimeout   = "openai_timeout"
	OptionDimension = "openai_dimensions"

	defaultBaseURL   = "https://api.openai.com/v1"
	defaultBatchSize = 128
	defaultTimeout   = 60 * time.Second
)

var (
	ErrServiceUnavailable = errors.New("openai: service unavailable")
	ErrUnauthorized       = errors.New("openai: unauthorized")
	ErrModelNotFound      = errors.New("openai: model not found")
	ErrInvalidResponse    = errors.New("openai: invalid response")
)

type Provider struct {
	config    domain.ProviderConfig
	client    embeddingClient
	baseURL   string
	apiKey    string
	batchSize int
	timeout   time.Duration
	dimension int
}

type Embedder struct {
	provider  string
	model     domain.ModelSpec
	client    embeddingClient
	baseURL   string
	apiKey    string
	batchSize int
	dimension int
}

type embeddingClient interface {
	Embed(ctx context.Context, baseURL string, apiKey string, request embedAPIRequest) (embedAPIResponse, error)
}

type embedAPIRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embedAPIResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type httpEmbeddingClient struct {
	client httpClient
}

func New(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
	return newProvider(config, newHTTPEmbeddingClient(defaultsTimeout(config)))
}

func newProvider(config domain.ProviderConfig, client embeddingClient) (*Provider, error) {
	normalized, baseURL, apiKey, batchSize, timeout, dimension, err := normalizeProviderConfig(config)
	if err != nil {
		return nil, err
	}
	return &Provider{
		config:    normalized,
		client:    client,
		baseURL:   baseURL,
		apiKey:    apiKey,
		batchSize: batchSize,
		timeout:   timeout,
		dimension: dimension,
	}, nil
}

func (p *Provider) Name() string {
	return Name
}

func (p *Provider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	normalized, err := normalizeModel(model, p.dimension)
	if err != nil {
		return nil, err
	}
	return &Embedder{
		provider:  Name,
		model:     normalized,
		client:    p.client,
		baseURL:   p.baseURL,
		apiKey:    p.apiKey,
		batchSize: p.batchSize,
		dimension: p.dimension,
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
		batchSize = defaultBatchSize
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

		response, err := e.client.Embed(ctx, e.baseURL, e.apiKey, embedAPIRequest{
			Model:      e.model.Name,
			Input:      texts,
			Dimensions: e.model.Dimensions,
		})
		if err != nil {
			return nil, err
		}
		if len(response.Data) != len(batch) {
			return nil, fmt.Errorf("%w: expected=%d actual=%d", ErrInvalidResponse, len(batch), len(response.Data))
		}
		byIndex := make(map[int][]float32, len(response.Data))
		for _, item := range response.Data {
			byIndex[item.Index] = append([]float32(nil), item.Embedding...)
		}
		for index, input := range batch {
			vector, ok := byIndex[index]
			if !ok {
				return nil, fmt.Errorf("%w: missing embedding at index=%d", ErrInvalidResponse, index)
			}
			if len(vector) == 0 {
				return nil, fmt.Errorf("%w: empty embedding at index=%d", ErrInvalidResponse, index)
			}
			results = append(results, domain.Embedding{ChunkID: input.ChunkID, Vector: vector})
		}
	}
	return results, nil
}

func normalizeProviderConfig(input domain.ProviderConfig) (domain.ProviderConfig, string, string, int, time.Duration, int, error) {
	normalized := domain.ProviderConfig{
		Name:    Name,
		Version: strings.TrimSpace(input.Version),
		Options: cloneStringMap(input.Options),
	}
	if name := strings.TrimSpace(input.Name); name != "" && name != Name {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, fmt.Errorf("openai: provider name must be %q", Name)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(firstNonEmpty(normalized.Options[OptionBaseURL], os.Getenv("OPENAI_BASE_URL"))), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := strings.TrimSpace(firstNonEmpty(normalized.Options[OptionAPIKey], os.Getenv("OPENAI_API_KEY")))
	if apiKey == "" {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, fmt.Errorf("openai: api key is required; set %s or OPENAI_API_KEY", OptionAPIKey)
	}
	batchSize, err := intOption(normalized.Options, OptionBatchSize, defaultBatchSize)
	if err != nil {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, err
	}
	if batchSize <= 0 {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, fmt.Errorf("openai: %s must be > 0", OptionBatchSize)
	}
	timeout, err := durationOption(normalized.Options, OptionTimeout, defaultTimeout)
	if err != nil {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, err
	}
	if timeout <= 0 {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, fmt.Errorf("openai: %s must be > 0", OptionTimeout)
	}
	dimension, err := intOption(normalized.Options, OptionDimension, DefaultDim)
	if err != nil {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, err
	}
	if dimension <= 0 {
		return domain.ProviderConfig{}, "", "", 0, 0, 0, fmt.Errorf("openai: %s must be > 0", OptionDimension)
	}
	normalized.Options[OptionBaseURL] = baseURL
	normalized.Options[OptionAPIKey] = apiKey
	normalized.Options[OptionBatchSize] = strconv.Itoa(batchSize)
	normalized.Options[OptionTimeout] = timeout.String()
	normalized.Options[OptionDimension] = strconv.Itoa(dimension)
	return normalized, baseURL, apiKey, batchSize, timeout, dimension, nil
}

func normalizeModel(input domain.ModelSpec, dimension int) (domain.ModelSpec, error) {
	normalized := cloneModelSpec(input)
	provider := strings.TrimSpace(normalized.Provider)
	if provider == "" {
		provider = Name
	}
	if provider != Name {
		return domain.ModelSpec{}, fmt.Errorf("openai: model provider must be %q", Name)
	}
	name := strings.TrimSpace(normalized.Name)
	if name == "" {
		name = DefaultModel
	}
	if normalized.Dimensions <= 0 {
		normalized.Dimensions = dimension
	}
	normalized.Provider = provider
	normalized.Name = name
	normalized.Version = strings.TrimSpace(normalized.Version)
	normalized.Options = cloneStringMap(normalized.Options)
	return normalized, nil
}

func validateRecipe(recipe domain.EmbedRecipe, model domain.ModelSpec) error {
	if strings.TrimSpace(recipe.Provider.Name) != "" && strings.TrimSpace(recipe.Provider.Name) != Name {
		return fmt.Errorf("openai: recipe provider must be %q", Name)
	}
	if provider := strings.TrimSpace(recipe.Model.Provider); provider != "" && provider != Name {
		return fmt.Errorf("openai: recipe model provider must be %q", Name)
	}
	if name := strings.TrimSpace(recipe.Model.Name); name != "" && name != model.Name {
		return fmt.Errorf("openai: recipe model must be %q", model.Name)
	}
	return nil
}

func newHTTPEmbeddingClient(timeout time.Duration) embeddingClient {
	return &httpEmbeddingClient{client: &http.Client{Timeout: timeout}}
}

func (c *httpEmbeddingClient) Embed(ctx context.Context, baseURL string, apiKey string, request embedAPIRequest) (embedAPIResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: encode request: %v", ErrInvalidResponse, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: build request: %v", ErrInvalidResponse, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := c.client.Do(httpRequest)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: %v", ErrServiceUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: read response: %v", ErrInvalidResponse, err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return embedAPIResponse{}, fmt.Errorf("%w: %s", ErrUnauthorized, decodeAPIError(body))
	}
	if response.StatusCode == http.StatusNotFound {
		return embedAPIResponse{}, fmt.Errorf("%w: %s", ErrModelNotFound, decodeAPIError(body))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := decodeAPIError(body)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		return embedAPIResponse{}, fmt.Errorf("%w: status=%d message=%s", ErrServiceUnavailable, response.StatusCode, message)
	}
	var decoded embedAPIResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return embedAPIResponse{}, fmt.Errorf("%w: decode response: %v", ErrInvalidResponse, err)
	}
	if len(decoded.Data) == 0 {
		return embedAPIResponse{}, fmt.Errorf("%w: data field is empty", ErrInvalidResponse)
	}
	return decoded, nil
}

func defaultsTimeout(config domain.ProviderConfig) time.Duration {
	_, _, _, _, timeout, _, err := normalizeProviderConfig(config)
	if err != nil {
		return defaultTimeout
	}
	return timeout
}

func decodeAPIError(body []byte) string {
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return strings.TrimSpace(string(body))
}

func intOption(options map[string]string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(options[key])
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("openai: invalid %s %q", key, raw)
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
		return 0, fmt.Errorf("openai: invalid %s %q", key, raw)
	}
	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
