package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	_ "github.com/bspiritxp/jcemb/internal/provider/ollama"
	_ "github.com/bspiritxp/jcemb/internal/provider/openai"
	"github.com/bspiritxp/jcemb/internal/registry"
	_ "github.com/bspiritxp/jcemb/internal/scanprovider/image"
	_ "github.com/bspiritxp/jcemb/internal/scanprovider/markdown"
	_ "github.com/bspiritxp/jcemb/internal/splitter/markdown"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	_ "github.com/bspiritxp/jcemb/internal/tagextractor/ollama"
	_ "github.com/bspiritxp/jcemb/internal/tagextractor/openai"
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
	Extensions      []string
	Concurrency     int
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	Model           string
	TagExtractor    domain.TagExtractorConfig
	Recursive       bool
	Force           bool
	ExcludePatterns []string
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
	Summary         Summary
	Failures        []FileError
	RootDir         string
	Recipe          domain.EmbedRecipe
	Store           domain.StoreConfig
	CollectionCount int
	Rebuilt         bool
	Processed       []string
	Skipped         []string
	Deleted         []string
}

type RunError struct {
	Result Result
}

type Dependencies struct {
	Scan            func(options internalfs.ScanOptions) ([]internalfs.File, error)
	LoadIndex       func(rootDir string) (index.Snapshot, error)
	SaveIndex       func(rootDir string, config domain.StoreConfig, files []domain.FileState) error
	ResolveAppPaths func() (jcpaths.AppPaths, error)
	SaveCollection  func(dataRoot string, entry index.CollectionEntry) error
	RemoveAll       func(path string) error
	Now             func() time.Time
	GetProvider     func(name string) (registry.ProviderFactory, error)
	GetSplitter     func(name string) (registry.SplitterFactory, error)
	GetTagExtractor func(name string) (domain.TagExtractorFactory, error)
	GetVectorStore  func(name string) (registry.VectorStoreFactory, error)
	GetScanProvider func(fileType string) (domain.ScanProvider, error)
	ExtensionMap    func() map[string]string
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
	reconcile       bool
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
		deps.Scan = internalfs.ScanFiles
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
	if deps.GetTagExtractor == nil {
		deps.GetTagExtractor = registry.GetTagExtractor
	}
	if deps.GetVectorStore == nil {
		deps.GetVectorStore = registry.GetVectorStore
	}
	if deps.GetScanProvider == nil {
		deps.GetScanProvider = registry.GetScanProvider
	}
	if deps.ExtensionMap == nil {
		deps.ExtensionMap = registry.ScanProviderExtensionMap
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
	message := fmt.Sprintf("scan: completed with %d file error(s)", e.Result.Summary.Errors)
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

	scanRoot, err := resolveScanRoot(normalized.Path)
	if err != nil {
		return Result{}, err
	}
	rootDir := scanRoot.RootDir

	result := Result{RootDir: rootDir}

	extensions, err := filterExtensionMap(s.deps.ExtensionMap(), normalized.Extensions)
	if err != nil {
		return result, err
	}
	files, err := s.deps.Scan(internalfs.ScanOptions{
		RootPath:        normalized.Path,
		Recursive:       normalized.Recursive,
		Extensions:      extensions,
		ExcludePatterns: append([]string(nil), normalized.ExcludePatterns...),
	})
	if err != nil {
		return result, err
	}
	if normalized.Type != "" {
		if _, err := s.deps.GetScanProvider(normalized.Type); err != nil {
			return result, fmt.Errorf("scan: unsupported type %q", normalized.Type)
		}
		files = filterFilesByType(files, normalized.Type)
	}

	filesByType := groupFilesByType(files)
	fileTypes := sortedFileTypes(filesByType)
	result.CollectionCount = len(fileTypes)
	for _, fileType := range fileTypes {
		scanProvider, err := s.deps.GetScanProvider(fileType)
		if err != nil {
			return result, err
		}
		providerConfig := domain.ScanProviderConfig{
			FileType:        fileType,
			DataDir:         normalized.DataDir,
			Provider:        normalized.Provider,
			ProviderOptions: cloneStringMap(normalized.ProviderOptions),
			Model:           normalized.Model,
			TagExtractor:    cloneTagExtractorConfig(normalized.TagExtractor),
			Recursive:       normalized.Recursive,
			Force:           normalized.Force,
		}
		recipe := scanProvider.Recipe(providerConfig)
		if result.Recipe.Type == "" {
			result.Recipe = recipe
		}
		state, err := s.preparePipelineState(scanRoot, normalized, recipe, fileType)
		if err != nil {
			return result, err
		}
		result.Rebuilt = result.Rebuilt || state.rebuildRequired
		jobs, seen := s.buildJobs(filesByType[fileType], normalized, state)
		result, err = s.processJobs(ctx, result, jobs, seen, state, scanProvider, providerConfig)
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
		normalized.Type = ""
	}
	if normalized.Type == "md" {
		normalized.Type = "markdown"
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
		return Request{}, fmt.Errorf("scan: resolve data dir: %w", err)
	}
	normalized.DataDir = filepath.Clean(expandedDataDir)
	if strings.TrimSpace(normalized.Provider) == "" {
		return Request{}, fmt.Errorf("scan: provider is required")
	}
	if strings.TrimSpace(normalized.Model) == "" {
		return Request{}, fmt.Errorf("scan: model is required")
	}
	normalized.ProviderOptions = cloneStringMap(normalized.ProviderOptions)
	normalized.TagExtractor = cloneTagExtractorConfig(normalized.TagExtractor)
	if normalized.Concurrency < minimumWorkerConcurrency {
		normalized.Concurrency = s.deps.DefaultWorkers
	}
	return normalized, nil
}

func filterExtensionMap(registered map[string]string, requested []string) (map[string]string, error) {
	if len(requested) == 0 {
		return cloneStringMap(registered), nil
	}
	normalized := normalizeRequestedExtensions(requested)
	filtered := make(map[string]string, len(normalized))
	for _, extension := range normalized {
		fileType, ok := registered[extension]
		if !ok {
			return nil, fmt.Errorf("scan: unsupported extension %q", extension)
		}
		filtered[extension] = fileType
	}
	return filtered, nil
}

func normalizeRequestedExtensions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		for _, piece := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' }) {
			extension := strings.ToLower(strings.TrimSpace(piece))
			if extension == "" {
				continue
			}
			if !strings.HasPrefix(extension, ".") {
				extension = "." + extension
			}
			if extension == "." {
				continue
			}
			if _, ok := seen[extension]; ok {
				continue
			}
			seen[extension] = struct{}{}
			out = append(out, extension)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) preparePipelineState(scanRoot jcpaths.CollectionRoot, request Request, recipe domain.EmbedRecipe, fileType string) (*pipelineState, error) {
	rootDir := scanRoot.RootDir
	rootIdentity := jcpaths.NormalizeStoredPath(rootDir)
	collectionID := index.CollectionIDForRootAndFileType(rootIdentity, fileType)
	storageRoot := jcpaths.CollectionStorageDir(request.DataDir, collectionID)
	state := &pipelineState{
		rootDir:     rootDir,
		storageRoot: storageRoot,
		request:     request,
		recipe:      recipe,
		reconcile:   scanRoot.IsDir && jcpaths.NormalizeStoredPath(scanRoot.InputPath) == rootIdentity,
		states:      map[string]domain.FileState{},
		storeConfig: domain.StoreConfig{
			CollectionID:    collectionID,
			RootIdentity:    rootIdentity,
			RootDir:         rootDir,
			DataDir:         request.DataDir,
			FileType:        fileType,
			Namespace:       s.deps.VectorStoreName,
			Provider:        recipe.Provider.Name,
			ProviderOptions: cloneStringMap(recipe.Provider.Options),
			Model:           recipe.Model.Name,
			Splitter:        recipe.Splitter.Name,
			DBVersion:       lancedb.DBVersion,
			CreatedAt:       s.deps.Now(),
			Flags: map[string]bool{
				"recursive": request.Recursive,
				"force":     request.Force,
			},
		},
	}

	snapshot, err := s.deps.LoadIndex(storageRoot)
	if errors.Is(err, index.ErrStateNotFound) && fileType == "markdown" {
		if fallbackSnapshot, fallbackErr := s.deps.LoadIndex(rootDir); fallbackErr == nil {
			snapshot = fallbackSnapshot
			err = nil
		}
	}
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
		if err := state.ensureCanRebuildCollection(); err != nil {
			return nil, err
		}
		state.rebuildRequired = true
	default:
		return nil, err
	}

	if state.hasSnapshot && !state.rebuildRequired {
		if _, rebuild := index.ConfigNeedsRebuild(snapshot.Config, state.storeConfig); rebuild {
			if err := state.ensureCanRebuildCollection(); err != nil {
				return nil, err
			}
			state.rebuildRequired = true
		}
	}

	if state.rebuildRequired {
		if err := s.deps.RemoveAll(filepath.Join(storageRoot, index.DirectoryName)); err != nil {
			return nil, fmt.Errorf("scan: reset rebuild state: %w", err)
		}
		state.snapshot = index.Snapshot{}
		state.hasSnapshot = false
		state.states = map[string]domain.FileState{}
		state.storeConfig.CreatedAt = s.deps.Now()
		state.storeConfig.VectorDim = 0
	}
	return state, nil
}

