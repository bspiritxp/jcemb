package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	internalfs "github.com/bspiritxp/jcemb/internal/fs"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/metadata"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	_ "github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	_ "github.com/bspiritxp/jcemb/internal/splitter/markdown"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
)

const (
	defaultType              = "md"
	defaultConcurrency       = 2
	defaultRecipeVersion     = "v1"
	defaultSplitterName      = "markdown"
	defaultSplitterVersion   = "v1"
	defaultVectorStoreName   = lancedb.Name
	minimumWorkerConcurrency = 1
)

type ProgressUpdate struct {
	Total     int
	Completed int
	Current   string
	Status    string
}

type Request struct {
	Path            string
	Type            string
	Concurrency     int
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	Model           string
	Recursive       bool
	Force           bool
	OnProgress      func(ProgressUpdate)
}

type Summary struct {
	Processed int
	Skipped   int
	Updated   int
	Deleted   int
	Errors    int
}

type FileError struct {
	RelPath string
	Err     error
}

type Result struct {
	Summary   Summary
	Failures  []FileError
	RootDir   string
	Recipe    domain.EmbedRecipe
	Store     domain.StoreConfig
	Rebuilt   bool
	Processed []string
	Skipped   []string
	Deleted   []string
}

type RunError struct {
	Result Result
}

type Dependencies struct {
	Scan            func(options internalfs.ScanOptions) ([]internalfs.File, error)
	LoadFile        func(file internalfs.File) (metadata.SourceDocument, error)
	LoadIndex       func(rootDir string) (index.Snapshot, error)
	SaveIndex       func(rootDir string, config domain.StoreConfig, files []domain.FileState) error
	ResolveAppPaths func() (jcpaths.AppPaths, error)
	SaveCollection  func(dataRoot string, entry index.CollectionEntry) error
	RemoveAll       func(path string) error
	Now             func() time.Time
	GetProvider     func(name string) (registry.ProviderFactory, error)
	GetSplitter     func(name string) (registry.SplitterFactory, error)
	GetVectorStore  func(name string) (registry.VectorStoreFactory, error)
	VectorStoreName string
	SplitterName    string
	SplitterVersion string
	RecipeVersion   string
	DefaultDocType  string
	DefaultWorkers  int
}

type Service struct {
	deps Dependencies
}

type pipelineState struct {
	rootDir         string
	storageRoot     string
	request         Request
	recipe          domain.EmbedRecipe
	snapshot        index.Snapshot
	hasSnapshot     bool
	rebuildRequired bool
	store           domain.VectorStore
	storeConfig     domain.StoreConfig
	states          map[string]domain.FileState
	dirty           bool
}

type fileJob struct {
	file internalfs.File
	mode jobMode
	prev domain.FileState
}

type jobMode string

const (
	jobSkip   jobMode = "skip"
	jobUpdate jobMode = "update"
)

type fileResult struct {
	relPath string
	mode    jobMode
	state   domain.FileState
	records []domain.VectorRecord
	err     error
	prev    domain.FileState
}

func NewService(deps Dependencies) *Service {
	if deps.Scan == nil {
		deps.Scan = internalfs.ScanMarkdown
	}
	if deps.LoadFile == nil {
		deps.LoadFile = metadata.LoadFile
	}
	if deps.LoadIndex == nil {
		deps.LoadIndex = index.Load
	}
	if deps.SaveIndex == nil {
		deps.SaveIndex = index.Save
	}
	if deps.ResolveAppPaths == nil {
		deps.ResolveAppPaths = jcpaths.ResolveAppPaths
	}
	if deps.SaveCollection == nil {
		deps.SaveCollection = index.SaveCollection
	}
	if deps.RemoveAll == nil {
		deps.RemoveAll = os.RemoveAll
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.GetProvider == nil {
		deps.GetProvider = registry.GetProvider
	}
	if deps.GetSplitter == nil {
		deps.GetSplitter = registry.GetSplitter
	}
	if deps.GetVectorStore == nil {
		deps.GetVectorStore = registry.GetVectorStore
	}
	if strings.TrimSpace(deps.VectorStoreName) == "" {
		deps.VectorStoreName = defaultVectorStoreName
	}
	if strings.TrimSpace(deps.SplitterName) == "" {
		deps.SplitterName = defaultSplitterName
	}
	if strings.TrimSpace(deps.SplitterVersion) == "" {
		deps.SplitterVersion = defaultSplitterVersion
	}
	if strings.TrimSpace(deps.RecipeVersion) == "" {
		deps.RecipeVersion = defaultRecipeVersion
	}
	if strings.TrimSpace(deps.DefaultDocType) == "" {
		deps.DefaultDocType = defaultType
	}
	if deps.DefaultWorkers < minimumWorkerConcurrency {
		deps.DefaultWorkers = defaultConcurrency
	}

	return &Service{deps: deps}
}

func (e *RunError) Error() string {
	message := fmt.Sprintf("embed: completed with %d file error(s)", e.Result.Summary.Errors)
	if len(e.Result.Failures) == 0 {
		return message
	}

	failures := append([]FileError(nil), e.Result.Failures...)
	sort.Slice(failures, func(i int, j int) bool {
		return failures[i].RelPath < failures[j].RelPath
	})

	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		details = append(details, fmt.Sprintf("  - %s: %v", failure.RelPath, failure.Err))
	}

	return message + "\n" + strings.Join(details, "\n")
}

