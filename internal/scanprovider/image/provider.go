package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	FileType           = "image"
	Version            = "v1"
	defaultProvider    = "openclip"
	defaultModel       = "ViT-B-32"
	defaultPretrained  = "laion2b_s34b_b79k"
	defaultDimensions  = 512
	defaultDevice      = "auto"
	defaultPython      = "python3"
	defaultVisionModel = "llava"
)

//go:embed scripts/embed.py
var embedScript string

type Provider struct {
	vectorizer ImageVectorizer
	captioner  Captioner
}

type ImageVectorizer interface {
	EmbedImage(ctx context.Context, config ImageModelConfig, path string, content []byte) ([]float32, error)
	EmbedText(ctx context.Context, config ImageModelConfig, text string) ([]float32, error)
}

type Captioner interface {
	Caption(ctx context.Context, path string, content []byte, config domain.ScanProviderConfig) (Caption, error)
}

type Caption struct {
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
}

func New() domain.ScanProvider {
	return Provider{vectorizer: RoutingVectorizer{}, captioner: RoutingCaptioner{}}
}

func NewWithClients(vectorizer ImageVectorizer, captioner Captioner) domain.ScanProvider {
	return Provider{vectorizer: vectorizer, captioner: captioner}
}

func (Provider) FileType() string {
	return FileType
}

func (Provider) Extensions() []string {
	return []string{".png", ".jpg", ".jpeg", ".webp", ".svg", ".gif", ".bmp", ".tif", ".tiff"}
}

func (Provider) Recipe(config domain.ScanProviderConfig) domain.EmbedRecipe {
	model := imageModelConfigFromOptions(config.ProviderOptions)
	return domain.EmbedRecipe{
		Type:    FileType,
		Version: Version,
		Provider: domain.ProviderConfig{
			Name:    model.Provider,
			Version: Version,
			Options: map[string]string{
				"pretrained": model.Pretrained,
				"device":     model.Device,
				"python":     model.Python,
			},
		},
		Model: domain.ModelSpec{
			Provider:   model.Provider,
			Name:       model.Model,
			Version:    Version,
			Dimensions: model.Dimensions,
		},
		Splitter: domain.SplitterSpec{
			Name:    FileType,
			Version: Version,
		},
		Flags: map[string]bool{
			"recursive": config.Recursive,
			"force":     config.Force,
		},
	}
}

func (p Provider) BuildRecords(ctx context.Context, request domain.ScanProviderRequest) (domain.ScanProviderResult, error) {
	content, err := os.ReadFile(request.File.FilePath)
	if err != nil {
		return domain.ScanProviderResult{}, fmt.Errorf("image: read %s: %w", request.File.FilePath, err)
	}
	sum := sha256.Sum256(content)
	fileHash := hex.EncodeToString(sum[:])

	caption, err := p.captioner.Caption(ctx, request.File.FilePath, content, request.Config)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	caption = normalizeCaption(caption, request.File.FileName)

	model := imageModelConfigFromOptions(request.Config.ProviderOptions)
	var vector []float32
	if model.Provider == "openai" {
		vector, err = p.vectorizer.EmbedText(ctx, model, captionText(caption))
	} else {
		vector, err = p.vectorizer.EmbedImage(ctx, model, request.File.FilePath, content)
	}
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	if len(vector) != model.Dimensions {
		return domain.ScanProviderResult{}, fmt.Errorf("image: vector dimension mismatch: expected=%d actual=%d", model.Dimensions, len(vector))
	}

	yaml := map[string]any{
		"file_type":   FileType,
		"description": caption.Description,
	}
	document := domain.Document{
		Source:    request.File.RelPath,
		FilePath:  request.File.FilePath,
		RelPath:   request.File.RelPath,
		FileName:  request.File.FileName,
		DocType:   FileType,
		FileHash:  fileHash,
		Title:     caption.Title,
		Content:   caption.Description,
		TitlePath: []string{caption.Title},
		Tags:      domain.NormalizeTags(caption.Tags),
		YAML:      yaml,
	}
	metadata, err := domain.NewChunkMetadata(map[string]any{
		domain.MetadataSourceKey:     document.Source,
		domain.MetadataFilePathKey:   document.FilePath,
		domain.MetadataRelPathKey:    document.RelPath,
		domain.MetadataFileNameKey:   document.FileName,
		domain.MetadataDocTypeKey:    document.DocType,
		domain.MetadataFileHashKey:   document.FileHash,
		domain.MetadataTitleKey:      document.Title,
		domain.MetadataChunkIndexKey: 0,
		domain.MetadataTitlePathKey:  document.TitlePath,
		domain.MetadataTagsKey:       document.Tags,
		domain.MetadataYAMLKey:       yaml,
	})
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	chunk := domain.Chunk{
		ID:         chunkID(document.RelPath, request.Recipe.Hash(), fileHash),
		Document:   document,
		Content:    caption.Description,
		Metadata:   metadata,
		RecipeHash: request.Recipe.Hash(),
		CreatedAt:  request.Now(),
	}
	state := domain.FileState{
		Source:        document.Source,
		FilePath:      document.FilePath,
		RelPath:       document.RelPath,
		FileName:      document.FileName,
		DocType:       FileType,
		FileHash:      fileHash,
		ModTime:       request.File.ModTime,
		RecipeHash:    request.Recipe.Hash(),
		ChunkIDs:      []string{chunk.ID},
		ChunkCount:    1,
		LastIndexedAt: request.Now(),
	}
	return domain.ScanProviderResult{
		State:   state,
		Records: []domain.VectorRecord{{Chunk: chunk, Vector: vector}},
	}, nil
}

