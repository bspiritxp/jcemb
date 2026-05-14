package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	configpkg "github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/domain"
	providerollama "github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/tagextractor"
)

const maxContentRunes = 4000

type Extractor struct {
	config  domain.TagExtractorConfig
	client  *http.Client
	baseURL string
	model   string
}

type generateResponse struct {
	Response string `json:"response"`
}

type tagsResponse struct {
	Tags []string `json:"tags"`
}

func init() {
	registry.MustRegisterTagExtractor(providerollama.Name, New)
}

func New(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
	normalized, baseURL, timeout, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(normalized.Model)
	if model == "" {
		return nil, fmt.Errorf("ollama: model is required")
	}

	return &Extractor{
		config:  normalized,
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		model:   model,
	}, nil
}

func (e *Extractor) Extract(ctx context.Context, req domain.TagExtractRequest) (domain.TagExtractResult, error) {
	prompt := buildPrompt(req.Document.Title, req.Document.Content, req.Config)
	payload := map[string]any{
		"model":  e.model,
		"stream": false,
		"format": "json",
		"prompt": prompt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: extract tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, decodeAPIError(responseBody))
	}

	var generated generateResponse
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(&generated); err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: decode response: %w", err)
	}

	var decoded tagsResponse
	decoder = json.NewDecoder(strings.NewReader(strings.TrimSpace(generated.Response)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("ollama: decode tags JSON: %w", err)
	}

	return domain.TagExtractResult{Tags: tagextractor.NormalizeSemanticTags(decoded.Tags, req.Config)}, nil
}

func normalizeConfig(config domain.TagExtractorConfig) (domain.TagExtractorConfig, string, time.Duration, error) {
	normalized := config
	normalized.Options = cloneStringMap(config.Options)

	defaults := configpkg.Defaults().Ollama
	baseURL := strings.TrimRight(strings.TrimSpace(normalized.Options[providerollama.OptionOllamaURL]), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(defaults.URL), "/")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	timeout := normalized.Timeout
	if timeout <= 0 {
		timeout = domain.DefaultTagExtractorTimeout
	}

	normalized.Options[providerollama.OptionOllamaURL] = baseURL
	normalized.Options[providerollama.OptionTimeout] = timeout.String()
	normalized.Timeout = timeout

	return normalized, baseURL, timeout, nil
}

func buildPrompt(title, content string, cfg domain.TagExtractorConfig) string {
	maxTags := cfg.MaxTags
	if maxTags <= 0 {
		maxTags = domain.DefaultTagExtractorMaxTags
	}
	truncated := truncateRunes(strings.TrimSpace(content), maxContentRunes)
	return fmt.Sprintf(
		"Extract %d-%d concise topical tags for retrieval. Return strict JSON: {\"tags\":[\"...\"]}. Document title: %s. Content (truncated): %s",
		1,
		maxTags,
		strings.TrimSpace(title),
		truncated,
	)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}

	runes := []rune(value)
	return string(runes[:limit])
}

func decodeAPIError(body []byte) string {
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
		return strings.TrimSpace(apiErr.Error)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		return http.StatusText(http.StatusInternalServerError)
	}
	return message
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
