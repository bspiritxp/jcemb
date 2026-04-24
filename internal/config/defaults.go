package config

import (
	"os"
	"time"
)

type DefaultsConfig struct {
	AppName        string
	DefaultPath    string
	IntegrationTag string
	IntegrationEnv string
	Ollama         OllamaDefaultsConfig
}

type OllamaDefaultsConfig struct {
	URL       string
	BatchSize int
	Timeout   time.Duration
}

func Defaults() DefaultsConfig {
	ollamaURL := os.Getenv("OLLAMA_HOST")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	return DefaultsConfig{
		AppName:        "jcemb",
		DefaultPath:    ".",
		IntegrationTag: "integration",
		IntegrationEnv: "INTEGRATION",
		Ollama: OllamaDefaultsConfig{
			URL:       ollamaURL,
			BatchSize: 8,
			Timeout:   30 * time.Second,
		},
	}
}
