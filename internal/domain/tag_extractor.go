package domain

import (
	"context"
	"time"
)

const (
	DefaultTagExtractorMaxTags   = 8
	DefaultTagExtractorMinTagLen = 2
	DefaultTagExtractorMaxTagLen = 32
	DefaultTagExtractorTimeout   = 30 * time.Second
)

type TagExtractRequest struct {
	Document Document
	Config   TagExtractorConfig
}

type TagExtractResult struct {
	Tags []string
}

type TagExtractorConfig struct {
	Provider      string
	Model         string
	Options       map[string]string
	Timeout       time.Duration
	MaxTags       int
	MinTagLen     int
	MaxTagLen     int
	SkipIfHasYAML bool
}

type TagExtractorFactory func(TagExtractorConfig) (TagExtractor, error)

type TagExtractor interface {
	Extract(context.Context, TagExtractRequest) (TagExtractResult, error)
}
