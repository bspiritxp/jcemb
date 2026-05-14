package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestImageProviderBuildRecordsUsesCaptionAndVector(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "cat.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake image"), 0o644))

	provider := NewWithClients(fakeVectorizer{}, fakeCaptioner{})
	result, err := provider.BuildRecords(context.Background(), domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  root,
			FilePath: imagePath,
			RelPath:  imagePath,
			FileName: "cat.png",
			DocType:  FileType,
			ModTime:  time.Unix(1, 0).UTC(),
		},
		Config: domain.ScanProviderConfig{FileType: FileType, DataDir: t.TempDir()},
		Recipe: provider.Recipe(domain.ScanProviderConfig{
			FileType: FileType,
		}),
		Now: func() time.Time { return time.Unix(2, 0).UTC() },
	})

	require.NoError(t, err)
	require.Equal(t, FileType, result.State.DocType)
	require.Equal(t, "cat photo", result.Records[0].Chunk.Metadata.Title)
	require.Equal(t, []string{"cat", "pet"}, result.Records[0].Chunk.Metadata.Tags)
	require.Equal(t, "a cat on a chair", result.Records[0].Chunk.Content)
	require.Len(t, result.Records[0].Vector, defaultDimensions)
}

func TestImageProviderRecipeUsesConfiguredModel(t *testing.T) {
	provider := Provider{}
	recipe := provider.Recipe(domain.ScanProviderConfig{ProviderOptions: map[string]string{
		"image_provider":   "jina-clip",
		"image_model":      "jinaai/jina-clip-v2",
		"image_dimensions": "1024",
		"image_device":     "cpu",
	}})

	require.Equal(t, "jina-clip", recipe.Provider.Name)
	require.Equal(t, "jinaai/jina-clip-v2", recipe.Model.Name)
	require.Equal(t, 1024, recipe.Model.Dimensions)
	require.Equal(t, "cpu", recipe.Provider.Options["device"])
}

func TestImageProviderOpenAIUsesCaptionTextForScanVector(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "cat.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake image"), 0o644))

	vectorizer := &recordingVectorizer{dimension: 1536}
	provider := NewWithClients(vectorizer, fakeCaptioner{})
	config := domain.ScanProviderConfig{
		FileType: FileType,
		DataDir:  t.TempDir(),
		ProviderOptions: map[string]string{
			"image_provider":   "openai",
			"image_model":      "text-embedding-3-small",
			"image_dimensions": "1536",
			"openai_api_key":   "sk-test",
		},
	}
	result, err := provider.BuildRecords(context.Background(), domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  root,
			FilePath: imagePath,
			RelPath:  imagePath,
			FileName: "cat.png",
			DocType:  FileType,
			ModTime:  time.Unix(1, 0).UTC(),
		},
		Config: config,
		Recipe: provider.Recipe(config),
		Now:    func() time.Time { return time.Unix(2, 0).UTC() },
	})

	require.NoError(t, err)
	require.Len(t, result.Records[0].Vector, 1536)
	require.Zero(t, vectorizer.imageCalls)
	require.Equal(t, 1, vectorizer.textCalls)
	require.Contains(t, vectorizer.lastText, "cat photo")
	require.Contains(t, vectorizer.lastText, "a cat on a chair")
	require.Contains(t, vectorizer.lastText, "cat")
	require.Contains(t, vectorizer.lastText, "pet")
}

func TestImageProviderBuildRecordsReusesCaptionTagsForSemanticTagVector(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "cat.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake image"), 0o644))

	vectorizer := &poolingVectorizer{
		imageVector: []float32{9, 9, 9},
		textVectors: map[string][]float32{
			"cat": []float32{1, 2, 3},
			"pet": []float32{3, 2, 1},
		},
	}
	provider := NewWithClients(vectorizer, fakeCaptioner{})
	config := domain.ScanProviderConfig{FileType: FileType, DataDir: t.TempDir(), ProviderOptions: map[string]string{"image_dimensions": "3"}}
	recipe := provider.Recipe(config)
	recipe.TagExtractor = &domain.TagExtractorRecipeSpec{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32}
	result, err := provider.BuildRecords(context.Background(), domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  root,
			FilePath: imagePath,
			RelPath:  imagePath,
			FileName: "cat.png",
			DocType:  FileType,
			ModTime:  time.Unix(1, 0).UTC(),
		},
		Config: config,
		Recipe: recipe,
		Now:    func() time.Time { return time.Unix(2, 0).UTC() },
	})

	require.NoError(t, err)
	require.Equal(t, []float32{9, 9, 9}, result.Records[0].Vector)
	requireVectorApprox(t, result.Records[0].TagVector, []float32{0.57735026, 0.57735026, 0.57735026})
	require.Equal(t, []string{"cat", "pet"}, result.Records[0].SemanticTags)
	require.Equal(t, []string{"cat", "pet"}, result.Records[0].Chunk.Metadata.Tags)
	require.Equal(t, 1, vectorizer.imageCalls)
	require.Equal(t, []string{"cat", "pet"}, vectorizer.textInputs)
}

