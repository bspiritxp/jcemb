package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	"github.com/stretchr/testify/require"
)

const (
	testProviderName = "test-fixture-provider"
	testModelName    = "fixture-model"
)

var (
	registerFixtureProviderOnce sync.Once
	fixtureProviderTracker      = &testProviderTracker{}
)

func TestEmbedAndQueryCommandsEndToEndWithOfflineFixtureProvider(t *testing.T) {
	registerFixtureProvider(t)
	fixtureProviderTracker.Reset()

	rootDir := copyFixtureTree(t, "basic")

	_, _, err := executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.NoError(t, err)
	require.Greater(t, fixtureProviderTracker.CallCount(), 0)
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, index.ConfigFileName))
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, index.IndexFileName))
	require.FileExists(t, filepath.Join(rootDir, index.DirectoryName, "lancedb.records.json"))

	textOutput, _, err := executeRootCommand(t, []string{"query", "go vector", "--path", rootDir, "--tags", "go,vector"})
	require.NoError(t, err)
	require.Contains(t, textOutput, "go vector")
	require.Contains(t, textOutput, testProviderName+"/"+testModelName)
	require.Contains(t, textOutput, "Tags (AND)")
	require.Contains(t, textOutput, "go, vector")
	require.Contains(t, textOutput, "docs/with-front-matter.md")
	require.Contains(t, textOutput, "Go Vector Guide")

	jsonOutput, _, err := executeRootCommand(t, []string{"query", "yaml", "--path", rootDir, "--json"})
	require.NoError(t, err)
	var envelope struct {
		Version   string `json:"version"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		VectorDim int    `json:"vector_dim"`
		Results   []struct {
			RelPath string `json:"rel_path"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &envelope))
	require.Equal(t, "v1", envelope.Version)
	require.Equal(t, testProviderName, envelope.Provider)
	require.Equal(t, testModelName, envelope.Model)
	require.Equal(t, 3, envelope.VectorDim)
	require.NotEmpty(t, envelope.Results)
	require.Equal(t, "docs/plain.md", envelope.Results[0].RelPath)
}

func TestEmbedCommandSupportsIncrementalSkipAndForce(t *testing.T) {
	registerFixtureProvider(t)
	fixtureProviderTracker.Reset()

	rootDir := copyFixtureTree(t, "basic")

	_, _, err := executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.NoError(t, err)
	firstCalls := fixtureProviderTracker.CallCount()
	require.Greater(t, firstCalls, 0)

	_, _, err = executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.NoError(t, err)
	require.Equal(t, firstCalls, fixtureProviderTracker.CallCount())

	_, _, err = executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive", "--force"})
	require.NoError(t, err)
	require.Greater(t, fixtureProviderTracker.CallCount(), firstCalls)
}

func TestEmbedCommandSyncsDeletedAndRenamedFiles(t *testing.T) {
	registerFixtureProvider(t)
	fixtureProviderTracker.Reset()

	rootDir := copyFixtureTree(t, "basic")
	require.NoError(t, os.Rename(filepath.Join(rootDir, "docs", "plain.md"), filepath.Join(rootDir, "docs", "renamed-note.md")))
	require.NoError(t, os.Remove(filepath.Join(rootDir, "docs", "delete-me.md")))

	_, _, err := executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.NoError(t, err)

	snapshot, err := index.Load(rootDir)
	require.NoError(t, err)
	require.Equal(t, []string{"docs/renamed-note.md", "docs/with-front-matter.md"}, collectRelPaths(snapshot.Files))

	jsonOutput, _, err := executeRootCommand(t, []string{"query", "yaml", "--path", rootDir, "--json"})
	require.NoError(t, err)
	require.Contains(t, jsonOutput, "docs/renamed-note.md")
	require.NotContains(t, jsonOutput, "docs/plain.md")
	require.NotContains(t, jsonOutput, "docs/delete-me.md")
}

func TestEmbedCommandReturnsRunErrorForInvalidYAMLFixture(t *testing.T) {
	registerFixtureProvider(t)
	fixtureProviderTracker.Reset()

	rootDir := copyFixtureTree(t, "invalid")

	_, _, err := executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "completed with 1 file error(s)")
	require.Contains(t, err.Error(), "docs/bad.md")
	require.Contains(t, err.Error(), "invalid yaml front matter")

	snapshot, loadErr := index.Load(rootDir)
	require.NoError(t, loadErr)
	require.Equal(t, []string{"docs/good.md"}, collectRelPaths(snapshot.Files))

	textOutput, _, queryErr := executeRootCommand(t, []string{"query", "good", "--path", rootDir})
	require.NoError(t, queryErr)
	require.Contains(t, textOutput, "docs/good.md")
	store, storeErr := lancedb.New(snapshot.Config)
	require.NoError(t, storeErr)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	results, searchErr := store.Search(context.Background(), domain.SearchQuery{Vector: []float32{1, 0, 0}, Limit: 10})
	require.NoError(t, searchErr)
	require.Len(t, results, 1)
	require.Equal(t, "docs/good.md", results[0].Chunk.Metadata.RelPath)
}

