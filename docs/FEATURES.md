# Microgravity Feature Batch — October 2026

Working notes for the feature push. Keep clear of `docs/FUTURE_ROADMAP.md`
(approved Phase 4–6 infrastructure roadmap — do not duplicate it here).

## Implemented (verified: `go build ./...`, `go vet ./...`, `go test ./...` all clean)

### 1. Daily token & cost budget guardrails
- Package `internal/budget`: per-calendar-day JSON store (`~/.microgravity/budget.json`),
  atomic writes, date rollover, warn-once, price tiers per model family.
- Enforced in `cloud.GenerateWithTools`: `budget.Check()` before every request
  (fails fast wrapping `budget.ErrExceeded`), `budget.Record()` after with **real**
  token counts parsed from Gemini `usageMetadata` (falls back to ~4 chars/token estimate).
- `mg budget` command: text panel with meters, `--json`, `--reset`.
- `agentui` dashboard telemetry now reads real budget data (removed the old fake
  34100/14100 seed numbers); `RecordTokenUsage` delegates to the budget store.
- Env: `MG_BUDGET_MAX_TOKENS` (default 3,000,000), `MG_BUDGET_MAX_COST_USD` (10.00),
  `MG_BUDGET_WARN_PCT` (80), `MG_BUDGET_DISABLE`, `MG_BUDGET_FILE` (tests).

### 2. Secret-scanning write guard
- Package `internal/secrets`: high-confidence credential formats (AWS AKIA/ASIA,
  GitHub tokens/PATs, OpenAI sk-, Google AIza, Slack xox, Stripe *_live_, JWTs,
  PEM private keys) + assignment patterns (`api_key = "..."` etc.).
- Placeholder/low-entropy/prose filtering prevents scaffold false positives.
- Hooked into `write_to_file` (full content) and `replace_file_content`
  (only the newly inserted text — pre-existing secrets stay editable).
- Overrides: per-call `AllowSecrets: true`, process-wide `MG_ALLOW_SECRET_WRITE=1`.
- Findings are masked (prefix + bullets + suffix) so errors never re-leak secrets.

### 3. Transient-error retry with backoff
- `cloud.executeRequestWithRetry` now retries 429/500/502/503/504 and network
  errors (3 attempts), honors `Retry-After` (capped 8s), exponential backoff
  with +25% jitter, respects context cancellation.
- After in-place retries, a persistent 429 still falls through to the existing
  model-endpoint fallback chain (`gemini-3.8-flash` → … ).

### 4. `mg doctor`
- Diagnoses: binary, config store, auth (presence only — never prints credential
  material), active model, budget state, MCP servers, Ollama reachability, git,
  state-dir writability. `--json` output; non-zero exit on any FAIL.

### Cockpit / Battlestation integration
- Command Launcher allowlist (`server.py` `safe_subcommands`) now includes
  `budget`, `ci`, `doctor` — add new read-only subcommands there when introduced.
- Launcher buttons: 💰 budget, 🩺 doctor (dispatch via `/api/microgravity/exec`).
- Test suite aligned with the redesigned battlestation: 297 passed, 15 skipped
  (skips = features removed by directive, each with an explicit reason).

### 5. MCP Server Mode (`mg mcp serve`)
- Package `cmd/mcp_serve.go`: stdio JSON-RPC 2.0 MCP server exposing all native
  tools (view_file, write_to_file, replace_file_content, grep_search, find_by_name,
  list_dir, run_command, manage_task, invoke_subagent, schedule, search_web,
  read_url_content, Letta memory, browser tools, and 30+ more).
- Handles `initialize`, `tools/list`, `tools/call` over stdin/stdout.
- Builds the same registry as `BuildEngineSession` (builtins + web tools +
  MCP resource tools), so MCP clients see identical tool schemas to the CLI agent.
- Smoke-verified: `initialize` returns protocolVersion/capabilities/serverInfo;
  `tools/list` returns 34 tools; `tools/call list_dir` returns directory listing;
  unknown tool returns JSON-RPC `-32603` error.

### 6. Tool Result Cache (`internal/cache`)
- Package `internal/cache`: TTL-based memoization of tool results, keyed by
  tool name + deterministic argument hash (sorted keys, SHA-256).
- Wired into `tools.Registry.Execute` — every native tool call checks the
  cache before executing the handler and stores successful results afterward.
- Env: `MG_CACHE_ENABLED` (default 1), `MG_CACHE_TTL` (default 5m0s).
- Repeated `list_dir`, `view_file`, `grep_search`, `read_url_content`, etc.
  calls return from cache without re-executing or burning model budget.

### 7. Fine-grained Tool Policy (`internal/policy`)
- Extended `Policy` with tool-level allow/deny rules (`WithToolRules`) and path
  deny patterns (`CheckPathDenied`).
- `CheckTool(toolName)` is enforced in `tools.Registry.Execute` before every
  native tool call — denied tools return an error without executing.
- `CheckPathDenied(path)` is enforced in `Policy.ResolvePath` — file tools
  (`view_file`, `write_to_file`, `replace_file_content`, `grep_search`,
  `find_by_name`, `list_dir`) cannot touch denied paths.
- Configured via env: `MG_POLICY_DENY_TOOLS`, `MG_POLICY_ALLOW_TOOLS`,
  `MG_POLICY_DENY_PATHS` (all comma-separated). Deny list takes precedence over
  allow list.

### 8. Session Replay (`mg replay`)
- Command `cmd/replay.go`: `mg replay <session-id>` renders a past agent
  session step-by-step from its AGY JSONL transcript
  (`transcript_full.jsonl`, with fallback to `transcript.jsonl`).