func (s *pipelineState) ensureCanRebuildCollection() error {
	if s.reconcile {
		return nil
	}
	return fmt.Errorf("scan: collection rebuild requires scanning the collection root %s", s.rootDir)
}

func (s *Service) buildJobs(files []internalfs.File, request Request, state *pipelineState) ([]fileJob, map[string]struct{}) {
	jobs := make([]fileJob, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		seen[file.RelPath] = struct{}{}
		prev, ok := state.states[file.RelPath]
		if !request.Force && !state.rebuildRequired && ok {
			fileHash, err := computeFileHash(file.FilePath)
			if err == nil {
				if _, needs := index.FileNeedsReindex(prev, fileHash, state.recipe); !needs {
					jobs = append(jobs, fileJob{file: file, mode: jobSkip, prev: prev})
					continue
				}
			}
		}
		jobs = append(jobs, fileJob{file: file, mode: jobUpdate, prev: prev})
	}
	return jobs, seen
}

func (s *Service) processJobs(ctx context.Context, result Result, jobs []fileJob, seen map[string]struct{}, state *pipelineState, scanProvider domain.ScanProvider, providerConfig domain.ScanProviderConfig) (Result, error) {
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
				resultCh <- s.executeJob(ctx, job, state.recipe, scanProvider, providerConfig)
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

	if state.reconcile {
		s.reconcileMissingFiles(ctx, &result, state, seen)
	}
	if state.dirty {
		files := make([]domain.FileState, 0, len(state.states))
		for _, fileState := range state.states {
			files = append(files, fileState)
		}
		if saveErr := s.deps.SaveIndex(state.storageRoot, state.storeConfig, files); saveErr != nil {
			return result, saveErr
		}
	}

	sort.Strings(result.Processed)
	sort.Strings(result.Skipped)
	sort.Strings(result.Deleted)
	return result, nil
}

