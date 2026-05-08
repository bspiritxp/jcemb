package markdown

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/bspiritxp/jcemb/internal/domain"
	gomarkdown "github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

const (
	Name                     = "markdown"
	DefaultVersion           = "v1"
	DefaultMaxChunkChars     = 1200
	DefaultChunkOverlapChars = 120
	DefaultShortFileMaxChars = 4000

	maxChunkCharsOption     = "max_chunk_chars"
	chunkOverlapCharsOption = "chunk_overlap_chars"
	ShortFileMaxCharsOption = "short_file_max_chars"
)

type Splitter struct {
	spec              domain.SplitterSpec
	recipeHash        string
	maxChunkChars     int
	overlapChars      int
	shortFileMaxChars int
	parserExtensions  parser.Extensions
}

type section struct {
	titlePath []string
	blocks    []string
}

func New(spec domain.SplitterSpec) (*Splitter, error) {
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return nil, err
	}

	maxChunkChars, err := intOption(normalized.Options, maxChunkCharsOption, DefaultMaxChunkChars)
	if err != nil {
		return nil, err
	}

	overlapChars, err := intOption(normalized.Options, chunkOverlapCharsOption, DefaultChunkOverlapChars)
	if err != nil {
		return nil, err
	}
	if overlapChars >= maxChunkChars {
		return nil, fmt.Errorf("splitter/markdown: %s must be smaller than %s", chunkOverlapCharsOption, maxChunkCharsOption)
	}
	shortFileMaxChars, err := intOption(normalized.Options, ShortFileMaxCharsOption, DefaultShortFileMaxChars)
	if err != nil {
		return nil, err
	}

	return &Splitter{
		spec:              normalized,
		recipeHash:        hashSplitterSpec(normalized),
		maxChunkChars:     maxChunkChars,
		overlapChars:      overlapChars,
		shortFileMaxChars: shortFileMaxChars,
		parserExtensions:  parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock,
	}, nil
}

func (s *Splitter) Split(ctx context.Context, document domain.Document) ([]domain.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	markdownBytes := []byte(gomarkdown.NormalizeNewlines([]byte(document.Content)))
	if chunk, ok, err := s.shortFileChunk(document, string(markdownBytes)); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return []domain.Chunk{chunk}, nil
	}
	root := gomarkdown.Parse(markdownBytes, parser.NewWithExtensions(s.parserExtensions))
	sections, err := s.collectSections(ctx, document, root)
	if err != nil {
		return nil, err
	}

	chunks := make([]domain.Chunk, 0, len(sections))
	chunkIndex := 0
	for _, candidate := range sections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		contents := s.splitSectionContent(candidate)
		for _, content := range contents {
			metadata, err := newChunkMetadata(document, chunkIndex, candidate.titlePath)
			if err != nil {
				return nil, err
			}

			chunkDocument := document
			chunkDocument.TitlePath = append([]string(nil), candidate.titlePath...)
			fingerprint := fingerprint(candidate.titlePath, content)
			chunks = append(chunks, domain.Chunk{
				ID:                 stableChunkID(document.RelPath, s.recipeHash, chunkIndex, fingerprint),
				Document:           chunkDocument,
				Content:            content,
				Metadata:           metadata,
				RecipeHash:         s.recipeHash,
				SectionFingerprint: fingerprint,
			})
			chunkIndex++
		}
	}

	return chunks, nil
}

func (s *Splitter) shortFileChunk(document domain.Document, normalizedContent string) (domain.Chunk, bool, error) {
	body := strings.TrimSpace(normalizedContent)
	content := assembleChunkContent(baseTitlePath(document), body)
	if content == "" || runeLen(content) > s.shortFileMaxChars {
		return domain.Chunk{}, false, nil
	}
	titlePath := baseTitlePath(document)
	metadata, err := newChunkMetadata(document, 0, titlePath)
	if err != nil {
		return domain.Chunk{}, false, err
	}
	chunkDocument := document
	chunkDocument.TitlePath = append([]string(nil), titlePath...)
	fingerprint := fingerprint(titlePath, content)
	return domain.Chunk{
		ID:                 stableChunkID(document.RelPath, s.recipeHash, 0, fingerprint),
		Document:           chunkDocument,
		Content:            content,
		Metadata:           metadata,
		RecipeHash:         s.recipeHash,
		SectionFingerprint: fingerprint,
	}, true, nil
}

