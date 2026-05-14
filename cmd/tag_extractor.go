package cmd

import (
	"strings"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/domain"
)

func appTagExtractorConfig(settings config.Settings) domain.TagExtractorConfig {
	if !settings.TagExtractor.Enabled {
		return domain.TagExtractorConfig{}
	}
	options := settings.ProviderOptions(settings.TagExtractor.Provider)
	for key, value := range settings.TagExtractor.Options {
		options[key] = value
	}
	return domain.TagExtractorConfig{
		Provider:      strings.TrimSpace(settings.TagExtractor.Provider),
		Model:         strings.TrimSpace(settings.TagExtractor.Model),
		Options:       options,
		Timeout:       settings.TagExtractor.Timeout,
		MaxTags:       settings.TagExtractor.MaxTags,
		MinTagLen:     settings.TagExtractor.MinTagLen,
		MaxTagLen:     settings.TagExtractor.MaxTagLen,
		SkipIfHasYAML: settings.TagExtractor.SkipIfHasYAML,
	}
}