func (s *Service) executeJob(ctx context.Context, job fileJob, recipe domain.EmbedRecipe, scanProvider domain.ScanProvider, providerConfig domain.ScanProviderConfig) fileResult {
	result := fileResult{relPath: job.file.RelPath, mode: job.mode, prev: job.prev}
	if job.mode == jobSkip {
		result.state = job.prev
		return result
	}

	providerResult, err := scanProvider.BuildRecords(ctx, domain.ScanProviderRequest{
		File: domain.SourceFile{
			RootDir:  job.file.RootDir,
			FilePath: job.file.FilePath,
			RelPath:  job.file.RelPath,
			FileName: job.file.FileName,
			DocType:  job.file.DocType,
			ModTime:  job.file.ModTime,
		},
		Config:          providerConfig,
		Recipe:          recipe,
		Now:             s.deps.Now,
		GetProvider:     providerFactoryAdapter(s.deps.GetProvider),
		GetSplitter:     splitterFactoryAdapter(s.deps.GetSplitter),
		GetTagExtractor: tagExtractorAdapter(s.deps.GetTagExtractor),
	})
	if err != nil {
		result.err = err
		return result
	}
	result.records = providerResult.Records
	result.state = providerResult.State
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
			return fmt.Errorf("scan: vector dimension mismatch for %s: expected=%d actual=%d", fileResult.relPath, vectorDim, candidateDim)
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
	resolved, err := resolveScanRoot(inputPath)
	if err != nil {
		return "", err
	}
	return resolved.RootDir, nil
}

