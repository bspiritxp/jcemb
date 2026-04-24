package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/bspiritxp/jcemb/internal/domain"
	internalfs "github.com/bspiritxp/jcemb/internal/fs"
)

type SourceDocument struct {
	Metadata DocumentMetadata
	Content  string
}

type DocumentMetadata struct {
	Source   string
	FilePath string
	RelPath  string
	FileName string
	DocType  string
	FileHash string
	Title    string
	Tags     []string
	YAML     map[string]any
}

func LoadFile(file internalfs.File) (SourceDocument, error) {
	content, err := os.ReadFile(file.FilePath)
	if err != nil {
		return SourceDocument{}, fmt.Errorf("metadata: read %s: %w", file.FilePath, err)
	}

	body, frontMatter, err := ExtractFrontMatter(string(content))
	if err != nil {
		return SourceDocument{}, fmt.Errorf("metadata: parse front matter for %s: %w", file.RelPath, err)
	}

	metadata, err := NewDocumentMetadata(file, content, frontMatter)
	if err != nil {
		return SourceDocument{}, err
	}

	return SourceDocument{
		Metadata: metadata,
		Content:  body,
	}, nil
}

func NewDocumentMetadata(file internalfs.File, rawContent []byte, frontMatter map[string]any) (DocumentMetadata, error) {
	if file.RelPath == "" {
		return DocumentMetadata{}, fmt.Errorf("metadata: relative path is required")
	}

	title, err := extractTitle(frontMatter)
	if err != nil {
		return DocumentMetadata{}, err
	}

	tags, err := extractTags(frontMatter)
	if err != nil {
		return DocumentMetadata{}, err
	}

	return DocumentMetadata{
		Source:   file.RelPath,
		FilePath: file.FilePath,
		RelPath:  file.RelPath,
		FileName: file.FileName,
		DocType:  file.DocType,
		FileHash: hashContent(rawContent),
		Title:    title,
		Tags:     tags,
		YAML:     cloneMap(frontMatter),
	}, nil
}

func (m DocumentMetadata) DomainDocument(content string) domain.Document {
	return domain.Document{
		Source:   m.Source,
		FilePath: m.FilePath,
		RelPath:  m.RelPath,
		FileName: m.FileName,
		DocType:  m.DocType,
		FileHash: m.FileHash,
		Title:    m.Title,
		Content:  content,
		Tags:     append([]string(nil), m.Tags...),
		YAML:     cloneMap(m.YAML),
	}
}

func (m DocumentMetadata) ChunkMetadata(chunkIndex int, titlePath []string) (domain.ChunkMetadata, error) {
	values := map[string]any{
		domain.MetadataSourceKey:     m.Source,
		domain.MetadataSourcePathKey: m.Source,
		domain.MetadataSourceFileKey: m.FilePath,
		domain.MetadataFilePathKey:   m.FilePath,
		domain.MetadataRelPathKey:    m.RelPath,
		domain.MetadataFileNameKey:   m.FileName,
		domain.MetadataDocTypeKey:    m.DocType,
		domain.MetadataFileHashKey:   m.FileHash,
		domain.MetadataDocHashKey:    m.FileHash,
		domain.MetadataTitleKey:      m.Title,
		domain.MetadataChunkIndexKey: chunkIndex,
		domain.MetadataTitlePathKey:  append([]string(nil), titlePath...),
		domain.MetadataTagsKey:       append([]string(nil), m.Tags...),
		domain.MetadataYAMLKey:       cloneMap(m.YAML),
	}

	return domain.NewChunkMetadata(values)
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func extractTitle(frontMatter map[string]any) (string, error) {
	raw, ok := frontMatter["title"]
	if !ok || raw == nil {
		return "", nil
	}

	title, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("metadata: front matter title must be a string")
	}

	return strings.TrimSpace(title), nil
}

func extractTags(frontMatter map[string]any) ([]string, error) {
	raw, ok := frontMatter["tags"]
	if !ok || raw == nil {
		return []string{}, nil
	}

	switch value := raw.(type) {
	case string:
		return domain.NormalizeTags([]string{value}), nil
	case []string:
		return domain.NormalizeTags(value), nil
	case []any:
		items := make([]string, 0, len(value))
		for index, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("metadata: front matter tags[%d] must be a string", index)
			}
			items = append(items, text)
		}
		return domain.NormalizeTags(items), nil
	default:
		return nil, fmt.Errorf("metadata: front matter tags must be a string or list of strings")
	}
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneValue(value)
	}

	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = cloneValue(item)
		}
		return items
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