func TestImageProviderRecipeIncludesTagExtractorSpecWhenEnabled(t *testing.T) {
	provider := Provider{}
	recipe := provider.Recipe(domain.ScanProviderConfig{
		ProviderOptions: map[string]string{"image_dimensions": "3"},
		TagExtractor: domain.TagExtractorConfig{
			Provider:      "openai",
			Model:         "gpt-4.1-mini",
			MaxTags:       6,
			MinTagLen:     2,
			MaxTagLen:     24,
			SkipIfHasYAML: true,
		},
	})
	require.NotNil(t, recipe.TagExtractor)
	require.Equal(t, "openai", recipe.TagExtractor.Provider)
	require.Equal(t, "gpt-4.1-mini", recipe.TagExtractor.Model)
	require.Equal(t, 6, recipe.TagExtractor.MaxTags)
}

func TestImageProviderBuildRecordsLeavesSemanticTagFieldsNilWhenCaptionHasNoTags(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "cat.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake image"), 0o644))

	vectorizer := &poolingVectorizer{imageVector: []float32{9, 9, 9}}
	provider := NewWithClients(vectorizer, noTagsCaptioner{})
	config := domain.ScanProviderConfig{FileType: FileType, DataDir: t.TempDir(), ProviderOptions: map[string]string{"image_dimensions": "3"}}
	recipe := provider.Recipe(config)
	recipe.TagExtractor = &domain.TagExtractorRecipeSpec{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32}
	result, err := provider.BuildRecords(context.Background(), domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  root,
			FilePath: imagePath,
			RelPath:  imagePath,
			FileName: "cat.png",
			DocType:  FileType,
			ModTime:  time.Unix(1, 0).UTC(),
		},
		Config: config,
		Recipe: recipe,
		Now:    func() time.Time { return time.Unix(2, 0).UTC() },
	})

	require.NoError(t, err)
	require.Nil(t, result.Records[0].TagVector)
	require.Nil(t, result.Records[0].SemanticTags)
	require.Empty(t, result.Records[0].Chunk.Metadata.Tags)
	require.Equal(t, 1, vectorizer.imageCalls)
	require.Empty(t, vectorizer.textInputs)
}

func TestImageProviderBuildRecordsLeavesSemanticTagFieldsNilWhenFeatureDisabled(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "cat.png")
	require.NoError(t, os.WriteFile(imagePath, []byte("fake image"), 0o644))

	vectorizer := &poolingVectorizer{imageVector: []float32{9, 9, 9}}
	provider := NewWithClients(vectorizer, fakeCaptioner{})
	config := domain.ScanProviderConfig{FileType: FileType, DataDir: t.TempDir(), ProviderOptions: map[string]string{"image_dimensions": "3"}}
	recipe := provider.Recipe(config)
	result, err := provider.BuildRecords(context.Background(), domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  root,
			FilePath: imagePath,
			RelPath:  imagePath,
			FileName: "cat.png",
			DocType:  FileType,
			ModTime:  time.Unix(1, 0).UTC(),
		},
		Config: config,
		Recipe: recipe,
		Now:    func() time.Time { return time.Unix(2, 0).UTC() },
	})

	require.NoError(t, err)
	require.Nil(t, result.Records[0].TagVector)
	require.Nil(t, result.Records[0].SemanticTags)
	require.Equal(t, []string{"cat", "pet"}, result.Records[0].Chunk.Metadata.Tags)
	require.Equal(t, 1, vectorizer.imageCalls)
	require.Empty(t, vectorizer.textInputs)
}

