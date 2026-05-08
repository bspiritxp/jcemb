package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
)

const QuerySchemaVersionV1 = "v1"

type QueryJSONEnvelope struct {
	Version   string            `json:"version"`
	Query     string            `json:"query"`
	RootPath  string            `json:"root_path"`
	Provider  string            `json:"provider"`
	Model     string            `json:"model"`
	FileType  string            `json:"file_type"`
	VectorDim int               `json:"vector_dim"`
	Tags      []string          `json:"tags"`
	Results   []QueryJSONResult `json:"results"`
}

type QueryJSONResult struct {
	Rank      int      `json:"rank"`
	Score     float64  `json:"score"`
	RelPath   string   `json:"rel_path"`
	TitlePath []string `json:"title_path"`
	Tags      []string `json:"tags"`
	ChunkID   string   `json:"chunk_id"`
	Preview   string   `json:"preview"`
}

func RenderQueryText(writer io.Writer, result queryapp.Result) error {
	if _, err := fmt.Fprintf(writer, "\n%s %s %s\n", Colorize(Cyan, "🔍"), Boldf("Query:"), Colorize(White, result.Query)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s/%s %s\n", Colorize(Magenta, "🤖"), Colorize(Dim, "Provider:"), result.Manifest.Provider, result.Manifest.Model, Colorize(Dim, fmt.Sprintf("(dim=%d)", result.Manifest.VectorDim))); err != nil {
		return err
	}
	if result.Manifest.FileType != "" {
		if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "File type:"), result.Manifest.FileType); err != nil {
			return err
		}
	}
	if len(result.Tags) > 0 {
		if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Yellow, "🏷"), Colorize(Dim, "Tags (AND):"), Colorize(Yellow, strings.Join(result.Tags, ", "))); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "%s\n", Colorize(Dim, strings.Repeat("─", 60))); err != nil {
		return err
	}

	if len(result.Results) == 0 {
		_, err := fmt.Fprintf(writer, "\n%s %s\n\n", Colorize(Yellow, "⚠"), Colorize(Dim, "No results found."))
		return err
	}

	for _, entry := range result.Results {
		titlePath := strings.Join(entry.Chunk.Metadata.TitlePath, " / ")
		if titlePath == "" {
			titlePath = entry.Chunk.Metadata.RelPath
		}

		scoreColor := ColorForScore(entry.Score)
		scoreStr := fmt.Sprintf("%.3f", entry.Score)

		if _, err := fmt.Fprintf(writer, "\n%s %s %s %s %s\n",
			Colorize(Cyan, "📄"),
			Boldf("[%d]", entry.Rank),
			Colorize(scoreColor, scoreStr),
			Colorize(Dim, "│"),
			Colorize(Cyan, entry.Chunk.Metadata.RelPath),
		); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(writer, "   %s %s\n", Colorize(Dim, "▸"), Colorize(Dim, titlePath)); err != nil {
			return err
		}
		if len(entry.Chunk.Metadata.Tags) > 0 {
			if _, err := fmt.Fprintf(writer, "   %s %s\n", Colorize(Yellow, "🏷"), Colorize(Yellow, strings.Join(entry.Chunk.Metadata.Tags, ", "))); err != nil {
				return err
			}
		}

		var content string
		if result.Full {
			content = strings.TrimSpace(entry.Chunk.Content)
		} else {
			content = previewText(entry.Chunk.Content)
		}
		if content != "" {
			if _, err := fmt.Fprintf(writer, "   %s %s\n", Colorize(Green, "↳"), Colorize(White, content)); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintf(writer, "\n%s %s\n\n", Colorize(Dim, "─"), Colorize(Dim, fmt.Sprintf("%d result(s)", len(result.Results))))
	return err
}

func RenderQueryJSON(writer io.Writer, result queryapp.Result) error {
	envelope := QueryJSONEnvelope{
		Version:   QuerySchemaVersionV1,
		Query:     result.Query,
		RootPath:  result.RootDir,
		Provider:  result.Manifest.Provider,
		Model:     result.Manifest.Model,
		FileType:  result.Manifest.FileType,
		VectorDim: result.Manifest.VectorDim,
		Tags:      append([]string(nil), result.Tags...),
		Results:   make([]QueryJSONResult, 0, len(result.Results)),
	}

	for _, entry := range result.Results {
		envelope.Results = append(envelope.Results, QueryJSONResult{
			Rank:      entry.Rank,
			Score:     entry.Score,
			RelPath:   entry.Chunk.Metadata.RelPath,
			TitlePath: append([]string(nil), entry.Chunk.Metadata.TitlePath...),
			Tags:      append([]string(nil), entry.Chunk.Metadata.Tags...),
			ChunkID:   entry.Chunk.ID,
			Preview:   previewText(entry.Chunk.Content),
		})
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

func RenderQueryTable(writer io.Writer, result queryapp.Result) error {
	columns := []struct {
		header string
		max    int
	}{
		{header: "Rank", max: 4},
		{header: "Score", max: 5},
		{header: "Path", max: 22},
		{header: "Title", max: 16},
		{header: "Tags", max: 8},
		{header: "Preview", max: 22},
	}

	rows := make([][]string, 0, len(result.Results))
	for _, entry := range result.Results {
		rows = append(rows, []string{
			fmt.Sprintf("%d", entry.Rank),
			fmt.Sprintf("%.3f", entry.Score),
			truncateTablePath(entry.Chunk.Metadata.RelPath, columns[2].max),
			tableCellText(strings.Join(entry.Chunk.Metadata.TitlePath, " / ")),
			tableCellText(strings.Join(entry.Chunk.Metadata.Tags, ", ")),
			tableCellText(tablePreviewText(entry.Chunk.Content)),
		})
	}

	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = column.max
	}

	if _, err := fmt.Fprintln(writer, queryTableBorder("┌", "┬", "┐", widths)); err != nil {
		return err
	}
	headers := make([]string, len(columns))
	for i, column := range columns {
		headers[i] = column.header
	}
	if _, err := fmt.Fprintln(writer, queryTableSingleLineRow(headers, widths)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, queryTableBorder("├", "┼", "┤", widths)); err != nil {
		return err
	}
	for _, row := range rows {
		if err := queryTableWrappedRow(writer, row, widths); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, queryTableBorder("└", "┴", "┘", widths))
	return err
}

func RenderQueryTSV(writer io.Writer, result queryapp.Result) error {
	if _, err := fmt.Fprintln(writer, "rank\tscore\trel_path\ttitle_path\ttags\tchunk_id\tpreview"); err != nil {
		return err
	}
	for _, entry := range result.Results {
		if _, err := fmt.Fprintf(writer, "%d\t%.6f\t%s\t%s\t%s\t%s\t%s\n",
			entry.Rank,
			entry.Score,
			tsvField(entry.Chunk.Metadata.RelPath),
			tsvField(strings.Join(entry.Chunk.Metadata.TitlePath, " / ")),
			tsvField(strings.Join(entry.Chunk.Metadata.Tags, ",")),
			tsvField(entry.Chunk.ID),
			tsvField(previewText(entry.Chunk.Content)),
		); err != nil {
			return err
		}
	}
	return nil
}

func RenderQueryTSVZ(writer io.Writer, result queryapp.Result) error {
	for _, entry := range result.Results {
		if _, err := fmt.Fprintf(writer, "%d\t%.6f\t%s\t%s\t%s\t%s\t%s\x00",
			entry.Rank,
			entry.Score,
			tsvZField(entry.Chunk.Metadata.RelPath),
			tsvZField(strings.Join(entry.Chunk.Metadata.TitlePath, " / ")),
			tsvZField(strings.Join(entry.Chunk.Metadata.Tags, ",")),
			tsvZField(entry.Chunk.ID),
			tsvZField(previewText(entry.Chunk.Content)),
		); err != nil {
			return err
		}
	}
	return nil
}

func tsvField(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func tsvZField(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\x00", "")
	return value
}

func queryTableBorder(left string, sep string, right string, widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("─", width+2)
	}
	return left + strings.Join(parts, sep) + right
}

func queryTableSingleLineRow(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		parts[i] = " " + padTableCell(cell, widths[i]) + " "
	}
	return "│" + strings.Join(parts, "│") + "│"
}

func queryTableWrappedRow(writer io.Writer, cells []string, widths []int) error {
	lines := make([][]string, len(cells))
	lineCount := 1
	for i, cell := range cells {
		lines[i] = wrapTableCell(cell, widths[i])
		if len(lines[i]) > lineCount {
			lineCount = len(lines[i])
		}
	}
	for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
		physical := make([]string, len(cells))
		for cellIndex := range cells {
			if lineIndex < len(lines[cellIndex]) {
				physical[cellIndex] = lines[cellIndex][lineIndex]
			}
		}
		if _, err := fmt.Fprintln(writer, queryTableSingleLineRow(physical, widths)); err != nil {
			return err
		}
	}
	return nil
}

