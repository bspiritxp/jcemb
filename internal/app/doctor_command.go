package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
)

type DoctorCommandRequest struct {
	Out        io.Writer
	ConfigPath string
	Settings   config.Settings
	JSON       bool
	CheckHTTP  func(context.Context, string) error
	RunPython  func(context.Context, string) error
	Stat       func(string) (os.FileInfo, error)
}

type DoctorCommandResult struct {
	ConfigPath string        `json:"config_path"`
	Checks     []DoctorCheck `json:"checks"`
}

type DoctorCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

func RunDoctorCommand(request DoctorCommandRequest) (DoctorCommandResult, error) {
	if request.Out == nil {
		request.Out = io.Discard
	}
	if strings.TrimSpace(request.ConfigPath) == "" {
		request.ConfigPath = config.Defaults().ConfigFile
	}
	if request.Settings == (config.Settings{}) {
		request.Settings = config.DefaultSettings()
	}
	if request.CheckHTTP == nil {
		request.CheckHTTP = checkHTTP
	}
	if request.RunPython == nil {
		request.RunPython = checkImagePython
	}
	if request.Stat == nil {
		request.Stat = os.Stat
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	checks := []DoctorCheck{
		checkConfigFile(request.ConfigPath, request.Stat),
		checkDataDir(request.Settings.DataDir, request.Stat),
		checkProviderModel(request.Settings),
		checkOpenAIConfig(request.Settings),
		checkOllama(ctx, request.Settings, request.CheckHTTP),
		checkImageConfig(ctx, request.Settings, request.RunPython),
	}
	result := DoctorCommandResult{ConfigPath: request.ConfigPath, Checks: checks}
	if request.JSON {
		encoder := json.NewEncoder(request.Out)
		encoder.SetIndent("", "  ")
		return result, encoder.Encode(result)
	}
	return result, renderDoctorText(request.Out, result)
}

func checkConfigFile(path string, stat func(string) (os.FileInfo, error)) DoctorCheck {
	if _, err := stat(path); err != nil {
		if os.IsNotExist(err) {
			return DoctorCheck{Name: "config", Status: "warn", Detail: path, Suggestion: "run `jcemb config` or `jcemb config --set-provider ...` to create it"}
		}
		return DoctorCheck{Name: "config", Status: "fail", Detail: err.Error()}
	}
	return DoctorCheck{Name: "config", Status: "ok", Detail: path}
}

func checkDataDir(path string, stat func(string) (os.FileInfo, error)) DoctorCheck {
	if strings.TrimSpace(path) == "" {
		return DoctorCheck{Name: "data_dir", Status: "fail", Suggestion: "set JCEMB_DATA_DIR or run `jcemb config --set-data-dir <path>`"}
	}
	info, err := stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(path)
			if parentInfo, parentErr := stat(parent); parentErr == nil && parentInfo.IsDir() {
				return DoctorCheck{Name: "data_dir", Status: "warn", Detail: path, Suggestion: "directory will be created on first write"}
			}
		}
		return DoctorCheck{Name: "data_dir", Status: "fail", Detail: err.Error()}
	}
	if !info.IsDir() {
		return DoctorCheck{Name: "data_dir", Status: "fail", Detail: path, Suggestion: "configured data_dir must be a directory"}
	}
	return DoctorCheck{Name: "data_dir", Status: "ok", Detail: path}
}

func checkProviderModel(settings config.Settings) DoctorCheck {
	if strings.TrimSpace(settings.Provider) == "" || strings.TrimSpace(settings.Model) == "" || settings.VectorDim <= 0 {
		return DoctorCheck{Name: "embedding", Status: "fail", Detail: fmt.Sprintf("%s/%s dim=%d", settings.Provider, settings.Model, settings.VectorDim), Suggestion: "set provider, model, and vector_dim in config"}
	}
	return DoctorCheck{Name: "embedding", Status: "ok", Detail: fmt.Sprintf("%s/%s dim=%d", settings.Provider, settings.Model, settings.VectorDim)}
}

