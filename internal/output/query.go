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
	VectorDim int               `json:"vector_dim"`
	Tags      []string          `json:"tags"`
	Results   []QueryJSONResult `json:"results"`
}

type QueryJSONResult struct {
	Rank      int      `json:"rank"`
	Score     float64  `json:"score"`
	RelPath   string   `json:"rel_path"`
	TitlePath []string `json:"title_path"`
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
			ChunkID:   entry.Chunk.ID,
			Preview:   previewText(entry.Chunk.Content),
		})
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
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