type ImageModelConfig struct {
	Provider   string
	Model      string
	Pretrained string
	Dimensions int
	Device     string
	Python     string
	DataDir    string
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
}

type RoutingVectorizer struct{}

func (RoutingVectorizer) EmbedImage(ctx context.Context, config ImageModelConfig, path string, content []byte) ([]float32, error) {
	if config.Provider == "openai" {
		caption, err := OpenAICaptioner{}.Caption(ctx, path, content, domain.ScanProviderConfig{ProviderOptions: optionsFromImageModelConfig(config)})
		if err != nil {
			return nil, err
		}
		text := strings.Join(append([]string{caption.Title, caption.Description}, caption.Tags...), " ")
		return OpenAIVectorizer{}.EmbedText(ctx, config, text)
	}
	return PythonVectorizer{}.EmbedImage(ctx, config, path, content)
}

func (RoutingVectorizer) EmbedText(ctx context.Context, config ImageModelConfig, text string) ([]float32, error) {
	if config.Provider == "openai" {
		return OpenAIVectorizer{}.EmbedText(ctx, config, text)
	}
	return PythonVectorizer{}.EmbedText(ctx, config, text)
}

type PythonVectorizer struct{}

func (PythonVectorizer) EmbedImage(ctx context.Context, config ImageModelConfig, path string, content []byte) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return runPythonEmbedding(ctx, config, map[string]any{
		"kind":        "image",
		"content_b64": base64.StdEncoding.EncodeToString(content),
	})
}

func (PythonVectorizer) EmbedText(ctx context.Context, config ImageModelConfig, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return runPythonEmbedding(ctx, config, map[string]any{
		"kind": "text",
		"text": text,
	})
}

type OllamaCaptioner struct {
	Client *http.Client
}

type RoutingCaptioner struct{}

func (RoutingCaptioner) Caption(ctx context.Context, path string, content []byte, config domain.ScanProviderConfig) (Caption, error) {
	model := imageModelConfigFromOptions(config.ProviderOptions)
	if model.Provider == "openai" {
		return OpenAICaptioner{}.Caption(ctx, path, content, config)
	}
	return OllamaCaptioner{}.Caption(ctx, path, content, config)
}

type OpenAIVectorizer struct {
	Client *http.Client
}

func (v OpenAIVectorizer) EmbedImage(ctx context.Context, config ImageModelConfig, path string, content []byte) ([]float32, error) {
	caption, err := OpenAICaptioner(v).Caption(ctx, path, content, domain.ScanProviderConfig{ProviderOptions: optionsFromImageModelConfig(config)})
	if err != nil {
		return nil, err
	}
	return v.EmbedText(ctx, config, strings.Join(append([]string{caption.Title, caption.Description}, caption.Tags...), " "))
}

func (v OpenAIVectorizer) EmbedText(ctx context.Context, config ImageModelConfig, text string) ([]float32, error) {
	request := map[string]any{
		"model":      config.Model,
		"input":      []string{text},
		"dimensions": config.Dimensions,
	}
	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := openAIRequest(ctx, v.Client, config, http.MethodPost, "/embeddings", request, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) != 1 {
		return nil, fmt.Errorf("image: openai embedding response count mismatch: expected=1 actual=%d", len(decoded.Data))
	}
	vector := append([]float32(nil), decoded.Data[0].Embedding...)
	if len(vector) != config.Dimensions {
		return nil, fmt.Errorf("image: vector dimension mismatch: expected=%d actual=%d", config.Dimensions, len(vector))
	}
	return vector, nil
}

