package image

import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
	registry.MustRegisterScanProvider(New)
}
