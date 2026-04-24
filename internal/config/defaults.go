package config

import "time"

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
	return DefaultsConfig{
		AppName:        "jcemb",
		DefaultPath:    ".",
		IntegrationTag: "integration",
		IntegrationEnv: "INTEGRATION",
		Ollama: OllamaDefaultsConfig{
			URL:       "http://localhost:11434",
			BatchSize: 8,
			Timeout:   30 * time.Second,
		},
	}
}
