package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	embedapp "github.com/bspiritxp/jcemb/internal/app/embed"
	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
	"github.com/bspiritxp/jcemb/internal/output"
)

type EmbedRequest struct {
	Path        string
	Type        string
	Concurrency int
	Provider    string
	Model       string
	Recursive   bool
	Force       bool
	OnProgress  func(embedapp.ProgressUpdate)
}

type QueryRequest struct {
	Text           string
	Tags           []string
	Limit          int
	Path           string
	JSON           bool
	Unique         bool
	Full           bool
	ThresholdAlpha float64
	ThresholdDelta float64
	MMRLambda      float64
	SearchWindow   int
}

type EmbedResult = embedapp.Result

type QueryResult = queryapp.Result

func RunEmbed(ctx context.Context, request EmbedRequest) (EmbedResult, error) {
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

	return fmt.Errorf("embed: completed with %d file error(s)\n%s", result.Summary.Errors, strings.Join(details, "\n"))
}

func RunQuery(ctx context.Context, request QueryRequest) (QueryResult, error) {
	service := queryapp.NewService(queryapp.Dependencies{})
	return service.Run(ctx, queryapp.Request{
		Text:           request.Text,
		Tags:           append([]string(nil), request.Tags...),
		Limit:          request.Limit,
		Path:           request.Path,
		Unique:         request.Unique,
		Full:           request.Full,
		ThresholdAlpha: request.ThresholdAlpha,
		ThresholdDelta: request.ThresholdDelta,
		MMRLambda:      request.MMRLambda,
		SearchWindow:   request.SearchWindow,
	})
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