func (e *RunError) Unwrap() error {
	if len(e.Result.Failures) == 0 {
		return nil
	}
	return e.Result.Failures[0].Err
}

func (s *Service) Run(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	normalized, err := s.normalizeRequest(request)
	if err != nil {
		return Result{}, err
	}

	rootDir, err := resolveRootDir(normalized.Path)
	if err != nil {
		return Result{}, err
	}

	recipe := s.buildRecipe(normalized)
	result := Result{RootDir: rootDir, Recipe: recipe}

	files, err := s.deps.Scan(internalfs.ScanOptions{RootPath: normalized.Path, Recursive: normalized.Recursive})
	if err != nil {
		return result, err
	}

	state, err := s.preparePipelineState(rootDir, normalized, recipe)
	if err != nil {
		return result, err
	}
	result.Rebuilt = state.rebuildRequired

	providerFactory, err := s.deps.GetProvider(normalized.Provider)
	if err != nil {
		return result, err
	}
	provider, err := providerFactory(domain.ProviderConfig{Name: normalized.Provider, Options: cloneStringMap(normalized.ProviderOptions)})
	if err != nil {
		return result, err
	}
	embedder, err := provider.NewEmbedder(domain.ModelSpec{Provider: normalized.Provider, Name: normalized.Model})
	if err != nil {
		return result, err
	}

	splitterFactory, err := s.deps.GetSplitter(s.deps.SplitterName)
	if err != nil {
		return result, err
	}
	splitter, err := splitterFactory(domain.SplitterSpec{Name: s.deps.SplitterName, Version: s.deps.SplitterVersion})
	if err != nil {
		return result, err
	}

	jobs, seen := s.buildJobs(files, normalized, state)
	result, err = s.processJobs(ctx, result, jobs, seen, state, splitter, embedder)
	if err != nil {
		return result, err
	}

	result.Store = state.storeConfig
	if state.store != nil {
		if closeErr := state.store.Close(); closeErr != nil {
			return result, closeErr
		}
	}
	if err := s.registerCollection(state); err != nil {
		return result, err
	}

	if result.Summary.Errors > 0 {
		return result, &RunError{Result: result}
	}
	return result, nil
}

func (s *Service) normalizeRequest(request Request) (Request, error) {
	normalized := request
	if strings.TrimSpace(normalized.Path) == "" {
		normalized.Path = "."
	}
	if strings.TrimSpace(normalized.Type) == "" {
		normalized.Type = s.deps.DefaultDocType
	}
	if normalized.Type != defaultType {
		return Request{}, fmt.Errorf("embed: unsupported type %q", normalized.Type)
	}
	if strings.TrimSpace(normalized.DataDir) == "" {
		paths, err := s.deps.ResolveAppPaths()
		if err != nil {
			return Request{}, err
		}
		normalized.DataDir = paths.DataRoot
	}
	expandedDataDir, err := jcpaths.ExpandUserHome(strings.TrimSpace(normalized.DataDir))
	if err != nil {
		return Request{}, fmt.Errorf("embed: resolve data dir: %w", err)
	}
	normalized.DataDir = filepath.Clean(expandedDataDir)
	if strings.TrimSpace(normalized.Provider) == "" {
		return Request{}, fmt.Errorf("embed: provider is required")
	}
	if strings.TrimSpace(normalized.Model) == "" {
		return Request{}, fmt.Errorf("embed: model is required")
	}
	normalized.ProviderOptions = cloneStringMap(normalized.ProviderOptions)
	if normalized.Concurrency < minimumWorkerConcurrency {
		normalized.Concurrency = s.deps.DefaultWorkers
	}
	return normalized, nil
}

