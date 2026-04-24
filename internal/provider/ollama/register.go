package ollama

import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
	registry.MustRegisterProvider(Name, New)
}
