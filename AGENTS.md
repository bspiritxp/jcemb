# AGENTS.md — jcemb

> Compact guidance for OpenCode sessions working on this Go CLI vector-search tool.

## What this repo is

A local-first Go CLI (`jcemb`) that embeds Markdown documents into a local vector store and queries them with semantic search.

- Only `.md` files are supported.
- Default provider: `ollama`, default model: `bge-m3`.
- Output is stored under `<root>/.vectordb/` (JSON index + records file, not a real LanceDB server).
- Tags come **only** from YAML front matter.

## Build & run

```bash
# Compile binary
go build -o jcemb .

# Embed a directory recursively
./jcemb embed /path/to/docs -r

# Query globally across indexed collections
./jcemb query "search text" -l 10

# JSON output
./jcemb query "search text" --path /path/to/docs --json
```

## Test

```bash
# Unit tests only — no external services required
go test ./...

# With coverage
go test -cover ./...

# Integration tests (require external Ollama + INTEGRATION=1)
go test -tags=integration ./...
```

Integration tests are gated by both **build tag** (`//go:build integration`) and **env var** (`INTEGRATION=1`). See `internal/testkit/integration_gate_test.go`.

## Architecture

| Package | Responsibility |
|---|---|
| `main.go` | Minimal entrypoint; only calls `cmd.Execute()` |
| `cmd/` | Cobra command layer: flag parsing, thin dispatch. No business logic. |
| `internal/app/` | Service orchestration (`embed`, `query`) |
| `internal/domain/` | Core contracts: `Document`, `Chunk`, `VectorStore`, `Embedder`, etc. |
| `internal/registry/` | Self-registration factories for provider / splitter / vector store |
| `internal/provider/ollama/` | Default embedding provider (HTTP to local Ollama) |
| `internal/splitter/markdown/` | Markdown structural splitter (headings → chunks) |
| `internal/storage/lancedb/` | Local vector store adapter (JSON file, not real LanceDB) |
| `internal/index/` | Versioned atomic JSON index (`config.json` + `index.json`) |
| `internal/fs/` | File discovery; skips `.vectordb`, `.git`, `node_modules` |
| `internal/metadata/` | YAML front matter extraction |
| `internal/output/` | Text / JSON query result rendering |
| `internal/testkit/` | Fake store, integration gate, test helpers |

## Registry & extension

New providers, splitters, and vector stores are added via `init()` self-registration:

```go
import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
    registry.MustRegisterProvider("myprovider", factory)
}
```

- Duplicate registration **panics**.
- Tests can reset with `registry.ResetProviders()`, `registry.ResetSplitters()`, `registry.ResetVectorStores()`.

## Key conventions

- **Incrementality**: embed skips unchanged files by comparing `file_hash + recipe_hash`. Use `--force` to rebuild all.
- **Reconcile**: deleted/renamed files are cleaned from both index and vector store on the next embed.
- **Tag filter**: `--tags a,b` means **AND** semantics (result must contain both tags).
- **Path filter**: `query --path` accepts a file or directory and filters by the relative prefix; directory paths include descendants. Omit `--path` for global search across all indexed collections.
- **Chunk ID stability**: derived from `rel_path + recipe_hash + chunk_index (+ section fingerprint)` so re-embeds are deterministic.
- **Concurrency flag typo**: the CLI flag is `--concurccy` (matches original spec), but internal fields use `concurrency`.

## Query denoise

- Query pipeline: `store window → sort → threshold → unique → MMR → final limit`.
- Defaults: `searchWindow=max(20, 5*limit)`, `thresholdAlpha=0.85`, `thresholdDelta=0.10`, `mmrLambda=0.5`.

| Flag | Meaning |
|---|---|
| `--threshold-alpha` | Relative-to-top1 cutoff; negative disables threshold-alpha rule |
| `--threshold-delta` | Absolute gap-from-top1 cutoff; negative disables threshold-delta rule |
| `--mmr-lambda` | MMR weight; `1.0` disables MMR and preserves score order |
| `--search-window` | Retrieval candidate window; `0` means auto window |

- `--unique` deduplicates by file `rel_path` after thresholding and before MMR.
- `--json` receives the same final result list as text output; these stages do not change the JSON schema, and previews are still truncated.

## External runtime dependencies

- **Ollama** must be running locally at `http://localhost:11434` (default).
- **bge-m3** model must be available (`ollama pull bge-m3`).
- Default vector dimension for bge-m3 is **1024**.

## Files an agent should know

- `go.mod` — Go 1.26.2, uses `cobra`, `testify`, `gomarkdown`, `yaml.v3`.
- `internal/config/defaults.go` — Default config values (Ollama URL, batch size, timeout).
- `.sisyphus/plans/go-cli-vector-tool.md` — Full original implementation plan.

## Gotchas

- `query` does **not** accept `--provider` or `--model` overrides. It reads those from `.vectordb/config.json`.
- `lancedb` here is a **local JSON-file adapter**, not the real LanceDB server/SDK.
- Do not put business logic in `cmd/` or `main.go`; keep commands thin.
- Scanner skips `.vectordb`, `.git`, and `node_modules` automatically.

## Extending

Providers, splitters, and vector stores are registered through package
initializers:

```go
import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
    registry.MustRegisterProvider("myprovider", factory)
}
```

Duplicate registrations panic by design. Tests can reset registries with the
corresponding reset helpers in `internal/registry`.