func (s *Service) buildRecipe(request Request) domain.EmbedRecipe {
	return domain.EmbedRecipe{
		Type:    request.Type,
		Version: s.deps.RecipeVersion,
		Provider: domain.ProviderConfig{
			Name:    request.Provider,
			Options: cloneStringMap(request.ProviderOptions),
		},
		Model: domain.ModelSpec{
			Provider: request.Provider,
			Name:     request.Model,
		},
		Splitter: domain.SplitterSpec{
			Name:    s.deps.SplitterName,
			Version: s.deps.SplitterVersion,
		},
		Flags: map[string]bool{
			"recursive": request.Recursive,
			"force":     request.Force,
		},
	}
}

func (s *Service) preparePipelineState(rootDir string, request Request, recipe domain.EmbedRecipe) (*pipelineState, error) {
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	collectionID := index.CollectionIDForRoot(rootIdentity)
	storageRoot := jcpaths.CollectionStorageDir(request.DataDir, collectionID)
	state := &pipelineState{
		rootDir:     rootDir,
		storageRoot: storageRoot,
		request:     request,
		recipe:      recipe,
		states:      map[string]domain.FileState{},
		storeConfig: domain.StoreConfig{
			CollectionID: collectionID,
			RootIdentity: rootIdentity,
			RootDir:      rootDir,
			DataDir:      request.DataDir,
			Namespace:    s.deps.VectorStoreName,
			Provider:     request.Provider,
			Model:        request.Model,
			Splitter:     s.deps.SplitterName,
			DBVersion:    lancedb.DBVersion,
			CreatedAt:    s.deps.Now(),
			Flags: map[string]bool{
				"recursive": request.Recursive,
				"force":     request.Force,
			},
		},
	}

	snapshot, err := s.deps.LoadIndex(rootDir)
	switch {
	case err == nil:
		state.snapshot = snapshot
		state.hasSnapshot = true
		state.storeConfig.CreatedAt = snapshot.Config.CreatedAt
		state.storeConfig.VectorDim = snapshot.Config.VectorDim
		for _, fileState := range snapshot.Files {
			state.states[fileState.RelPath] = fileState
		}
	case errors.Is(err, index.ErrStateNotFound):
		return state, nil
	case errors.Is(err, index.ErrRebuildRequired):
		state.rebuildRequired = true
	default:
		return nil, err
	}

	if state.hasSnapshot && !state.rebuildRequired {
		if _, rebuild := index.ConfigNeedsRebuild(snapshot.Config, state.storeConfig); rebuild {
			state.rebuildRequired = true
		}
	}

	if state.rebuildRequired {
		if err := s.deps.RemoveAll(filepath.Join(storageRoot, index.DirectoryName)); err != nil {
			return nil, fmt.Errorf("embed: reset rebuild state: %w", err)
		}
		state.snapshot = index.Snapshot{}
		state.hasSnapshot = false
		state.states = map[string]domain.FileState{}
		state.storeConfig.CreatedAt = s.deps.Now()
		state.storeConfig.VectorDim = 0
	}
	return state, nil
}

func (s *Service) buildJobs(files []internalfs.File, request Request, state *pipelineState) ([]fileJob, map[string]struct{}) {
	jobs := make([]fileJob, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		seen[file.RelPath] = struct{}{}
		prev, ok := state.states[file.RelPath]
		if !request.Force && !state.rebuildRequired && ok {
			sourceDocument, err := s.deps.LoadFile(file)
			if err == nil {
				if _, needs := index.FileNeedsReindex(prev, sourceDocument.Metadata.FileHash, state.recipe); !needs {
					jobs = append(jobs, fileJob{file: file, mode: jobSkip, prev: prev})
					continue
				}
			}
		}
		jobs = append(jobs, fileJob{file: file, mode: jobUpdate, prev: prev})
	}
	return jobs, seen
}

