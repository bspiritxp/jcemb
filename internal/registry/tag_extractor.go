package registry

import "github.com/bspiritxp/jcemb/internal/domain"

type TagExtractorRegistry struct {
	registry *typedRegistry[domain.TagExtractorFactory]
}

func NewTagExtractorRegistry() *TagExtractorRegistry {
	return &TagExtractorRegistry{registry: newTypedRegistry[domain.TagExtractorFactory]("tag extractor")}
}

func (r *TagExtractorRegistry) Register(name string, factory domain.TagExtractorFactory) error {
	return r.registry.Register(name, factory)
}

func (r *TagExtractorRegistry) MustRegister(name string, factory domain.TagExtractorFactory) {
	r.registry.MustRegister(name, factory)
}

func (r *TagExtractorRegistry) Get(name string) (domain.TagExtractorFactory, error) {
	return r.registry.Get(name)
}

func (r *TagExtractorRegistry) List() []string {
	return r.registry.List()
}

func (r *TagExtractorRegistry) Reset() {
	r.registry.Reset()
}

var defaultTagExtractorRegistry = NewTagExtractorRegistry()

func RegisterTagExtractor(name string, factory domain.TagExtractorFactory) error {
	return defaultTagExtractorRegistry.Register(name, factory)
}

func MustRegisterTagExtractor(name string, factory domain.TagExtractorFactory) {
	defaultTagExtractorRegistry.MustRegister(name, factory)
}

func GetTagExtractor(name string) (domain.TagExtractorFactory, error) {
	return defaultTagExtractorRegistry.Get(name)
}

func ListTagExtractors() []string {
	return defaultTagExtractorRegistry.List()
}

func ResetTagExtractors() {
	defaultTagExtractorRegistry.Reset()
}