- `mg replay --list` lists available sessions under the brain directory.
- `mg replay --interactive <session-id>` step-through mode (Enter to advance,
  q to quit).
- Each step shows index, source, type, status, timestamp, content, tool calls,
  thinking, and truncated fields.

### 9. Parallel Tool Execution (`MG_MAX_PARALLEL_TOOLS`)
- `internal/executor/executor.go`: `ExecuteToolCalls` runs independent tool
  calls concurrently when `MG_MAX_PARALLEL_TOOLS > 1` (default 1 = sequential,
  backward compatible). A semaphore caps concurrency; TUI rendering and plan
  updates are serialized. Results are aggregated in original call order.
- `SessionOptions.MaxParallel` + `Executor.SetMaxParallel()` for programmatic
  control.
- Useful for research-heavy turns where the model issues multiple independent
  calls (e.g. `grep_search` + `list_dir` + `read_url_content`).

### 10. Write-Ahead Journal & `mg undo` (`internal/journal`)
- Package `internal/journal`: write-ahead JSON log of file modifications made
  by `write_to_file` and `replace_file_content`. Each entry records op, target
  path, original content snapshot, new content, and timestamp.
- `write_to_file` and `replace_file_content` record before writing (journaling
  failure never blocks the write). Disabled via `MG_JOURNAL_DISABLE=1`.
- `mg undo [n]` reverts the last n modifications by restoring original content
  (or removing newly-created files). `mg undo --list` / `--clear` supported.
- Persisted atomically to `~/.microgravity/journal.json`.

### 11. `mg diff` — styled unified diff (`internal/git`)
- Extended `internal/git/git.go` with `GitDiffStat` (diffstat summary) and
  `GitDiffFile` (single-file diff), refactoring `GitDiff` to share a common
  `runGitDiff` helper.
- `cmd/diff.go`: `mg diff [--staged] [--stat] [--file <path>] [--last-session]`
  renders a styled unified diff of working tree or staged changes.
- `--last-session` diffs the working tree against the most recent mg
  checkpoint (requires a git repo with prior checkpoints).

### 12. `--dry-run` — preview tool calls without executing
- Root flag `--dry-run` (wired through `SessionOptions.DryRun` →
  `Registry.SetDryRun`). When enabled, `tools.Registry.Execute` prints
  `[DRY-RUN] Would execute tool <name> <args>` instead of invoking the handler,
  letting operators preview the planned tool-call sequence before an agent
  turn runs. Policy and cache checks are still evaluated; only the handler
  body is skipped.

### 13. Session export — `mg sessions export --format md|json`
- `cmd/sessions.go`: `sessions export <name> <file> [--format md|json]`
  exports a saved session history. Markdown format wraps content with a
  session header and code block; JSON format emits a structured object with
  session name, export timestamp, format, and content.
- Default format is `md`.

### 14. JSON structured logging — `MG_LOG_FORMAT=json`
- Package `internal/logger`: structured logger with Debug/Info/Warn/Error
  levels. When `MG_LOG_FORMAT=json`, emits JSON objects with `level`, `msg`,
  `ts` fields; otherwise plain text with level prefix.
- Wired into budget warnings (`internal/budget/budget.go`). More call sites
  can be migrated to `logger.Info/Warn/Error` as needed.

### 15. Local-model budget parity (`internal/ollama`)
- `internal/ollama/client.go`: `GenerateWithTools` now calls
  `budget.Record()` after every Ollama model call, using a ~4 chars/token
  estimate (Ollama doesn't report token counts in its streaming response).
  This brings local-model token/cost accounting to parity with the cloud
  path, which was already recording usage via Gemini `usageMetadata`.

### 16. Visual system upgrade (`internal/ui`)
- Theme system: DarkTheme/LightTheme with named color roles (primary, success,
  accent, brand, highlight, separator, system, error, text, text_muted,
  background, surface, border). SetTheme() switches at runtime.
- Panel component: Reusable bordered card with title, PanelGrid for horizontal
  / vertical arrangement, SectionHeader, FormatValue, FormatStat helpers.
- Sparkline: Braille-pattern mini charts for telemetry trends
  (SparklineStat labeled variant).
- Spinner: Animated braille spinner with Start/Stop/Spin/Pulse.
- Table: Aligned table rendering with per-column alignment, FormatKeyValue.
- Dashboard panels rebuilt on Panel component with theme-aware colors.

## Planned next (re-derived backlog — the original 16-item list was lost to compaction)
- `mg diff` — styled unified diff of working tree or last-session changes.
- `mg undo` — write-ahead journal for `write_to_file`/`replace_file_content`
  with `mg undo [n]` (pairs naturally with the secret guard).
- `--dry-run` — print the planned tool-call sequence without executing.
- Session export — `mg sessions export --format md|json`.
- JSON structured logging — `MG_LOG_FORMAT=json`.
- `--max-parallel-tools` — cap concurrent tool execution in the agent loop.
- Local-model parity: budget recording for the Ollama path (cloud only today).

## Verification commands
- Go: `go build ./... && go vet ./... && go test ./... -count=1` in repo root.
- Cockpit: `python -m pytest` (A:\system\cockpit), battlestation probe
  `_diag_battlestation.py` (Playwright, 48 checks, 0 page errors).
- Live wiring: `mg budget`, `mg doctor`, then through the cockpit:
  `POST /api/microgravity/exec {"cmd_args":["doctor"]}`.
