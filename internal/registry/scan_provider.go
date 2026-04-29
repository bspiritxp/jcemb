package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bspiritxp/jcemb/internal/domain"
)

type ScanProviderFactory func() domain.ScanProvider

type ScanProviderRegistry struct {
	providers map[string]ScanProviderFactory
	extension map[string]string
}

func NewScanProviderRegistry() *ScanProviderRegistry {
	return &ScanProviderRegistry{
		providers: map[string]ScanProviderFactory{},
		extension: map[string]string{},
	}
}

func (r *ScanProviderRegistry) Register(factory ScanProviderFactory) error {
	if factory == nil {
		return fmt.Errorf("scan provider: factory is required")
	}
	provider := factory()
	if provider == nil {
		return fmt.Errorf("scan provider: factory returned nil")
	}
	fileType := strings.TrimSpace(provider.FileType())
	if fileType == "" {
		return fmt.Errorf("scan provider: file type is required")
	}
	if _, exists := r.providers[fileType]; exists {
		return fmt.Errorf("scan provider %q already registered", fileType)
	}
	extensions := provider.Extensions()
	if len(extensions) == 0 {
		return fmt.Errorf("scan provider %q: at least one extension is required", fileType)
	}
	normalizedExtensions := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		normalized := normalizeExtension(extension)
		if normalized == "" {
			return fmt.Errorf("scan provider %q: extension is required", fileType)
		}
		if existing, exists := r.extension[normalized]; exists {
			return fmt.Errorf("scan provider %q: extension %q already registered by %q", fileType, normalized, existing)
		}
		normalizedExtensions = append(normalizedExtensions, normalized)
	}
	r.providers[fileType] = factory
	for _, extension := range normalizedExtensions {
		r.extension[extension] = fileType
	}
	return nil
}

func (r *ScanProviderRegistry) MustRegister(factory ScanProviderFactory) {
	if err := r.Register(factory); err != nil {
		panic(err)
	}
}

func (r *ScanProviderRegistry) Get(fileType string) (domain.ScanProvider, error) {
	trimmed := strings.TrimSpace(fileType)
	factory, ok := r.providers[trimmed]
	if !ok {
		return nil, fmt.Errorf("scan provider %q is not registered", trimmed)
	}
	return factory(), nil
}

func (r *ScanProviderRegistry) FileTypeForExtension(extension string) (string, bool) {
	fileType, ok := r.extension[normalizeExtension(extension)]
	return fileType, ok
}

func (r *ScanProviderRegistry) ExtensionMap() map[string]string {
	out := make(map[string]string, len(r.extension))
	for extension, fileType := range r.extension {
		out[extension] = fileType
	}
	return out
}

func (r *ScanProviderRegistry) ListFileTypes() []string {
	fileTypes := make([]string, 0, len(r.providers))
	for fileType := range r.providers {
		fileTypes = append(fileTypes, fileType)
	}
	sort.Strings(fileTypes)
	return fileTypes
}

func (r *ScanProviderRegistry) Reset() {
	r.providers = map[string]ScanProviderFactory{}
	r.extension = map[string]string{}
}

var defaultScanProviderRegistry = NewScanProviderRegistry()

func RegisterScanProvider(factory ScanProviderFactory) error {
	return defaultScanProviderRegistry.Register(factory)
}

func MustRegisterScanProvider(factory ScanProviderFactory) {
	defaultScanProviderRegistry.MustRegister(factory)
}

func GetScanProvider(fileType string) (domain.ScanProvider, error) {
	return defaultScanProviderRegistry.Get(fileType)
}

func ScanProviderFileTypeForExtension(extension string) (string, bool) {
	return defaultScanProviderRegistry.FileTypeForExtension(extension)
}

func ScanProviderExtensionMap() map[string]string {
	return defaultScanProviderRegistry.ExtensionMap()
}

func ListScanProviderFileTypes() []string {
	return defaultScanProviderRegistry.ListFileTypes()
}

func ResetScanProviders() {
	defaultScanProviderRegistry.Reset()
}

func normalizeExtension(extension string) string {
	trimmed := strings.TrimSpace(strings.ToLower(extension))
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, ".") {
		trimmed = "." + trimmed
	}
	return trimmed
}
