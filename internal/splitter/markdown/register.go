package markdown

import (
	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/registry"
)

func init() {
	registry.MustRegisterSplitter(Name, func(spec domain.SplitterSpec) (domain.Splitter, error) {
		return New(spec)
	})
}
