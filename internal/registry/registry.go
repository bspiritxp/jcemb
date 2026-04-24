package registry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type typedRegistry[T any] struct {
	mu      sync.RWMutex
	kind    string
	entries map[string]T
}

func newTypedRegistry[T any](kind string) *typedRegistry[T] {
	return &typedRegistry[T]{
		kind:    kind,
		entries: make(map[string]T),
	}
}

func (r *typedRegistry[T]) Register(name string, value T) error {
	key := normalizeRegistryKey(name)
	if key == "" {
		return fmt.Errorf("registry: %s name is required", r.kind)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[key]; exists {
		return fmt.Errorf("registry: %s %q already registered", r.kind, key)
	}

	r.entries[key] = value
	return nil
}

func (r *typedRegistry[T]) MustRegister(name string, value T) {
	if err := r.Register(name, value); err != nil {
		panic(err)
	}
}

func (r *typedRegistry[T]) Get(name string) (T, error) {
	key := normalizeRegistryKey(name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	value, ok := r.entries[key]
	if !ok {
		var zero T
		return zero, fmt.Errorf("registry: unknown %s %q", r.kind, key)
	}

	return value, nil
}

func (r *typedRegistry[T]) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func (r *typedRegistry[T]) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = make(map[string]T)
}

func normalizeRegistryKey(name string) string {
	return strings.TrimSpace(name)
}