func (s *Splitter) collectSections(ctx context.Context, document domain.Document, root ast.Node) ([]section, error) {
	children := root.GetChildren()
	baseTitlePath := baseTitlePath(document)
	headingPath := make([]string, 0, 6)
	current := section{titlePath: append([]string(nil), baseTitlePath...)}
	sections := make([]section, 0)

	flush := func(force bool) {
		if len(current.blocks) == 0 && len(current.titlePath) == 0 {
			return
		}
		if !force && len(current.blocks) == 0 {
			return
		}
		copied := section{
			titlePath: append([]string(nil), current.titlePath...),
			blocks:    append([]string(nil), current.blocks...),
		}
		sections = append(sections, copied)
		current = section{}
	}

	for _, child := range children {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		heading, ok := child.(*ast.Heading)
		if ok {
			flush(false)
			headingPath = updateHeadingPath(headingPath, heading.Level, headingText(heading))
			current.titlePath = append(append([]string(nil), baseTitlePath...), headingPath...)
			continue
		}

		block := strings.TrimSpace(renderBlock(child))
		if block == "" {
			continue
		}
		current.blocks = append(current.blocks, block)
		if len(current.titlePath) == 0 {
			current.titlePath = append([]string(nil), baseTitlePath...)
		}
	}

	flush(true)
	return sections, nil
}

func (s *Splitter) splitSectionContent(candidate section) []string {
	wholeBody := strings.TrimSpace(strings.Join(candidate.blocks, "\n\n"))
	wholeContent := assembleChunkContent(candidate.titlePath, wholeBody)
	if wholeContent == "" {
		return nil
	}
	if runeLen(wholeContent) <= s.maxChunkChars {
		return []string{wholeContent}
	}

	prefix := titlePrefix(candidate.titlePath)
	available := s.maxChunkChars - runeLen(prefix)
	if available <= 0 {
		available = s.maxChunkChars
	}

	parts := make([]string, 0, len(candidate.blocks))
	currentBlocks := make([]string, 0)

	emitCurrent := func() {
		if len(currentBlocks) == 0 {
			return
		}
		content := assembleChunkContent(candidate.titlePath, strings.Join(currentBlocks, "\n\n"))
		if content != "" {
			parts = append(parts, content)
		}
		currentBlocks = currentBlocks[:0]
	}

	for _, block := range candidate.blocks {
		candidateBlocks := append(append([]string(nil), currentBlocks...), block)
		if runeLen(assembleChunkContent(candidate.titlePath, strings.Join(candidateBlocks, "\n\n"))) <= s.maxChunkChars {
			currentBlocks = append(currentBlocks, block)
			continue
		}

		emitCurrent()
		if runeLen(assembleChunkContent(candidate.titlePath, block)) <= s.maxChunkChars {
			currentBlocks = append(currentBlocks, block)
			continue
		}

		for _, piece := range splitLongText(block, available, s.overlapChars) {
			content := assembleChunkContent(candidate.titlePath, piece)
			if content != "" {
				parts = append(parts, content)
			}
		}
	}

	emitCurrent()
	if len(parts) == 0 && len(candidate.titlePath) > 0 {
		return []string{strings.Join(candidate.titlePath, " > ")}
	}

	return parts
}

func newChunkMetadata(document domain.Document, chunkIndex int, titlePath []string) (domain.ChunkMetadata, error) {
	return domain.NewChunkMetadata(map[string]any{
		domain.MetadataSourceKey:     document.Source,
		domain.MetadataFilePathKey:   document.FilePath,
		domain.MetadataRelPathKey:    document.RelPath,
		domain.MetadataFileNameKey:   document.FileName,
		domain.MetadataDocTypeKey:    document.DocType,
		domain.MetadataFileHashKey:   document.FileHash,
		domain.MetadataTitleKey:      document.Title,
		domain.MetadataChunkIndexKey: chunkIndex,
		domain.MetadataTitlePathKey:  append([]string(nil), titlePath...),
		domain.MetadataTagsKey:       append([]string(nil), document.Tags...),
		domain.MetadataYAMLKey:       cloneMap(document.YAML),
	})
}

