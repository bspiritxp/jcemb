package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	openaiprovider "github.com/bspiritxp/jcemb/internal/provider/openai"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/tagextractor"
)

const (
	Name               = "openai"
	defaultModel       = "gpt-4.1-mini"
	defaultBaseURL     = "https://api.openai.com/v1"
	promptContentLimit = 4000
)

type Extractor struct {
	config domain.TagExtractorConfig
	client *http.Client
}

func init() {
	registry.MustRegisterTagExtractor(Name, New)
}

func New(cfg domain.TagExtractorConfig) (domain.TagExtractor, error) {
	return &Extractor{
		config: cfg,
		client: &http.Client{Timeout: resolveTimeout(cfg)},
	}, nil
}

func (e *Extractor) Extract(ctx context.Context, request domain.TagExtractRequest) (domain.TagExtractResult, error) {
	cfg := effectiveConfig(e.config, request.Config)
	apiKey := strings.TrimSpace(cfg.Options[openaiprovider.OptionAPIKey])
	if apiKey == "" {
		return domain.TagExtractResult{}, fmt.Errorf("tagextractor/openai: api key is required; set %s", openaiprovider.OptionAPIKey)
	}

	payload := map[string]any{
		"model": resolveModel(cfg),
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": buildPrompt(request.Document, cfg),
			}},
		}},
		"text": map[string]any{
			"format": map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "tags",
					"strict": true,
					"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"tags": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
						},
						"required":             []string{"tags"},
						"additionalProperties": false,
					},
				},
			},
		},
	}

	var decoded struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}

	if err := openAIRequest(ctx, e.client, cfg, http.MethodPost, "/responses", payload, &decoded); err != nil {
		return domain.TagExtractResult{}, err
	}

	text := extractOutputText(decoded.OutputText, decoded.Output)
	if text == "" {
		return domain.TagExtractResult{}, fmt.Errorf("tagextractor/openai: response output text is empty")
	}

	var parsed struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return domain.TagExtractResult{}, fmt.Errorf("tagextractor/openai: decode response tags JSON: %w", err)
	}

	return domain.TagExtractResult{Tags: tagextractor.NormalizeSemanticTags(parsed.Tags, cfg)}, nil
}

func effectiveConfig(base domain.TagExtractorConfig, override domain.TagExtractorConfig) domain.TagExtractorConfig {
	resolved := base
	if strings.TrimSpace(override.Provider) != "" {
		resolved.Provider = override.Provider
	}
	if strings.TrimSpace(override.Model) != "" {
		resolved.Model = override.Model
	}
	if override.Options != nil {
		resolved.Options = cloneOptions(override.Options)
	} else if resolved.Options != nil {
		resolved.Options = cloneOptions(resolved.Options)
	} else {
		resolved.Options = map[string]string{}
	}
	if override.Timeout > 0 {
		resolved.Timeout = override.Timeout
	}
	if override.MaxTags > 0 {
		resolved.MaxTags = override.MaxTags
	}
	if override.MinTagLen > 0 {
		resolved.MinTagLen = override.MinTagLen
	}
	if override.MaxTagLen > 0 {
		resolved.MaxTagLen = override.MaxTagLen
	}
	if override.SkipIfHasYAML {
		resolved.SkipIfHasYAML = true
	}
	return resolved
}

func cloneOptions(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func resolveModel(cfg domain.TagExtractorConfig) string {
	if model := strings.TrimSpace(cfg.Model); model != "" {
		return model
	}
	return defaultModel
}

func resolveBaseURL(cfg domain.TagExtractorConfig) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Options[openaiprovider.OptionBaseURL]), "/")
	if baseURL == "" {
		return defaultBaseURL
	}
	return baseURL
}

func resolveTimeout(cfg domain.TagExtractorConfig) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	if raw := strings.TrimSpace(cfg.Options[openaiprovider.OptionTimeout]); raw != "" {
		if timeout, err := time.ParseDuration(raw); err == nil && timeout > 0 {
			return timeout
		}
	}
	return domain.DefaultTagExtractorTimeout
}

func buildPrompt(doc domain.Document, cfg domain.TagExtractorConfig) string {
	minTags := 1
	maxTags := cfg.MaxTags
	if maxTags <= 0 {
		maxTags = domain.DefaultTagExtractorMaxTags
	}
	return fmt.Sprintf(
		"Extract %d-%d concise topical tags for retrieval. Return strict JSON: {\"tags\":[\"...\"]}. Document title: %s. Content (truncated): %s",
		minTags,
		maxTags,
		strconv.Quote(resolveDocumentTitle(doc)),
		truncateRunes(doc.Content, promptContentLimit),
	)
}

func resolveDocumentTitle(doc domain.Document) string {
	for _, candidate := range []string{doc.Title, doc.FileName, doc.RelPath, doc.FilePath, doc.Source} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return "untitled"
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func extractOutputText(outputText string, outputs []struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}) string {
	if text := strings.TrimSpace(outputText); text != "" {
		return text
	}
	for _, output := range outputs {
		for _, content := range output.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text)
			}
		}
	}
	return ""
}

func openAIRequest(ctx context.Context, client *http.Client, cfg domain.TagExtractorConfig, method string, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, resolveBaseURL(cfg)+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Options[openaiprovider.OptionAPIKey]))
	if client == nil {
		client = &http.Client{Timeout: resolveTimeout(cfg)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("tagextractor/openai: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("tagextractor/openai: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tagextractor/openai: status=%d message=%s", resp.StatusCode, decodeOpenAIError(responseBody))
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("tagextractor/openai: decode response: %w", err)
	}
	return nil
}

func decodeOpenAIError(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return strings.TrimSpace(string(body))
}