func checkOpenAIConfig(settings config.Settings) DoctorCheck {
	usesOpenAI := strings.TrimSpace(settings.Provider) == config.OpenAIProviderName || strings.TrimSpace(settings.Image.Provider) == config.OpenAIProviderName
	if !usesOpenAI {
		return DoctorCheck{Name: "openai", Status: "ok", Detail: "not selected"}
	}
	if strings.TrimSpace(settings.OpenAI.BaseURL) == "" {
		return DoctorCheck{Name: "openai", Status: "fail", Suggestion: "set --set-openai-base-url"}
	}
	if strings.TrimSpace(settings.OpenAI.APIKey) == "" {
		return DoctorCheck{Name: "openai", Status: "fail", Detail: settings.OpenAI.BaseURL, Suggestion: "set OPENAI_API_KEY or `jcemb config --set-openai-api-key ...`"}
	}
	return DoctorCheck{Name: "openai", Status: "ok", Detail: fmt.Sprintf("%s dim=%d", settings.OpenAI.BaseURL, settings.OpenAI.Dimensions)}
}

func checkOllama(ctx context.Context, settings config.Settings, check func(context.Context, string) error) DoctorCheck {
	usesOllama := strings.TrimSpace(settings.Provider) == config.DefaultProviderName || strings.TrimSpace(settings.Image.VisionModel) != ""
	if !usesOllama {
		return DoctorCheck{Name: "ollama", Status: "ok", Detail: "not selected"}
	}
	url := strings.TrimRight(strings.TrimSpace(settings.Ollama.URL), "/")
	if url == "" {
		return DoctorCheck{Name: "ollama", Status: "fail", Suggestion: "set --set-ollama-url"}
	}
	if err := check(ctx, url+"/api/tags"); err != nil {
		return DoctorCheck{Name: "ollama", Status: "fail", Detail: err.Error(), Suggestion: "start Ollama and pull required models"}
	}
	return DoctorCheck{Name: "ollama", Status: "ok", Detail: url}
}

func checkImageConfig(ctx context.Context, settings config.Settings, runPython func(context.Context, string) error) DoctorCheck {
	if settings.Image.Dimensions <= 0 {
		return DoctorCheck{Name: "image", Status: "fail", Suggestion: "set --set-image-dimensions"}
	}
	if strings.TrimSpace(settings.Image.Python) == "" {
		return DoctorCheck{Name: "image", Status: "fail", Suggestion: "set --set-image-python"}
	}
	if strings.TrimSpace(settings.Image.Provider) == "openclip" || strings.TrimSpace(settings.Image.Provider) == "jina-clip" || strings.TrimSpace(settings.Image.Provider) == "jina" {
		if err := runPython(ctx, settings.Image.Python); err != nil {
			return DoctorCheck{Name: "image", Status: "fail", Detail: err.Error(), Suggestion: "install torch, open_clip_torch, and pillow in the configured Python environment"}
		}
	}
	return DoctorCheck{Name: "image", Status: "ok", Detail: fmt.Sprintf("%s/%s dim=%d vision=%s", settings.Image.Provider, settings.Image.Model, settings.Image.Dimensions, settings.Image.VisionModel)}
}

func checkHTTP(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func checkImagePython(ctx context.Context, python string) error {
	script := "import torch\nimport open_clip\nfrom PIL import Image\nprint('ok')\n"
	cmd := exec.CommandContext(ctx, python, "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func renderDoctorText(writer io.Writer, result DoctorCommandResult) error {
	if _, err := fmt.Fprintf(writer, "Doctor: %s\n", result.ConfigPath); err != nil {
		return err
	}
	for _, check := range result.Checks {
		line := fmt.Sprintf("[%s] %s", check.Status, check.Name)
		if check.Detail != "" {
			line += ": " + check.Detail
		}
		if check.Suggestion != "" {
			line += " (" + check.Suggestion + ")"
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return nil
}