func (s *Service) processJobs(ctx context.Context, result Result, jobs []fileJob, seen map[string]struct{}, state *pipelineState, splitter domain.Splitter, embedder domain.Embedder) (Result, error) {
	workerCount := state.request.Concurrency
	if workerCount > len(jobs) && len(jobs) > 0 {
		workerCount = len(jobs)
	}
	if workerCount < minimumWorkerConcurrency {
		workerCount = minimumWorkerConcurrency
	}

	jobCh := make(chan fileJob)
	resultCh := make(chan fileResult)

	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobCh {
				resultCh <- s.executeJob(ctx, job, state.recipe, splitter, embedder)
			}
		}()
	}

	go func() {
		for _, job := range jobs {
			jobCh <- job
		}
		close(jobCh)
		workers.Wait()
		close(resultCh)
	}()

	totalJobs := len(jobs)
	completedCount := 0
	if state.request.OnProgress != nil && totalJobs > 0 {
		state.request.OnProgress(ProgressUpdate{Total: totalJobs})
	}

	for fileResult := range resultCh {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		s.applyFileResult(ctx, &result, state, fileResult)
		completedCount++
		if state.request.OnProgress != nil {
			status := "completed"
			if fileResult.mode == jobSkip {
				status = "skipped"
			} else if fileResult.err != nil {
				status = "error"
			}
			state.request.OnProgress(ProgressUpdate{
				Total:     totalJobs,
				Completed: completedCount,
				Current:   fileResult.relPath,
				Status:    status,
			})
		}
	}

	s.reconcileMissingFiles(ctx, &result, state, seen)
	if state.dirty {
		files := make([]domain.FileState, 0, len(state.states))
		for _, fileState := range state.states {
			files = append(files, fileState)
		}
		if saveErr := s.deps.SaveIndex(state.rootDir, state.storeConfig, files); saveErr != nil {
			return result, saveErr
		}
	}

	sort.Strings(result.Processed)
	sort.Strings(result.Skipped)
	sort.Strings(result.Deleted)
	return result, nil
}

func (s *Service) executeJob(ctx context.Context, job fileJob, recipe domain.EmbedRecipe, splitter domain.Splitter, embedder domain.Embedder) fileResult {
	result := fileResult{relPath: job.file.RelPath, mode: job.mode, prev: job.prev}
	if job.mode == jobSkip {
		result.state = job.prev
		return result
	}

	sourceDocument, err := s.deps.LoadFile(job.file)
	if err != nil {
		result.err = err
		return result
	}
	document := sourceDocument.Metadata.DomainDocument(sourceDocument.Content)

	chunks, err := splitter.Split(ctx, document)
	if err != nil {
		result.err = err
		return result
	}

	records := make([]domain.VectorRecord, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))
	if len(chunks) > 0 {
		inputs := make([]domain.EmbedInput, 0, len(chunks))
		for _, chunk := range chunks {
			inputs = append(inputs, domain.EmbedInput{ChunkID: chunk.ID, Text: chunk.Content, Metadata: chunk.Metadata})
		}

		embeddings, err := embedder.Embed(ctx, domain.EmbedRequest{Recipe: recipe, Inputs: inputs})
		if err != nil {
			result.err = err
			return result
		}

		vectors := make(map[string][]float32, len(embeddings))
		for _, embedding := range embeddings {
			vectors[embedding.ChunkID] = append([]float32(nil), embedding.Vector...)
		}
		for _, chunk := range chunks {
			vector, ok := vectors[chunk.ID]
			if !ok {
				result.err = fmt.Errorf("embed: missing vector for chunk %s", chunk.ID)
				return result
			}
			records = append(records, domain.VectorRecord{Chunk: chunk, Vector: vector})
			chunkIDs = append(chunkIDs, chunk.ID)
		}
	}

	result.records = records
	result.state = domain.FileState{
		Source:        document.Source,
		FilePath:      document.FilePath,
		RelPath:       document.RelPath,
		FileName:      document.FileName,
		DocType:       document.DocType,
		FileHash:      document.FileHash,
		ModTime:       job.file.ModTime,
		RecipeHash:    recipe.Hash(),
		ChunkIDs:      chunkIDs,
		ChunkCount:    len(chunkIDs),
		LastIndexedAt: s.deps.Now(),
	}
	return result
}