func TestOllamaCaptionImageContentConvertsWebPToPNG(t *testing.T) {
	content, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA=")
	require.NoError(t, err)

	converted, err := ollamaCaptionImageContent("tiny.webp", content)

	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(converted), "\x89PNG\r\n\x1a\n"))
	_, err = png.Decode(bytes.NewReader(converted))
	require.NoError(t, err)
}

func TestOllamaCaptionImageContentConvertsWebPWithPNGExtension(t *testing.T) {
	content, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA=")
	require.NoError(t, err)

	converted, err := ollamaCaptionImageContent("mislabeled.png", content)

	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(converted), "\x89PNG\r\n\x1a\n"))
	_, err = png.Decode(bytes.NewReader(converted))
	require.NoError(t, err)
}

func TestOllamaCaptionImageContentRejectsSVG(t *testing.T) {
	_, err := ollamaCaptionImageContent("diagram.svg", []byte("<svg></svg>"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support SVG")
}

func TestImagePythonEnvPinsModelCachesUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HF_HOME", "/bad/hf")
	t.Setenv("HUGGINGFACE_HUB_CACHE", "/bad/hub")
	t.Setenv("TORCH_HOME", "/bad/torch")
	t.Setenv("XDG_CACHE_HOME", "/bad/xdg")

	env := imagePythonEnv(ImageModelConfig{DataDir: dataDir})

	require.Contains(t, env, "HF_HOME="+filepath.Join(dataDir, "models", "image", "cache", "huggingface"))
	require.Contains(t, env, "HUGGINGFACE_HUB_CACHE="+filepath.Join(dataDir, "models", "image", "cache", "huggingface", "hub"))
	require.Contains(t, env, "TORCH_HOME="+filepath.Join(dataDir, "models", "image", "cache", "torch"))
	require.Contains(t, env, "XDG_CACHE_HOME="+filepath.Join(dataDir, "models", "image", "cache"))
	require.NotContains(t, env, "HF_HOME=/bad/hf")
	require.NotContains(t, env, "HUGGINGFACE_HUB_CACHE=/bad/hub")
	require.NotContains(t, env, "TORCH_HOME=/bad/torch")
	require.NotContains(t, env, "XDG_CACHE_HOME=/bad/xdg")
}

func requireVectorApprox(t *testing.T, got []float32, want []float32) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, want[i], got[i], 1e-6)
	}
}

type fakeVectorizer struct{}

func (fakeVectorizer) EmbedImage(context.Context, ImageModelConfig, string, []byte) ([]float32, error) {
	return make([]float32, defaultDimensions), nil
}

func (fakeVectorizer) EmbedText(context.Context, ImageModelConfig, string) ([]float32, error) {
	return make([]float32, defaultDimensions), nil
}

type fakeCaptioner struct{}

func (fakeCaptioner) Caption(context.Context, string, []byte, domain.ScanProviderConfig) (Caption, error) {
	return Caption{Title: "cat photo", Tags: []string{"Pet", "cat"}, Description: "a cat on a chair"}, nil
}

type noTagsCaptioner struct{}

func (noTagsCaptioner) Caption(context.Context, string, []byte, domain.ScanProviderConfig) (Caption, error) {
	return Caption{Title: "cat photo", Description: "a cat on a chair"}, nil
}

type recordingVectorizer struct {
	dimension  int
	imageCalls int
	textCalls  int
	lastText   string
}

func (v *recordingVectorizer) EmbedImage(context.Context, ImageModelConfig, string, []byte) ([]float32, error) {
	v.imageCalls++
	return make([]float32, v.dimension), nil
}

func (v *recordingVectorizer) EmbedText(_ context.Context, _ ImageModelConfig, text string) ([]float32, error) {
	v.textCalls++
	v.lastText = strings.ToLower(text)
	return make([]float32, v.dimension), nil
}

type poolingVectorizer struct {
	imageVector []float32
	textVectors map[string][]float32
	imageCalls  int
	textInputs  []string
}

func (v *poolingVectorizer) EmbedImage(context.Context, ImageModelConfig, string, []byte) ([]float32, error) {
	v.imageCalls++
	return append([]float32(nil), v.imageVector...), nil
}

func (v *poolingVectorizer) EmbedText(_ context.Context, _ ImageModelConfig, text string) ([]float32, error) {
	v.textInputs = append(v.textInputs, text)
	if vector, ok := v.textVectors[text]; ok {
		return append([]float32(nil), vector...), nil
	}
	return nil, nil
}