func padTableCell(value string, width int) string {
	padding := width - visibleWidth(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func truncateTablePath(value string, maxWidth int) string {
	value = tableCellText(value)
	if visibleWidth(value) <= maxWidth {
		return value
	}
	const marker = "…"
	keepWidth := maxWidth - visibleWidth(marker)
	if keepWidth <= 0 {
		return marker
	}
	var kept []rune
	width := 0
	for _, r := range reverseRunes([]rune(value)) {
		rw := visibleWidth(string(r))
		if width+rw > keepWidth {
			break
		}
		kept = append(kept, r)
		width += rw
	}
	return marker + string(reverseRunes(kept))
}

func wrapTableCell(value string, width int) []string {
	value = tableCellText(value)
	if value == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{value}
	}

	lines := make([]string, 0, maxInt(1, visibleWidth(value)/width))
	var current strings.Builder
	currentWidth := 0
	for _, word := range strings.Fields(value) {
		wordWidth := visibleWidth(word)
		if wordWidth > width {
			if currentWidth > 0 {
				lines = append(lines, current.String())
				current.Reset()
				currentWidth = 0
			}
			chunks := wrapLongTableWord(word, width)
			lines = append(lines, chunks...)
			continue
		}
		if currentWidth == 0 {
			current.WriteString(word)
			currentWidth = wordWidth
			continue
		}
		if currentWidth+1+wordWidth > width {
			lines = append(lines, current.String())
			current.Reset()
			currentWidth = 0
		}
		if currentWidth > 0 {
			current.WriteByte(' ')
			currentWidth++
		}
		current.WriteString(word)
		currentWidth += wordWidth
	}
	if currentWidth > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func wrapLongTableWord(value string, width int) []string {
	if value == "" {
		return nil
	}
	lines := make([]string, 0, maxInt(1, visibleWidth(value)/width))
	var current strings.Builder
	currentWidth := 0
	for _, r := range value {
		rw := visibleWidth(string(r))
		if currentWidth > 0 && currentWidth+rw > width {
			lines = append(lines, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(r)
		currentWidth += rw
	}
	if currentWidth > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func tableCellText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func reverseRunes(values []rune) []rune {
	reversed := make([]rune, len(values))
	for i, r := range values {
		reversed[len(values)-1-i] = r
	}
	return reversed
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func previewText(content string) string {
	condensed := strings.Join(strings.Fields(content), " ")
	if condensed == "" {
		return ""
	}
	const maxPreviewRunes = 120
	runes := []rune(condensed)
	if len(runes) <= maxPreviewRunes {
		return condensed
	}
	return string(runes[:maxPreviewRunes-1]) + "…"
}

func tablePreviewText(content string) string {
	condensed := strings.Join(strings.Fields(content), " ")
	if condensed == "" {
		return ""
	}
	const maxPreviewRunes = 72
	runes := []rune(condensed)
	if len(runes) <= maxPreviewRunes {
		return condensed
	}
	return string(runes[:maxPreviewRunes-1]) + "…"
}
