package registry

import "github.com/bspiritxp/jcemb/internal/domain"

type SplitterFactory func(spec domain.SplitterSpec) (domain.Splitter, error)

type SplitterRegistry struct {
	registry *typedRegistry[SplitterFactory]
}

func NewSplitterRegistry() *SplitterRegistry {
	return &SplitterRegistry{registry: newTypedRegistry[SplitterFactory]("splitter")}
}

func (r *SplitterRegistry) Register(name string, factory SplitterFactory) error {
	return r.registry.Register(name, factory)
}

func (r *SplitterRegistry) MustRegister(name string, factory SplitterFactory) {
	r.registry.MustRegister(name, factory)
}

func (r *SplitterRegistry) Get(name string) (SplitterFactory, error) {
	return r.registry.Get(name)
}

func (r *SplitterRegistry) List() []string {
	return r.registry.List()
}

func (r *SplitterRegistry) Reset() {
	r.registry.Reset()
}

var defaultSplitterRegistry = NewSplitterRegistry()

func RegisterSplitter(name string, factory SplitterFactory) error {
	return defaultSplitterRegistry.Register(name, factory)
}

func MustRegisterSplitter(name string, factory SplitterFactory) {
	defaultSplitterRegistry.MustRegister(name, factory)
}

func GetSplitter(name string) (SplitterFactory, error) {
	return defaultSplitterRegistry.Get(name)
}

func ListSplitters() []string {
	return defaultSplitterRegistry.List()
}

func ResetSplitters() {
	defaultSplitterRegistry.Reset()
}
