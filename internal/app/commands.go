package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	embedapp "github.com/bspiritxp/jcemb/internal/app/embed"
	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
	showapp "github.com/bspiritxp/jcemb/internal/app/show"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/output"
)

type EmbedRequest struct {
	Path            string
	Type            string
	Extensions      []string
	Concurrency     int
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	Model           string
	Recursive       bool
	Force           bool
	ExcludePatterns []string
	OnProgress      func(embedapp.ProgressUpdate)
}

type QueryRequest struct {
	Text            string
	Tags            []string
	Limit           int
	Path            string
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	FileType        string
	JSON            bool
	Unique          bool
	Full            bool
	ThresholdAlpha  float64
	ThresholdDelta  float64
	MMRLambda       float64
	SearchWindow    int
	Format          string
	Rerank          string
}

type EmbedResult = embedapp.Result

type QueryResult = queryapp.Result

type ShowRequest struct {
	FilePath string
	DataDir  string
}

type ShowResult = showapp.Result

func RunEmbed(ctx context.Context, request EmbedRequest) (EmbedResult, error) {
	loaded, err := config.Load()
	if err != nil {
		return EmbedResult{}, err
	}
	if strings.TrimSpace(request.DataDir) == "" {
		request.DataDir = loaded.Settings.DataDir
	}
	if strings.TrimSpace(request.Provider) == "" {
		request.Provider = loaded.Settings.Provider
	}
	if strings.TrimSpace(request.Model) == "" {
		request.Model = loaded.Settings.Model
	}
	if request.Provider == config.OpenAIProviderName && (strings.TrimSpace(request.Model) == "" || request.Model == config.DefaultModelName) {
		request.Model = config.OpenAIDefaultModel
	}
	if len(request.ProviderOptions) == 0 {
		request.ProviderOptions = loaded.Settings.ProviderOptions(request.Provider)
	}
	service := embedapp.NewService(embedapp.Dependencies{})
	return service.Run(ctx, embedapp.Request(request))
}

func Embed(request EmbedRequest) error {
	result, err := RunEmbed(context.Background(), request)
	if err == nil {
		return nil
	}
	if len(result.Failures) == 0 {
		return err
	}

	failures := append([]embedapp.FileError(nil), result.Failures...)
	sort.Slice(failures, func(i int, j int) bool {
		return failures[i].RelPath < failures[j].RelPath
	})

	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		details = append(details, fmt.Sprintf("  - %s: %v", failure.RelPath, failure.Err))
	}

	return fmt.Errorf("scan: completed with %d file error(s)\n%s", result.Summary.Errors, strings.Join(details, "\n"))
}

func RunQuery(ctx context.Context, request QueryRequest) (QueryResult, error) {
	loaded, err := config.Load()
	if err != nil {
		return QueryResult{}, err
	}
	if strings.TrimSpace(request.DataDir) == "" {
		request.DataDir = loaded.Settings.DataDir
	}
	if strings.TrimSpace(request.Provider) == "" {
		request.Provider = loaded.Settings.Provider
	}
	if len(request.ProviderOptions) == 0 {
		request.ProviderOptions = loaded.Settings.ProviderOptions(request.Provider)
	}
	service := queryapp.NewService(queryapp.Dependencies{})
	return service.Run(ctx, queryapp.Request{
		Text:            request.Text,
		Tags:            append([]string(nil), request.Tags...),
		Limit:           request.Limit,
		Path:            request.Path,
		DataDir:         request.DataDir,
		Provider:        request.Provider,
		ProviderOptions: cloneStringMap(request.ProviderOptions),
		FileType:        request.FileType,
		Unique:          request.Unique,
		Full:            request.Full,
		ThresholdAlpha:  request.ThresholdAlpha,
		ThresholdDelta:  request.ThresholdDelta,
		MMRLambda:       request.MMRLambda,
		SearchWindow:    request.SearchWindow,
		Rerank:          request.Rerank,
	})
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func Query(request QueryRequest) error {
	format := strings.TrimSpace(strings.ToLower(request.Format))
	switch format {
	case "", "text", "json", "tsv", "tsv-z", "table":
	default:
		return fmt.Errorf("query: format must be text, json, table, tsv, or tsv-z")
	}
	result, err := RunQuery(context.Background(), request)
	if err != nil {
		return err
	}
	switch format {
	case "", "text":
		if request.JSON {
			return output.RenderQueryJSON(os.Stdout, result)
		}
		return output.RenderQueryText(os.Stdout, result)
	case "json":
		return output.RenderQueryJSON(os.Stdout, result)
	case "table":
		return output.RenderQueryTable(os.Stdout, result)
	case "tsv":
		return output.RenderQueryTSV(os.Stdout, result)
	case "tsv-z":
		return output.RenderQueryTSVZ(os.Stdout, result)
	}
	return output.RenderQueryText(os.Stdout, result)
}

func RunShow(request ShowRequest) (ShowResult, error) {
	loaded, err := config.Load()
	if err != nil {
		return ShowResult{}, err
	}
	if strings.TrimSpace(request.DataDir) == "" {
		request.DataDir = loaded.Settings.DataDir
	}
	service := showapp.NewService(showapp.Dependencies{})
	return service.Run(context.Background(), showapp.Request{
		FilePath: request.FilePath,
		DataDir:  request.DataDir,
	})
}

func Show(request ShowRequest) error {
	result, err := RunShow(request)
	if err != nil {
		return err
	}
	return output.RenderShowText(os.Stdout, result)
}
