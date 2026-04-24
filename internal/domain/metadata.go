package domain

import (
	"fmt"
	"sort"
	"strings"
)

const (
	MetadataSourceKey     = "source"
	MetadataSourcePathKey = "source_path"
	MetadataSourceFileKey = "source_file"
	MetadataFilePathKey   = "file_path"
	MetadataRelPathKey    = "rel_path"
	MetadataFileNameKey   = "file_name"
	MetadataDocTypeKey    = "doc_type"
	MetadataFileHashKey   = "file_hash"
	MetadataDocHashKey    = "doc_hash"
	MetadataTitleKey      = "title"
	MetadataChunkIndexKey = "chunk_index"
	MetadataTitlePathKey  = "title_path"
	MetadataTagsKey       = "tags"
	MetadataYAMLKey       = "yaml"
)

type ChunkMetadata struct {
	Source     string
	FilePath   string
	RelPath    string
	FileName   string
	DocType    string
	FileHash   string
	Title      string
	ChunkIndex int
	TitlePath  []string
	Tags       []string
	YAML       map[string]any
}

func NewChunkMetadata(values map[string]any) (ChunkMetadata, error) {
	if values == nil {
		return ChunkMetadata{}, fmt.Errorf("metadata: values are required")
	}

	source, err := requiredStringWithAliases(values, MetadataSourceKey, MetadataSourcePathKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	filePath, err := requiredStringWithAliases(values, MetadataFilePathKey, MetadataSourceFileKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	relPath, err := requiredString(values, MetadataRelPathKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	fileName, err := requiredString(values, MetadataFileNameKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	docType, err := requiredString(values, MetadataDocTypeKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	fileHash, err := requiredStringWithAliases(values, MetadataFileHashKey, MetadataDocHashKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	chunkIndex, err := requiredInt(values, MetadataChunkIndexKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	title, err := optionalString(values, MetadataTitleKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	titlePath, err := optionalStringSlice(values, MetadataTitlePathKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	tags, err := optionalStringSlice(values, MetadataTagsKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	yamlValues, err := optionalStringAnyMap(values, MetadataYAMLKey)
	if err != nil {
		return ChunkMetadata{}, err
	}

	metadata := ChunkMetadata{
		Source:     source,
		FilePath:   filePath,
		RelPath:    relPath,
		FileName:   fileName,
		DocType:    docType,
		FileHash:   fileHash,
		Title:      title,
		ChunkIndex: chunkIndex,
		TitlePath:  normalizeStringSlice(titlePath),
		Tags:       NormalizeTags(tags),
		YAML:       cloneMap(yamlValues),
	}

	return metadata, metadata.Validate()
}

func (m ChunkMetadata) Validate() error {
	if strings.TrimSpace(m.Source) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataSourceKey)
	}
	if strings.TrimSpace(m.FilePath) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataFilePathKey)
	}
	if strings.TrimSpace(m.RelPath) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataRelPathKey)
	}
	if strings.TrimSpace(m.FileName) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataFileNameKey)
	}
	if strings.TrimSpace(m.DocType) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataDocTypeKey)
	}
	if strings.TrimSpace(m.FileHash) == "" {
		return fmt.Errorf("metadata: %s is required", MetadataFileHashKey)
	}
	if m.ChunkIndex < 0 {
		return fmt.Errorf("metadata: %s must be >= 0", MetadataChunkIndexKey)
	}
	if len(m.Tags) != len(NormalizeTags(m.Tags)) {
		return fmt.Errorf("metadata: %s must already be normalized", MetadataTagsKey)
	}
	for index, tag := range m.Tags {
		if tag != strings.TrimSpace(strings.ToLower(tag)) || tag == "" {
			return fmt.Errorf("metadata: %s[%d] must be normalized", MetadataTagsKey, index)
		}
	}
	for index, title := range m.TitlePath {
		if strings.TrimSpace(title) == "" {
			return fmt.Errorf("metadata: %s[%d] cannot be blank", MetadataTitlePathKey, index)
		}
	}
	if m.YAML == nil {
		return fmt.Errorf("metadata: %s cannot be nil", MetadataYAMLKey)
	}

	return nil
}

func (m ChunkMetadata) AsMap() map[string]any {
	return map[string]any{
		MetadataSourceKey:     m.Source,
		MetadataSourcePathKey: m.Source,
		MetadataSourceFileKey: m.FilePath,
		MetadataFilePathKey:   m.FilePath,
		MetadataRelPathKey:    m.RelPath,
		MetadataFileNameKey:   m.FileName,
		MetadataDocTypeKey:    m.DocType,
		MetadataFileHashKey:   m.FileHash,
		MetadataDocHashKey:    m.FileHash,
		MetadataTitleKey:      m.Title,
		MetadataChunkIndexKey: m.ChunkIndex,
		MetadataTitlePathKey:  append([]string(nil), m.TitlePath...),
		MetadataTagsKey:       append([]string(nil), m.Tags...),
		MetadataYAMLKey:       cloneMap(m.YAML),
	}
}

func requiredStringWithAliases(values map[string]any, keys ...string) (string, error) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}

		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("metadata: %s must be a string", key)
		}

		text = strings.TrimSpace(text)
		if text == "" {
			return "", fmt.Errorf("metadata: %s is required", keys[0])
		}

		return text, nil
	}

	return "", fmt.Errorf("metadata: %s is required", keys[0])
}

func optionalString(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return "", nil
	}

	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("metadata: %s must be a string", key)
	}

	return strings.TrimSpace(text), nil
}

func NormalizeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))

	for _, tag := range tags {
		value := strings.ToLower(strings.TrimSpace(tag))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	sort.Strings(normalized)

	return normalized
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func requiredString(values map[string]any, key string) (string, error) {
	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("metadata: %s is required", key)
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("metadata: %s must be a string", key)
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("metadata: %s is required", key)
	}

	return text, nil
}

func requiredInt(values map[string]any, key string) (int, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("metadata: %s is required", key)
	}

	integer, ok := value.(int)
	if !ok {
		return 0, fmt.Errorf("metadata: %s must be an int", key)
	}

	return integer, nil
}

func optionalStringSlice(values map[string]any, key string) ([]string, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return []string{}, nil
	}

	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...), nil
	case []any:
		items := make([]string, 0, len(value))
		for index, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("metadata: %s[%d] must be a string", key, index)
			}
			items = append(items, text)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("metadata: %s must be a []string", key)
	}
}

func optionalStringAnyMap(values map[string]any, key string) (map[string]any, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}

	value, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata: %s must be a map[string]any", key)
	}

	return value, nil
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}
