package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	embedapp "github.com/bspiritxp/jcemb/internal/app/embed"
	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/output"
)

type EmbedRequest struct {
	Path            string
	Type            string
	Concurrency     int
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	Model           string
	Recursive       bool
	Force           bool
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
	JSON            bool
	Unique          bool
	Full            bool
	ThresholdAlpha  float64
	ThresholdDelta  float64
	MMRLambda       float64
	SearchWindow    int
}

type EmbedResult = embedapp.Result

type QueryResult = queryapp.Result

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
		Unique:          request.Unique,
		Full:            request.Full,
		ThresholdAlpha:  request.ThresholdAlpha,
		ThresholdDelta:  request.ThresholdDelta,
		MMRLambda:       request.MMRLambda,
		SearchWindow:    request.SearchWindow,
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
	result, err := RunQuery(context.Background(), request)
	if err != nil {
		return err
	}
	if request.JSON {
		return output.RenderQueryJSON(os.Stdout, result)
	}
	return output.RenderQueryText(os.Stdout, result)
}
