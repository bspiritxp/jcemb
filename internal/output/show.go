package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bspiritxp/jcemb/internal/app/show"
)

type ShowJSONEnvelope struct {
	Found      bool               `json:"found"`
	File       ShowJSONFile       `json:"file,omitempty"`
	Collection ShowJSONCollection `json:"collection,omitempty"`
	Chunks     []ShowJSONChunk    `json:"chunks,omitempty"`
}

type ShowJSONFile struct {
	FilePath   string `json:"file_path"`
	RelPath    string `json:"rel_path"`
	FileName   string `json:"file_name"`
	DocType    string `json:"doc_type"`
	FileHash   string `json:"file_hash"`
	ChunkCount int    `json:"chunk_count"`
}

type ShowJSONCollection struct {
	CollectionID string `json:"collection_id"`
	RootDir      string `json:"root_dir"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	VectorDim    int    `json:"vector_dim"`
	FileType     string `json:"file_type"`
}

type ShowJSONChunk struct {
	ChunkID      string   `json:"chunk_id"`
	Tags         []string `json:"tags"`
	SemanticTags []string `json:"semantic_tags,omitempty"`
	TagVectorLen int      `json:"tag_vector_len,omitempty"`
	VectorLen    int      `json:"vector_len"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
}

func RenderShowText(writer io.Writer, result show.Result) error {
	if !result.Found {
		_, err := fmt.Fprintf(writer, "%s %s\n", Colorize(Yellow, "⚠"), Colorize(Dim, "未找到"))
		return err
	}

	if _, err := fmt.Fprintf(writer, "\n%s %s %s\n", Colorize(Cyan, "📄"), Boldf("File:"), Colorize(White, result.File.FilePath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "Rel path:"), result.File.RelPath); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "Doc type:"), result.File.DocType); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "File hash:"), result.File.FileHash); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "\n%s %s %s\n", Colorize(Magenta, "📚"), Boldf("Collection:"), Colorize(White, result.Collection.CollectionID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "Root dir:"), result.Collection.RootDir); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s/%s %s\n", Colorize(Dim, "•"), Colorize(Dim, "Provider/Model:"), result.Collection.Provider, result.Collection.Model, Colorize(Dim, fmt.Sprintf("(dim=%d)", result.Collection.VectorDim))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "File type:"), result.Collection.FileType); err != nil {
		return err
	}

	if len(result.Chunks) > 0 {
		if _, err := fmt.Fprintf(writer, "\n%s %s (%d chunk(s))\n", Colorize(Green, "🧩"), Boldf("Chunks:"), len(result.Chunks)); err != nil {
			return err
		}
		for i, chunk := range result.Chunks {
			if _, err := fmt.Fprintf(writer, "\n  %s %s %s\n", Colorize(Cyan, "▸"), Colorize(Bold, "Chunk "+strconv.Itoa(i+1)+":"), Colorize(Cyan, chunk.ChunkID)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(writer, "    %s %s %d\n", Colorize(Dim, "•"), Colorize(Dim, "Vector len:"), chunk.VectorLen); err != nil {
				return err
			}
			if chunk.Title != "" {
				if _, err := fmt.Fprintf(writer, "    %s %s %s\n", Colorize(Dim, "•"), Colorize(Dim, "Title:"), chunk.Title); err != nil {
					return err
				}
			}
			if len(chunk.Tags) > 0 {
				if _, err := fmt.Fprintf(writer, "    %s %s %s\n", Colorize(Yellow, "🏷"), Colorize(Dim, "Tags:"), Colorize(Yellow, strings.Join(chunk.Tags, ", "))); err != nil {
					return err
				}
			}
			content := previewText(chunk.Content)
			if content != "" {
				if _, err := fmt.Fprintf(writer, "    %s %s %s\n", Colorize(Green, "↳"), Colorize(Dim, "Content:"), Colorize(White, content)); err != nil {
					return err
				}
			}
			if i < len(result.Chunks)-1 {
				if _, err := fmt.Fprintf(writer, "\n"); err != nil {
					return err
				}
			}
		}
	}

	_, err := fmt.Fprintf(writer, "\n")
	return err
}

func RenderShowJSON(writer io.Writer, result show.Result) error {
	if !result.Found {
		_, err := writer.Write([]byte("{\n  \"found\": false\n}\n"))
		return err
	}

	envelope := ShowJSONEnvelope{
		Found: true,
		File: ShowJSONFile{
			FilePath:   result.File.FilePath,
			RelPath:    result.File.RelPath,
			FileName:   result.File.FileName,
			DocType:    result.File.DocType,
			FileHash:   result.File.FileHash,
			ChunkCount: result.File.ChunkCount,
		},
		Collection: ShowJSONCollection{
			CollectionID: result.Collection.CollectionID,
			RootDir:      result.Collection.RootDir,
			Provider:     result.Collection.Provider,
			Model:        result.Collection.Model,
			VectorDim:    result.Collection.VectorDim,
			FileType:     result.Collection.FileType,
		},
		Chunks: make([]ShowJSONChunk, 0, len(result.Chunks)),
	}

	for _, chunk := range result.Chunks {
		envelope.Chunks = append(envelope.Chunks, ShowJSONChunk{
			ChunkID:      chunk.ChunkID,
			Tags:         append([]string(nil), chunk.Tags...),
			SemanticTags: append([]string(nil), chunk.SemanticTags...),
			TagVectorLen: chunk.TagVectorLen,
			VectorLen:    chunk.VectorLen,
			Title:        chunk.Title,
			Content:      chunk.Content,
		})
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}
