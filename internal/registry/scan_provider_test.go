package registry

import (
	"context"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestScanProviderRegistryMapsExtensionsAndRejectsDuplicates(t *testing.T) {
	registry := NewScanProviderRegistry()

	require.NoError(t, registry.Register(func() domain.ScanProvider {
		return fakeScanProvider{fileType: "markdown", extensions: []string{".md"}}
	}))
	require.NoError(t, registry.Register(func() domain.ScanProvider {
		return fakeScanProvider{fileType: "image", extensions: []string{".png", "jpg"}}
	}))

	fileType, ok := registry.FileTypeForExtension(".JPG")
	require.True(t, ok)
	require.Equal(t, "image", fileType)
	require.Equal(t, map[string]string{".jpg": "image", ".md": "markdown", ".png": "image"}, registry.ExtensionMap())

	err := registry.Register(func() domain.ScanProvider {
		return fakeScanProvider{fileType: "photo", extensions: []string{".png"}}
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already registered")
}

type fakeScanProvider struct {
	fileType   string
	extensions []string
}

func (p fakeScanProvider) FileType() string {
	return p.fileType
}

func (p fakeScanProvider) Extensions() []string {
	return p.extensions
}

func (p fakeScanProvider) Recipe(domain.ScanProviderConfig) domain.EmbedRecipe {
	return domain.EmbedRecipe{}
}

func (p fakeScanProvider) BuildRecords(context.Context, domain.ScanProviderRequest) (domain.ScanProviderResult, error) {
	return domain.ScanProviderResult{}, nil
}