type OpenAICaptioner struct {
	Client *http.Client
}

func (c OpenAICaptioner) Caption(ctx context.Context, path string, content []byte, config domain.ScanProviderConfig) (Caption, error) {
	model := imageModelConfigFromOptions(config.ProviderOptions)
	mimeType, ok := openAIMimeTypeForPath(path)
	if !ok {
		return Caption{}, fmt.Errorf("image: OpenAI image input does not support %s", filepath.Ext(path))
	}
	payload := map[string]any{
		"model": modelVisionModel(config.ProviderOptions),
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": "Describe this image for semantic search. Return compact JSON with keys title, tags, description."},
				{"type": "input_image", "image_url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content), "detail": "auto"},
			},
		}},
		"text": map[string]any{
			"format": map[string]any{
				"type": "json_schema",
				"name": "image_metadata",
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"description": map[string]any{"type": "string"},
					},
					"required":             []string{"title", "tags", "description"},
					"additionalProperties": false,
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
	if err := openAIRequest(ctx, c.Client, model, http.MethodPost, "/responses", payload, &decoded); err != nil {
		return Caption{}, err
	}
	text := strings.TrimSpace(decoded.OutputText)
	if text == "" {
		for _, output := range decoded.Output {
			for _, content := range output.Content {
				if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
					text = strings.TrimSpace(content.Text)
					break
				}
			}
		}
	}
	var caption Caption
	if err := json.Unmarshal([]byte(text), &caption); err != nil {
		return Caption{}, fmt.Errorf("image: decode OpenAI caption JSON: %w", err)
	}
	caption = normalizeCaption(caption, filepath.Base(path))
	if caption.Description == "" {
		return Caption{}, fmt.Errorf("image: OpenAI caption response description is required")
	}
	return caption, nil
}

func (c OllamaCaptioner) Caption(ctx context.Context, path string, content []byte, config domain.ScanProviderConfig) (Caption, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.ProviderOptions["ollama_url"]), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := strings.TrimSpace(config.ProviderOptions["vision_model"])
	if model == "" {
		model = defaultVisionModel
	}
	captionContent, err := ollamaCaptionImageContent(path, content)
	if err != nil {
		return Caption{}, err
	}
	payload := map[string]any{
		"model":  model,
		"stream": false,
		"format": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"description": map[string]any{"type": "string"},
			},
			"required": []string{"title", "tags", "description"},
		},
		"prompt": "Describe this image for semantic search. Return compact JSON with title, tags, and description.",
		"images": []string{base64.StdEncoding.EncodeToString(captionContent)},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Caption{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Caption{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Caption{}, fmt.Errorf("image: caption %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Caption{}, fmt.Errorf("image: read caption response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Caption{}, fmt.Errorf("image: caption %s: ollama status %d: %s", path, resp.StatusCode, decodeOllamaError(responseBody))
	}
	var decoded struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Caption{}, fmt.Errorf("image: decode caption response: %w", err)
	}
	var caption Caption
	if err := json.Unmarshal([]byte(strings.TrimSpace(decoded.Response)), &caption); err != nil {
		return Caption{}, fmt.Errorf("image: decode caption JSON: %w", err)
	}
	caption = normalizeCaption(caption, filepath.Base(path))
	if caption.Description == "" {
		return Caption{}, fmt.Errorf("image: caption response description is required")
	}
	return caption, nil
}

func EmbedQuery(ctx context.Context, storeConfig domain.StoreConfig, runtimeOptions map[string]string, input string, imagePath bool) ([]float32, error) {
	vectorizer := PythonVectorizer{}
	options := cloneStringMap(storeConfig.ProviderOptions)
	for key, value := range runtimeOptions {
		if strings.TrimSpace(value) != "" {
			options[key] = value
		}
	}
	model := ImageModelConfig{
		Provider:   storeConfig.Provider,
		Model:      storeConfig.Model,
		Pretrained: options["pretrained"],
		Dimensions: storeConfig.VectorDim,
		Device:     options["device"],
		Python:     options["python"],
		BaseURL:    options["openai_base_url"],
		APIKey:     options["openai_api_key"],
		DataDir:    storeConfig.DataDir,
	}
	if model.Provider == "" || model.Model == "" || model.Dimensions <= 0 {
		model = imageModelConfigFromOptions(nil)
		model.DataDir = storeConfig.DataDir
	}
	if model.Device == "" {
		model.Device = defaultDevice
	}
	if model.Python == "" {
		model.Python = defaultPython
	}
	if model.Provider == "openai" {
		if model.BaseURL == "" {
			model.BaseURL = firstNonEmpty(os.Getenv("OPENAI_BASE_URL"), "https://api.openai.com/v1")
		}
		if model.APIKey == "" {
			model.APIKey = os.Getenv("OPENAI_API_KEY")
		}
	}
	if imagePath {
		content, err := os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("image: read query image %s: %w", input, err)
		}
		return vectorizer.EmbedImage(ctx, model, input, content)
	}
	return vectorizer.EmbedText(ctx, model, input)
}

