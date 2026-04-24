package registry

import "github.com/bspiritxp/jcemb/internal/domain"

type VectorStoreFactory func(config domain.StoreConfig) (domain.VectorStore, error)

type VectorStoreRegistry struct {
	registry *typedRegistry[VectorStoreFactory]
}

func NewVectorStoreRegistry() *VectorStoreRegistry {
	return &VectorStoreRegistry{registry: newTypedRegistry[VectorStoreFactory]("vector store")}
}

func (r *VectorStoreRegistry) Register(name string, factory VectorStoreFactory) error {
	return r.registry.Register(name, factory)
}

func (r *VectorStoreRegistry) MustRegister(name string, factory VectorStoreFactory) {
	r.registry.MustRegister(name, factory)
}

func (r *VectorStoreRegistry) Get(name string) (VectorStoreFactory, error) {
	return r.registry.Get(name)
}

func (r *VectorStoreRegistry) List() []string {
	return r.registry.List()
}

func (r *VectorStoreRegistry) Reset() {
	r.registry.Reset()
}

var defaultVectorStoreRegistry = NewVectorStoreRegistry()

func RegisterVectorStore(name string, factory VectorStoreFactory) error {
	return defaultVectorStoreRegistry.Register(name, factory)
}

func MustRegisterVectorStore(name string, factory VectorStoreFactory) {
	defaultVectorStoreRegistry.MustRegister(name, factory)
}

func GetVectorStore(name string) (VectorStoreFactory, error) {
	return defaultVectorStoreRegistry.Get(name)
}

func ListVectorStores() []string {
	return defaultVectorStoreRegistry.List()
}

func ResetVectorStores() {
	defaultVectorStoreRegistry.Reset()
}