func TestQueryCommandSupportsSubdirectoryAndFilePathScopes(t *testing.T) {
	registerFixtureProvider(t)
	fixtureProviderTracker.Reset()

	rootDir := copyFixtureTree(t, "basic")
	_, _, err := executeRootCommand(t, []string{"embed", rootDir, "--provider", testProviderName, "--model", testModelName, "--recursive"})
	require.NoError(t, err)

	subdirPath := filepath.Join(rootDir, "docs")
	textOutput, _, err := executeRootCommand(t, []string{"query", "go vector", "--path", subdirPath})
	require.NoError(t, err)
	require.Contains(t, textOutput, "docs/with-front-matter.md")
	require.NotContains(t, textOutput, "notes/outside.md")

	filePath := filepath.Join(rootDir, "docs", "with-front-matter.md")
	jsonOutput, _, err := executeRootCommand(t, []string{"query", "go vector", "--path", filePath, "--json"})
	require.NoError(t, err)
	var envelope struct {
		Results []struct {
			RelPath string `json:"rel_path"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &envelope))
	require.NotEmpty(t, envelope.Results)
	for _, result := range envelope.Results {
		require.Equal(t, "docs/with-front-matter.md", result.RelPath)
	}
}

func TestQueryCommandRequiresVectorDBUnderPath(t *testing.T) {
	rootDir := copyFixtureTree(t, "empty")

	_, stderr, err := executeRootCommand(t, []string{"query", "go vector", "--path", rootDir})
	require.Error(t, err)
	require.Contains(t, err.Error(), ".vectordb not found")
	require.Contains(t, stderr, "Error: query: .vectordb not found")
	require.Contains(t, stderr, "Usage:")
}

type testProviderTracker struct {
	mu    sync.Mutex
	calls int
}

func (t *testProviderTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = 0
}

func (t *testProviderTracker) Add(count int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls += count
}

func (t *testProviderTracker) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type fixtureProvider struct{}

type fixtureEmbedder struct {
	model domain.ModelSpec
}

func (p fixtureProvider) Name() string {
	return testProviderName
}

func (p fixtureProvider) NewEmbedder(model domain.ModelSpec) (domain.Embedder, error) {
	if strings.TrimSpace(model.Provider) == "" {
		model.Provider = testProviderName
	}
	if strings.TrimSpace(model.Name) == "" {
		model.Name = testModelName
	}
	return fixtureEmbedder{model: model}, nil
}

func (e fixtureEmbedder) Provider() string {
	return e.model.Provider
}

func (e fixtureEmbedder) Model() domain.ModelSpec {
	return e.model
}

func (e fixtureEmbedder) Embed(_ context.Context, request domain.EmbedRequest) ([]domain.Embedding, error) {
	fixtureProviderTracker.Add(len(request.Inputs))
	results := make([]domain.Embedding, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		results = append(results, domain.Embedding{
			ChunkID: input.ChunkID,
			Vector:  vectorizeFixtureText(input.Text),
		})
	}
	return results, nil
}

func vectorizeFixtureText(text string) []float32 {
	lower := strings.ToLower(text)
	keywords := []string{"go", "vector", "yaml"}
	vector := make([]float32, 0, len(keywords))
	for _, keyword := range keywords {
		vector = append(vector, float32(strings.Count(lower, keyword)))
	}
	return vector
}

func registerFixtureProvider(t *testing.T) {
	t.Helper()
	registerFixtureProviderOnce.Do(func() {
		require.NoError(t, registry.RegisterProvider(testProviderName, func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
			return fixtureProvider{}, nil
		}))
	})
}

func executeRootCommand(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	cmd := NewRootCmd()
	stdoutBuffer := &bytes.Buffer{}
	stderrBuffer := &bytes.Buffer{}
	cmd.SetOut(stderrBuffer)
	cmd.SetErr(stderrBuffer)
	cmd.SetArgs(args)

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer

	readDone := make(chan string, 1)
	go func() {
		captured, _ := io.ReadAll(reader)
		readDone <- string(captured)
	}()

	execErr := cmd.Execute()
	require.NoError(t, writer.Close())
	os.Stdout = originalStdout
	stdoutBuffer.WriteString(<-readDone)
	require.NoError(t, reader.Close())

	return stdoutBuffer.String(), stderrBuffer.String(), execErr
}

func copyFixtureTree(t *testing.T, name string) string {
	t.Helper()
	sourceRoot := filepath.Join("testdata", "fixtures", name)
	destinationRoot := t.TempDir()
	require.NoError(t, copyDirectory(sourceRoot, destinationRoot))
	return destinationRoot
}

func copyDirectory(sourceRoot string, destinationRoot string) error {
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceRoot, entry.Name())
		destinationPath := filepath.Join(destinationRoot, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(destinationPath, 0o755); err != nil {
				return err
			}
			if err := copyDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destinationPath, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func collectRelPaths(files []domain.FileState) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelPath)
	}
	return paths
}