func SupportedExtension(extension string) bool {
	normalized := strings.ToLower(strings.TrimSpace(extension))
	return slices.Contains((Provider{}).Extensions(), normalized)
}

func normalizeCaption(caption Caption, fallbackName string) Caption {
	caption.Title = strings.TrimSpace(caption.Title)
	if caption.Title == "" {
		caption.Title = strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName))
	}
	caption.Description = strings.TrimSpace(caption.Description)
	caption.Tags = domain.NormalizeTags(caption.Tags)
	sort.Strings(caption.Tags)
	return caption
}

func captionText(caption Caption) string {
	parts := []string{caption.Title, caption.Description}
	parts = append(parts, caption.Tags...)
	return strings.Join(parts, " ")
}

func ollamaCaptionImageContent(path string, content []byte) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		return nil, fmt.Errorf("image: Ollama caption does not support SVG input; use image.provider=openai or convert %s to a raster image", path)
	}

	_, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("image: detect %s for Ollama caption: %w", path, err)
	}
	if format == "png" || format == "jpeg" {
		return content, nil
	}

	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("image: decode %s for Ollama caption: %w", path, err)
	}
	var output bytes.Buffer
	if err := png.Encode(&output, decoded); err != nil {
		return nil, fmt.Errorf("image: convert %s to PNG for Ollama caption: %w", path, err)
	}
	return output.Bytes(), nil
}

func chunkID(relPath string, recipeHash string, fileHash string) string {
	sum := sha256.Sum256([]byte(relPath + "|" + recipeHash + "|" + fileHash + "|0"))
	return hex.EncodeToString(sum[:])
}

func ensureModelCache(config ImageModelConfig) error {
	if strings.TrimSpace(config.DataDir) == "" {
		return fmt.Errorf("image: data dir is required")
	}
	dir := filepath.Join(config.DataDir, "models", "image", config.Provider, sanitizePathPart(config.Model))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("image: create model cache: %w", err)
	}
	payload := map[string]any{
		"provider":   config.Provider,
		"model":      config.Model,
		"pretrained": config.Pretrained,
		"dimensions": config.Dimensions,
		"device":     config.Device,
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(content, '\n'), 0o644)
}

func imageModelConfigFromOptions(options map[string]string) ImageModelConfig {
	config := ImageModelConfig{
		Provider:   defaultProvider,
		Model:      defaultModel,
		Pretrained: defaultPretrained,
		Dimensions: defaultDimensions,
		Device:     defaultDevice,
		Python:     defaultPython,
	}
	if value := strings.TrimSpace(options["image_provider"]); value != "" {
		config.Provider = value
	}
	if value := strings.TrimSpace(options["image_model"]); value != "" {
		config.Model = value
	}
	if value := strings.TrimSpace(options["image_pretrained"]); value != "" {
		config.Pretrained = value
	}
	if value := strings.TrimSpace(options["image_dimensions"]); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			config.Dimensions = parsed
		}
	}
	if value := strings.TrimSpace(options["image_device"]); value != "" {
		config.Device = value
	}
	if value := strings.TrimSpace(options["image_python"]); value != "" {
		config.Python = value
	}
	if value := strings.TrimSpace(options["openai_base_url"]); value != "" {
		config.BaseURL = strings.TrimRight(value, "/")
	}
	if value := strings.TrimSpace(options["openai_api_key"]); value != "" {
		config.APIKey = value
	}
	if value := strings.TrimSpace(options["openai_timeout"]); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			config.Timeout = parsed
		}
	}
	config.Provider = normalizeProvider(config.Provider)
	if config.Provider == "jina" {
		config.Provider = "jina-clip"
	}
	if config.Provider == "jina-clip" && config.Model == defaultModel {
		config.Model = "jinaai/jina-clip-v2"
		config.Pretrained = ""
	}
	if config.Provider == "openai" {
		if config.Model == defaultModel {
			config.Model = "text-embedding-3-small"
		}
		if config.Dimensions == defaultDimensions {
			config.Dimensions = 1536
		}
		if config.BaseURL == "" {
			config.BaseURL = firstNonEmpty(os.Getenv("OPENAI_BASE_URL"), "https://api.openai.com/v1")
		}
		if config.APIKey == "" {
			config.APIKey = os.Getenv("OPENAI_API_KEY")
		}
		if config.Timeout <= 0 {
			config.Timeout = 60 * time.Second
		}
	}
	return config
}

