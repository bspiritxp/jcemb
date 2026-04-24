package registry

import "github.com/bspiritxp/jcemb/internal/domain"

type ProviderFactory func(config domain.ProviderConfig) (domain.EmbedderProvider, error)

type ProviderRegistry struct {
	registry *typedRegistry[ProviderFactory]
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{registry: newTypedRegistry[ProviderFactory]("provider")}
}

func (r *ProviderRegistry) Register(name string, factory ProviderFactory) error {
	return r.registry.Register(name, factory)
}

func (r *ProviderRegistry) MustRegister(name string, factory ProviderFactory) {
	r.registry.MustRegister(name, factory)
}

func (r *ProviderRegistry) Get(name string) (ProviderFactory, error) {
	return r.registry.Get(name)
}

func (r *ProviderRegistry) List() []string {
	return r.registry.List()
}

func (r *ProviderRegistry) Reset() {
	r.registry.Reset()
}

var defaultProviderRegistry = NewProviderRegistry()

func RegisterProvider(name string, factory ProviderFactory) error {
	return defaultProviderRegistry.Register(name, factory)
}

func MustRegisterProvider(name string, factory ProviderFactory) {
	defaultProviderRegistry.MustRegister(name, factory)
}

func GetProvider(name string) (ProviderFactory, error) {
	return defaultProviderRegistry.Get(name)
}

func ListProviders() []string {
	return defaultProviderRegistry.List()
}

func ResetProviders() {
	defaultProviderRegistry.Reset()
}
