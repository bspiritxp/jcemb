package markdown

import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
	registry.MustRegisterScanProvider(New)
}