func normalizeSpec(spec domain.SplitterSpec) (domain.SplitterSpec, error) {
	if strings.TrimSpace(spec.Name) == "" {
		spec.Name = Name
	}
	if spec.Name != Name {
		return domain.SplitterSpec{}, fmt.Errorf("splitter/markdown: unsupported splitter name %q", spec.Name)
	}
	if strings.TrimSpace(spec.Version) == "" {
		spec.Version = DefaultVersion
	}
	if spec.Options == nil {
		spec.Options = map[string]string{}
	}
	return spec, nil
}

func intOption(options map[string]string, key string, fallback int) (int, error) {
	raw, ok := options[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("splitter/markdown: option %s must be an integer: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("splitter/markdown: option %s must be > 0", key)
	}
	return value, nil
}

func baseTitlePath(document domain.Document) []string {
	if len(document.TitlePath) > 0 {
		return normalizeStrings(document.TitlePath)
	}
	if trimmed := strings.TrimSpace(document.Title); trimmed != "" {
		return []string{trimmed}
	}
	if title, ok := document.YAML["title"].(string); ok {
		trimmed := strings.TrimSpace(title)
		if trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

func updateHeadingPath(current []string, level int, title string) []string {
	if level < 1 {
		level = 1
	}
	next := append([]string(nil), current...)
	if len(next) < level {
		padding := make([]string, level-len(next))
		next = append(next, padding...)
	} else {
		next = next[:level]
	}
	next[level-1] = title
	return normalizeStrings(next)
}

func headingText(heading *ast.Heading) string {
	title := strings.TrimSpace(renderInlineChildren(heading.GetChildren()))
	if title == "" {
		return "(untitled)"
	}
	return title
}

func assembleChunkContent(titlePath []string, body string) string {
	title := strings.TrimSpace(strings.Join(normalizeStrings(titlePath), " > "))
	body = strings.TrimSpace(body)
	switch {
	case title == "" && body == "":
		return ""
	case title == "":
		return body
	case body == "":
		return title
	default:
		return title + "\n\n" + body
	}
}

func titlePrefix(titlePath []string) string {
	title := strings.TrimSpace(strings.Join(normalizeStrings(titlePath), " > "))
	if title == "" {
		return ""
	}
	return title + "\n\n"
}

func splitLongText(text string, limit int, overlap int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if limit <= 0 || runeLen(trimmed) <= limit {
		return []string{trimmed}
	}

	runes := []rune(trimmed)
	parts := make([]string, 0, (len(runes)/limit)+1)
	for start := 0; start < len(runes); {
		end := min(len(runes), start+limit)
		if end < len(runes) {
			if breakpoint := findBreakPoint(runes, start, end); breakpoint > start {
				end = breakpoint
			}
		}

		piece := strings.TrimSpace(string(runes[start:end]))
		if piece != "" {
			parts = append(parts, piece)
		}
		if end >= len(runes) {
			break
		}

		nextStart := end - overlap
		if nextStart <= start {
			nextStart = end
		}
		for nextStart < len(runes) && unicode.IsSpace(runes[nextStart]) {
			nextStart++
		}
		start = nextStart
	}
	return parts
}

func findBreakPoint(runes []rune, start int, end int) int {
	minimum := start + (end-start)/2
	for index := end - 1; index > minimum; index-- {
		if unicode.IsSpace(runes[index]) {
			return index
		}
	}
	return end
}

func renderBlock(node ast.Node) string {
	switch typed := node.(type) {
	case *ast.Paragraph:
		return renderInlineChildren(typed.GetChildren())
	case *ast.CodeBlock:
		info := strings.TrimSpace(string(typed.Info))
		body := strings.TrimRight(string(typed.Literal), "\n")
		if info == "" {
			return "```\n" + body + "\n```"
		}
		return "```" + info + "\n" + body + "\n```"
	case *ast.BlockQuote:
		parts := renderChildrenAsBlocks(typed.GetChildren())
		if len(parts) == 0 {
			return ""
		}
		quoted := make([]string, 0, len(parts))
		for _, part := range parts {
			lines := strings.Split(part, "\n")
			for index, line := range lines {
				lines[index] = "> " + line
			}
			quoted = append(quoted, strings.Join(lines, "\n"))
		}
		return strings.Join(quoted, "\n\n")
	case *ast.List:
		return renderList(typed)
	case *ast.HTMLBlock:
		return strings.TrimSpace(string(typed.Literal))
	case *ast.HorizontalRule:
		return "---"
	case *ast.Table:
		rows := renderChildrenAsBlocks(typed.GetChildren())
		return strings.Join(rows, "\n")
	case *ast.TableRow:
		cells := make([]string, 0, len(typed.GetChildren()))
		for _, child := range typed.GetChildren() {
			cells = append(cells, strings.TrimSpace(renderBlock(child)))
		}
		return strings.Join(cells, " | ")
	case *ast.TableCell:
		return renderInlineChildren(typed.GetChildren())
	case *ast.Heading:
		return renderInlineChildren(typed.GetChildren())
	default:
		if children := renderChildrenAsBlocks(node.GetChildren()); len(children) > 0 {
			return strings.Join(children, "\n\n")
		}
		if leaf := node.AsLeaf(); leaf != nil {
			return strings.TrimSpace(string(leaf.Literal))
		}
		return ""
	}
}

func renderList(list *ast.List) string {
	items := make([]string, 0, len(list.GetChildren()))
	for index, child := range list.GetChildren() {
		item, ok := child.(*ast.ListItem)
		if !ok {
			continue
		}
		body := strings.TrimSpace(strings.Join(renderChildrenAsBlocks(item.GetChildren()), "\n"))
		if body == "" {
			continue
		}
		prefix := "- "
		if list.ListFlags&ast.ListTypeOrdered != 0 {
			prefix = strconv.Itoa(index+1) + ". "
		}
		items = append(items, prefix+indentFollowingLines(body, strings.Repeat(" ", len(prefix))))
	}
	return strings.Join(items, "\n")
}

func indentFollowingLines(value string, indent string) string {
	lines := strings.Split(value, "\n")
	for index := 1; index < len(lines); index++ {
		lines[index] = indent + lines[index]
	}
	return strings.Join(lines, "\n")
}

func renderChildrenAsBlocks(children []ast.Node) []string {
	parts := make([]string, 0, len(children))
	for _, child := range children {
		part := strings.TrimSpace(renderBlock(child))
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func renderInlineChildren(children []ast.Node) string {
	var builder strings.Builder
	for _, child := range children {
		builder.WriteString(renderInlineNode(child))
	}
	return strings.TrimSpace(builder.String())
}

func renderInlineNode(node ast.Node) string {
	switch typed := node.(type) {
	case *ast.Text:
		return string(typed.Literal)
	case *ast.Code:
		return "`" + string(typed.Literal) + "`"
	case *ast.Softbreak, *ast.Hardbreak:
		return "\n"
	case *ast.Link:
		label := strings.TrimSpace(renderInlineChildren(typed.GetChildren()))
		destination := strings.TrimSpace(string(typed.Destination))
		switch {
		case label == "":
			return destination
		case destination == "" || label == destination:
			return label
		default:
			return label + " <" + destination + ">"
		}
	case *ast.Image:
		alt := strings.TrimSpace(renderInlineChildren(typed.GetChildren()))
		if alt != "" {
			return alt
		}
		return strings.TrimSpace(string(typed.Destination))
	default:
		if children := node.GetChildren(); len(children) > 0 {
			return renderInlineChildren(children)
		}
		if leaf := node.AsLeaf(); leaf != nil {
			return string(leaf.Literal)
		}
		return ""
	}
}

func normalizeStrings(values []string) []string {
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

func hashSplitterSpec(spec domain.SplitterSpec) string {
	keys := make([]string, 0, len(spec.Options))
	for key := range spec.Options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"name=" + spec.Name, "version=" + spec.Version}
	for _, key := range keys {
		parts = append(parts, key+"="+spec.Options[key])
	}
	return hashString(strings.Join(parts, "|"))
}

func fingerprint(titlePath []string, content string) string {
	return hashString(strings.Join(normalizeStrings(titlePath), "\x1f") + "\x1e" + strings.TrimSpace(content))
}

func stableChunkID(relPath string, recipeHash string, chunkIndex int, sectionFingerprint string) string {
	return hashString(strings.Join([]string{relPath, recipeHash, strconv.Itoa(chunkIndex), sectionFingerprint}, "|"))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func runeLen(value string) int {
	return len([]rune(value))
}