func resolveScanRoot(inputPath string) (jcpaths.CollectionRoot, error) {
	resolved, err := jcpaths.ResolveCollectionRoot(inputPath)
	if err != nil {
		return jcpaths.CollectionRoot{}, fmt.Errorf("scan: resolve path: %w", err)
	}
	return resolved, nil
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

func cloneTagExtractorConfig(config domain.TagExtractorConfig) domain.TagExtractorConfig {
	config.Provider = strings.TrimSpace(config.Provider)
	config.Model = strings.TrimSpace(config.Model)
	config.Options = cloneStringMap(config.Options)
	return config
}

func (s *Service) registerCollection(state *pipelineState) error {
	return s.deps.SaveCollection(state.request.DataDir, index.CollectionEntry{
		CollectionID: state.storeConfig.CollectionID,
		RootIdentity: state.storeConfig.RootIdentity,
		RootDir:      state.rootDir,
		FileType:     state.storeConfig.FileType,
		UpdatedAt:    s.deps.Now(),
	})
}

func filterFilesByType(files []internalfs.File, fileType string) []internalfs.File {
	normalized := normalizeFileType(fileType)
	if normalized == "" {
		return files
	}
	filtered := make([]internalfs.File, 0, len(files))
	for _, file := range files {
		if normalizeFileType(file.DocType) == normalized {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func groupFilesByType(files []internalfs.File) map[string][]internalfs.File {
	grouped := make(map[string][]internalfs.File)
	for _, file := range files {
		fileType := normalizeFileType(file.DocType)
		if fileType == "" {
			continue
		}
		file.DocType = fileType
		grouped[fileType] = append(grouped[fileType], file)
	}
	return grouped
}

func sortedFileTypes(filesByType map[string][]internalfs.File) []string {
	fileTypes := make([]string, 0, len(filesByType))
	for fileType := range filesByType {
		fileTypes = append(fileTypes, fileType)
	}
	sort.Strings(fileTypes)
	return fileTypes
}

func normalizeFileType(fileType string) string {
	trimmed := strings.TrimSpace(fileType)
	if trimmed == "" {
		return ""
	}
	if trimmed == "md" {
		return "markdown"
	}
	return trimmed
}

func computeFileHash(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func providerFactoryAdapter(getProvider func(name string) (registry.ProviderFactory, error)) func(string) (func(domain.ProviderConfig) (domain.EmbedderProvider, error), error) {
	return func(name string) (func(domain.ProviderConfig) (domain.EmbedderProvider, error), error) {
		factory, err := getProvider(name)
		if err != nil {
			return nil, err
		}
		return func(config domain.ProviderConfig) (domain.EmbedderProvider, error) {
			return factory(config)
		}, nil
	}
}

func splitterFactoryAdapter(getSplitter func(name string) (registry.SplitterFactory, error)) func(string) (func(domain.SplitterSpec) (domain.Splitter, error), error) {
	return func(name string) (func(domain.SplitterSpec) (domain.Splitter, error), error) {
		factory, err := getSplitter(name)
		if err != nil {
			return nil, err
		}
		return func(spec domain.SplitterSpec) (domain.Splitter, error) {
			return factory(spec)
		}, nil
	}
}

func tagExtractorAdapter(getTagExtractor func(name string) (domain.TagExtractorFactory, error)) func(domain.TagExtractorConfig) (domain.TagExtractor, error) {
	return func(config domain.TagExtractorConfig) (domain.TagExtractor, error) {
		factory, err := getTagExtractor(config.Provider)
		if err != nil {
			return nil, err
		}
		return factory(config)
	}
}