func modelVisionModel(options map[string]string) string {
	model := strings.TrimSpace(options["vision_model"])
	if model == "" || model == defaultVisionModel {
		if strings.TrimSpace(options["image_provider"]) == "openai" {
			return "gpt-4.1-mini"
		}
		return defaultVisionModel
	}
	return model
}

func openAIRequest(ctx context.Context, client *http.Client, config ImageModelConfig, method string, path string, payload any, target any) error {
	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("image: openai api key is required; set openai.api_key or OPENAI_API_KEY")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("image: openai request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("image: read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("image: openai status=%d message=%s", resp.StatusCode, decodeOpenAIError(responseBody))
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("image: decode OpenAI response: %w", err)
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

func decodeOllamaError(body []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error) != "" {
		return strings.TrimSpace(envelope.Error)
	}
	return strings.TrimSpace(string(body))
}

func optionsFromImageModelConfig(config ImageModelConfig) map[string]string {
	return map[string]string{
		"image_provider":   config.Provider,
		"image_model":      config.Model,
		"image_dimensions": strconv.Itoa(config.Dimensions),
		"image_device":     config.Device,
		"image_python":     config.Python,
		"openai_base_url":  config.BaseURL,
		"openai_api_key":   config.APIKey,
		"openai_timeout":   config.Timeout.String(),
	}
}

func openAIMimeTypeForPath(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".webp":
		return "image/webp", true
	case ".gif":
		return "image/gif", true
	case ".png":
		return "image/png", true
	default:
		return "", false
	}
}

func runPythonEmbedding(ctx context.Context, config ImageModelConfig, payload map[string]any) ([]float32, error) {
	if config.DataDir != "" {
		if err := ensureModelCache(config); err != nil {
			return nil, err
		}
	}
	request := map[string]any{
		"provider":   config.Provider,
		"model":      config.Model,
		"pretrained": config.Pretrained,
		"dimensions": config.Dimensions,
		"device":     config.Device,
	}
	maps.Copy(request, payload)
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, config.Python, "-c", embedScript)
	cmd.Env = imagePythonEnv(config)
	cmd.Stdin = bytes.NewReader(body)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("image: %s embedding failed: %s", config.Provider, message)
	}
	var response struct {
		Vector []float32 `json:"vector"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("image: decode embedding response: %w", err)
	}
	if len(response.Vector) != config.Dimensions {
		return nil, fmt.Errorf("image: vector dimension mismatch: expected=%d actual=%d", config.Dimensions, len(response.Vector))
	}
	return response.Vector, nil
}

func imagePythonEnv(config ImageModelConfig) []string {
	env := withoutEnvKeys(os.Environ(), "HF_HOME", "HUGGINGFACE_HUB_CACHE", "TORCH_HOME", "XDG_CACHE_HOME")
	if strings.TrimSpace(config.DataDir) == "" {
		return env
	}
	cacheDir := filepath.Join(config.DataDir, "models", "image", "cache")
	hfHome := filepath.Join(cacheDir, "huggingface")
	torchHome := filepath.Join(cacheDir, "torch")
	if err := os.MkdirAll(hfHome, 0o755); err == nil {
		env = append(env, "HF_HOME="+hfHome)
		env = append(env, "HUGGINGFACE_HUB_CACHE="+filepath.Join(hfHome, "hub"))
		env = append(env, "XDG_CACHE_HOME="+cacheDir)
	}
	if err := os.MkdirAll(torchHome, 0o755); err == nil {
		env = append(env, "TORCH_HOME="+torchHome)
	}
	return env
}

func withoutEnvKeys(env []string, keys ...string) []string {
	blocked := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		blocked[key] = struct{}{}
	}
	filtered := env[:0]
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		if _, skip := blocked[key]; skip {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
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
	if values == nil {
		return map[string]string{}
	}
	return maps.Clone(values)
}

func sanitizePathPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	sanitized := replacer.Replace(strings.TrimSpace(value))
	if sanitized == "" {
		return "model"
	}
	return sanitized
}

func CloneProviderWithClients(vectorizer ImageVectorizer, captioner Captioner) Provider {
	return Provider{vectorizer: vectorizer, captioner: captioner}
}
