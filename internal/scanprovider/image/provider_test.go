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

func TestOllamaCaptionImageContentConvertsWebPToPNG(t *testing.T) {
	content, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA=")
	require.NoError(t, err)

	converted, err := ollamaCaptionImageContent("tiny.webp", content)

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
