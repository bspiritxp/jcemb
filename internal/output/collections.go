package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	collectionShortIDLen          = 12
	CollectionListSchemaVersionV1 = "v1"
)

type CollectionRow struct {
	CollectionID string
	RootDir      string
	FileType     string
	Provider     string
	Model        string
	VectorDim    int
	FileCount    int
	UpdatedAt    time.Time
	CreatedAt    time.Time
	Unreadable   bool
	LoadError    string
}

type CollectionListJSONEnvelope struct {
	Version     string                     `json:"version"`
	DataDir     string                     `json:"data_dir"`
	Collections []CollectionListJSONEntry  `json:"collections"`
}

type CollectionListJSONEntry struct {
	CollectionID string    `json:"collection_id"`
	RootDir      string    `json:"root_dir"`
	FileType     string    `json:"file_type"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	VectorDim    int       `json:"vector_dim"`
	FileCount    int       `json:"file_count"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedAt    time.Time `json:"created_at"`
	Unreadable   bool      `json:"unreadable,omitempty"`
	LoadError    string    `json:"load_error,omitempty"`
}

func RenderCollectionListJSON(writer io.Writer, dataDir string, collections []CollectionRow) error {
	envelope := CollectionListJSONEnvelope{
		Version:     CollectionListSchemaVersionV1,
		DataDir:     dataDir,
		Collections: make([]CollectionListJSONEntry, 0, len(collections)),
	}
	for _, info := range collections {
		envelope.Collections = append(envelope.Collections, CollectionListJSONEntry(info))
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

func RenderCollectionList(writer io.Writer, dataDir string, collections []CollectionRow) error {
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Cyan, "📚"), Boldf("Collections in"), Colorize(Dim, dataDir)); err != nil {
		return err
	}

	if len(collections) == 0 {
		_, err := fmt.Fprintf(writer, "  %s\n", Colorize(Dim, "(no collections)"))
		return err
	}

	headers := []string{"ID", "PATH", "TYPE", "PROVIDER", "MODEL", "DIM", "FILES", "UPDATED"}
	rows := make([][]string, 0, len(collections))
	for _, info := range collections {
		row := []string{
			shortCollectionID(info.CollectionID),
			info.RootDir,
			emptyDash(info.FileType),
			emptyDash(info.Provider),
			emptyDash(info.Model),
			dimOrDash(info.VectorDim),
			strconv.Itoa(info.FileCount),
			formatRelativeTime(info.UpdatedAt),
		}
		if info.Unreadable {
			row[3] = Colorize(Red, "<unreadable>")
		}
		rows = append(rows, row)
	}

	widths := computeColumnWidths(headers, rows)
	headerLine := formatRow(headers, widths, true)
	if _, err := fmt.Fprintln(writer, headerLine); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, Colorize(Dim, strings.Repeat("─", visibleWidth(headerLine)))); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, formatRow(row, widths, false)); err != nil {
			return err
		}
	}

	return nil
}

func shortCollectionID(id string) string {
	if len(id) <= collectionShortIDLen {
		return id
	}
	return id[:collectionShortIDLen]
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func dimOrDash(value int) string {
	if value <= 0 {
		return "-"
	}
	return strconv.Itoa(value)
}

func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func computeColumnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = visibleWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if w := visibleWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	return widths
}

func formatRow(cells []string, widths []int, header bool) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		padded := cell + strings.Repeat(" ", widths[i]-visibleWidth(cell))
		if header {
			parts[i] = Colorize(Cyan, padded)
		} else {
			parts[i] = padded
		}
	}
	return strings.Join(parts, "  ")
}

func visibleWidth(s string) int {
	stripped := stripANSIRunes(s)
	width := 0
	for _, r := range stripped {
		if r >= 0x1100 && (r <= 0x115F ||
			r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6) ||
			(r >= 0x1F300 && r <= 0x1FAFF)) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func stripANSIRunes(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if in {
			if (r >= '@' && r <= '~') || r == 'm' {
				in = false
			}
			continue
		}
		if r == 0x1b {
			in = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
