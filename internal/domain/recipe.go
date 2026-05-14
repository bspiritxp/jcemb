package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type ProviderConfig struct {
	Name    string
	Version string
	Options map[string]string
}

type ModelSpec struct {
	Provider   string
	Name       string
	Version    string
	Dimensions int
	Options    map[string]string
}

type SplitterSpec struct {
	Name    string
	Version string
	Options map[string]string
}

type TagExtractorRecipeSpec struct {
	Provider      string
	Model         string
	MaxTags       int
	MinTagLen     int
	MaxTagLen     int
	SkipIfHasYAML bool
}

type EmbedRecipe struct {
	Type         string
	Version      string
	Provider     ProviderConfig
	Model        ModelSpec
	Splitter     SplitterSpec
	TagExtractor *TagExtractorRecipeSpec
	Flags        map[string]bool
}

func (r EmbedRecipe) Validate() error {
	if strings.TrimSpace(r.Type) == "" {
		return fmt.Errorf("recipe: type is required")
	}
	if strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("recipe: version is required")
	}
	if strings.TrimSpace(r.Provider.Name) == "" {
		return fmt.Errorf("recipe: provider.name is required")
	}
	if strings.TrimSpace(r.Model.Name) == "" {
		return fmt.Errorf("recipe: model.name is required")
	}
	if strings.TrimSpace(r.Splitter.Name) == "" {
		return fmt.Errorf("recipe: splitter.name is required")
	}
	if r.Model.Dimensions < 0 {
		return fmt.Errorf("recipe: model.dimensions must be >= 0")
	}

	providerName := strings.TrimSpace(r.Provider.Name)
	modelProvider := strings.TrimSpace(r.Model.Provider)
	if modelProvider != "" && modelProvider != providerName {
		return fmt.Errorf("recipe: model.provider %q does not match provider.name %q", modelProvider, providerName)
	}

	return nil
}

func (r EmbedRecipe) Identifier() string {
	if err := r.Validate(); err != nil {
		return ""
	}

	providerOptions := canonicalStringMap(r.Provider.Options)
	modelOptions := canonicalStringMap(r.Model.Options)
	splitterOptions := canonicalStringMap(r.Splitter.Options)
	flags := canonicalBoolMap(r.Flags)
	modelProvider := strings.TrimSpace(r.Model.Provider)
	if modelProvider == "" {
		modelProvider = strings.TrimSpace(r.Provider.Name)
	}

	parts := []string{
		"type=" + strings.TrimSpace(r.Type),
		"version=" + strings.TrimSpace(r.Version),
		"provider.name=" + strings.TrimSpace(r.Provider.Name),
		"provider.version=" + strings.TrimSpace(r.Provider.Version),
		"provider.options=" + providerOptions,
		"model.provider=" + modelProvider,
		"model.name=" + strings.TrimSpace(r.Model.Name),
		"model.version=" + strings.TrimSpace(r.Model.Version),
		fmt.Sprintf("model.dimensions=%d", r.Model.Dimensions),
		"model.options=" + modelOptions,
		"splitter.name=" + strings.TrimSpace(r.Splitter.Name),
		"splitter.version=" + strings.TrimSpace(r.Splitter.Version),
		"splitter.options=" + splitterOptions,
	}
	if r.TagExtractor != nil {
		parts = append(parts, "tag_extractor="+canonicalTagExtractorRecipeSpec(r.TagExtractor))
	}
	parts = append(parts, "flags="+flags)

	return strings.Join(parts, "|")
}

func (r EmbedRecipe) Hash() string {
	identifier := r.Identifier()
	if identifier == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(identifier))
	return hex.EncodeToString(sum[:])
}

func canonicalStringMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}

	return "{" + strings.Join(parts, ",") + "}"
}

func canonicalBoolMap(values map[string]bool) string {
	if len(values) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%t", key, values[key]))
	}

	return "{" + strings.Join(parts, ",") + "}"
}

func canonicalTagExtractorRecipeSpec(spec *TagExtractorRecipeSpec) string {
	if spec == nil {
		return ""
	}

	parts := []string{
		"provider=" + strings.TrimSpace(spec.Provider),
		"model=" + strings.TrimSpace(spec.Model),
		fmt.Sprintf("max_tags=%d", spec.MaxTags),
		fmt.Sprintf("min_tag_len=%d", spec.MinTagLen),
		fmt.Sprintf("max_tag_len=%d", spec.MaxTagLen),
		fmt.Sprintf("skip_if_has_yaml=%t", spec.SkipIfHasYAML),
	}

	return "{" + strings.Join(parts, ",") + "}"
}