func (s *Service) applyFileResult(ctx context.Context, result *Result, state *pipelineState, fileResult fileResult) {
	if fileResult.err != nil {
		result.Failures = append(result.Failures, FileError{RelPath: fileResult.relPath, Err: fileResult.err})
		result.Summary.Errors++
		return
	}
	if fileResult.mode == jobSkip {
		result.Summary.Processed++
		result.Summary.Skipped++
		state.states[fileResult.relPath] = fileResult.state
		result.Skipped = append(result.Skipped, fileResult.relPath)
		return
	}
	if err := s.commitFileResult(ctx, state, fileResult); err != nil {
		result.Failures = append(result.Failures, FileError{RelPath: fileResult.relPath, Err: err})
		result.Summary.Errors++
		return
	}
	state.states[fileResult.relPath] = fileResult.state
	state.dirty = true
	result.Summary.Processed++
	result.Summary.Updated++
	result.Processed = append(result.Processed, fileResult.relPath)
}

func (s *Service) commitFileResult(ctx context.Context, state *pipelineState, fileResult fileResult) error {
	vectorDim := state.storeConfig.VectorDim
	if len(fileResult.records) > 0 {
		candidateDim := len(fileResult.records[0].Vector)
		if vectorDim == 0 {
			vectorDim = candidateDim
		}
		if vectorDim != candidateDim {
			delete(state.states, fileResult.relPath)
			return fmt.Errorf("embed: vector dimension mismatch for %s: expected=%d actual=%d", fileResult.relPath, vectorDim, candidateDim)
		}
	}
	if err := s.ensureStore(ctx, state, vectorDim); err != nil {
		return err
	}
	if state.store != nil {
		if err := state.store.DeleteBySource(ctx, fileResult.state.Source); err != nil {
			return err
		}
		if len(fileResult.records) > 0 {
			if err := state.store.Upsert(ctx, fileResult.records); err != nil {
				delete(state.states, fileResult.relPath)
				return err
			}
		}
		if err := state.store.PutFileState(ctx, fileResult.state); err != nil {
			delete(state.states, fileResult.relPath)
			return err
		}
	}
	state.storeConfig.VectorDim = vectorDim
	return nil
}

func (s *Service) reconcileMissingFiles(ctx context.Context, result *Result, state *pipelineState, seen map[string]struct{}) {
	stale := make([]string, 0)
	for relPath := range state.states {
		if _, ok := seen[relPath]; !ok {
			stale = append(stale, relPath)
		}
	}
	sort.Strings(stale)

	for _, relPath := range stale {
		fileState := state.states[relPath]
		if err := s.ensureStore(ctx, state, state.storeConfig.VectorDim); err != nil {
			result.Failures = append(result.Failures, FileError{RelPath: relPath, Err: err})
			result.Summary.Errors++
			continue
		}
		if state.store != nil {
			if err := state.store.DeleteBySource(ctx, relPath); err != nil {
				result.Failures = append(result.Failures, FileError{RelPath: relPath, Err: err})
				result.Summary.Errors++
				continue
			}
			if err := state.store.DeleteFileState(ctx, relPath); err != nil {
				result.Failures = append(result.Failures, FileError{RelPath: relPath, Err: err})
				result.Summary.Errors++
				continue
			}
		}
		delete(state.states, relPath)
		state.dirty = true
		result.Summary.Deleted++
		result.Deleted = append(result.Deleted, fileState.RelPath)
	}
}

func (s *Service) ensureStore(ctx context.Context, state *pipelineState, vectorDim int) error {
	if state.store != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if vectorDim == 0 {
		return nil
	}
	factory, err := s.deps.GetVectorStore(s.deps.VectorStoreName)
	if err != nil {
		return err
	}
	config := state.storeConfig
	config.VectorDim = vectorDim
	store, err := factory(config)
	if err != nil {
		return err
	}
	state.store = store
	state.storeConfig = config
	return nil
}

func resolveRootDir(inputPath string) (string, error) {
	resolved, err := jcpaths.ResolveCollectionRoot(inputPath)
	if err != nil {
		return "", fmt.Errorf("embed: resolve path: %w", err)
	}
	return resolved.RootDir, nil
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

func (s *Service) registerCollection(state *pipelineState) error {
	return s.deps.SaveCollection(state.request.DataDir, index.CollectionEntry{
		CollectionID: state.storeConfig.CollectionID,
		RootIdentity: state.storeConfig.RootIdentity,
		RootDir:      state.rootDir,
		UpdatedAt:    s.deps.Now(),
	})
}
