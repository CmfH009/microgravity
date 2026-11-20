# Microgravity (`mg`) 🌌

[![CI](https://github.com/CmfH009/microgravity/actions/workflows/ci.yml/badge.svg)](https://github.com/CmfH009/microgravity/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/CmfH009/microgravity)](https://github.com/CmfH009/microgravity/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/CmfH009/microgravity)](https://github.com/CmfH009/microgravity/blob/master/go.mod)
[![License: MIT](https://img.shields.io/github/license/CmfH009/microgravity)](https://github.com/CmfH009/microgravity/blob/master/LICENSE)

**Microgravity** (`mg`) is an autonomous AI agent CLI and execution environment driven by local (Ollama/Gemma) and cloud-routed (Gemini/Gemma) LLM model architectures, providing full parity with the Google Antigravity (`agy`) CLI ecosystem.

> **Status:** Active development. Run `go test -race ./...`, `go vet ./...`, and `go build ./...` locally before release.

---

## ⚡ Key Capabilities & Architecture

- **Full Antigravity (`agy`) Tool Suite Parity**:
  - `run_command` with synchronous wait timeout (`WaitMsBeforeAsync`), asynchronous background task execution, streaming task logs (`.system_generated/tasks/`), and reactive message notifications.
  - `manage_task` (`list`, `kill`, `status`, `send_input`) for inspecting and controlling background processes and piping stdin input.
  - `invoke_subagent` and `define_subagent` for dynamic subagent role definition and parallel multi-subagent array dispatch.
  - `manage_subagents` (`list`, `kill`, `kill_all`) with rich state lifecycles (`running`, `idle`, `waiting_for_input`, `waiting_for_dependents`, `waiting_for_message`, `canceling`, `errored`, `completed`, `killed`).
  - `send_message` for thread-safe inter-agent messaging.
  - `call_mcp_tool` for dynamic lazy and eager MCP tool invocation across connected servers.
  - `search_web` (with query, domain filters, and citation formatting) and `read_url_content` (HTML-to-markdown extraction).
  - `generate_image` (generates UI mockups, diagrams, and layout artifacts stored directly in the brain artifact registry).
  - `schedule` (one-shot duration timers with condition triggers `never`, `any`, `<sender-id>`, and recurring cron schedules).
  - `view_file` (with 1-indexed slicing, 46KB truncation, and `ContentOffset`), `write_to_file` (with `ArtifactMetadata`), `replace_file_content` (with `Instruction`, `Description`, `TargetLintErrorIds`), `grep_search`, `find_by_name`, and `list_dir`.
- **Conversation Transcripts & Brain Architecture (`~/.gemini/antigravity-cli/brain/`)**:
  - Full AGY JSONL specification support for `transcript.jsonl` (compact with `truncated_fields`) and `transcript_full.jsonl` (untruncated full steps).
- **Interactive Slash Commands (`/caveman`, `/goal`, `/schedule`, `/plan`, `/grill-me`, `/teamwork-preview`, `/learn`, `/dashboard`, `/fix`, `/ci`, `/rules`, `/mcp`, `/subagents`)**.

---

## 🚀 CLI Command Reference

| Command | Description |
| :--- | :--- |
| `mg` / `microgravity` | Launch the interactive AI agent session |
| `mg agent` / `agents` | List available built-in and specialized agent roles |
| `mg changelog` | Show changelog and release notes |
| `mg plugin <subcommand>` | Manage plugins (`list`, `import`, `install`, `uninstall`, `enable`, `disable`, `validate`, `link`) |
| `mg mcp <subcommand>` | Manage MCP servers (`add`, `remove`, `list`, `enable`, `disable`, `inspect`, `serve`) |
| `mg mcp serve` | Run mg as a stdio JSON-RPC MCP server exposing all native tools |
| `mg update` | Update CLI to latest release |
| `mg fix [-a 5] [-c]` | Autonomous self-healing test repair & doubt loop |
| `mg ci [-o .github/workflows]` | Generate production GitHub Actions workflows |
| `mg dashboard [-w] [-p 8085]` | Launch Web & Terminal Telemetry Control Center |
| `mg diff [--staged] [--stat] [--file <path>] [--last-session]` | Show styled unified diff of working tree or staged changes |
| `mg swarm [tasks...]` | Launch parallel worker subagent swarm |
| `mg compress [file]` | Compress file / AST token pruning |
| `mg profile [pkg]` | Benchmark and runtime memory profiling |
| `mg graph [-p] [-s] [-a]` | Render ASCII dependency tree |
| `mg preview [-p 3000]` | Start hot-reloading web app preview server |
| `mg skills` | Browse, search, and inspect installed AGY skills |
| `mg audit [--fix]` | Run workspace setup and health audit |
| `mg models` | Inspect active local/cloud model endpoints |
| `mg subagents list` | List active subagent threads |
| `mg sessions [list\|delete\|export\|import]` | Manage saved chat sessions |
| `mg sessions export <name> <file> [--format md\|json]` | Export session history as markdown or JSON |
| `mg install [--dir] [--skip-path]` | Configure environment paths & shell settings |
| `mg undo [n]` | Revert the last n file modifications (write-ahead journal) |

---

## 🚩 Root Flags Reference

| Flag | Description |
| :--- | :--- |
| `--add-dir` | Add a directory to the workspace (repeatable) |
| `--agent` | Select agent persona for the CLI session (`self`, `research`, `code-reviewer`, `security-auditor`, `test-engineer`, `web-performance-auditor`) |
| `-c`, `--continue` | Continue the most recent conversation |
| `--conversation` | Resume a previous conversation by ID |
| `--dangerously-skip-permissions` | Auto-approve all tool permission requests without prompting |
| `--dry-run` | Preview planned tool calls without executing them |
| `--disable-slash-commands` | Disable slash command and skill expansion in print mode |
| `--effort` | Reasoning effort for the session (`low`, `medium`, `high`) |
| `-i`, `--prompt-interactive` | Run an initial prompt interactively and continue the session |
| `--input-format` | Input format for print mode (`text`, `stream-json`) |
| `--output-format` | Output format for print mode (`text`, `json`, `stream-json`) |
| `--json-schema` | Enforce structured output validation against JSON schema |
| `--log-file` | Override CLI log file path |
| `--mode` | Set agent execution mode (`accept-edits`, `plan`, `default`) |
| `--model` | Model for the current CLI session |
| `--new-project` | Create a new project for this session |
| `-p`, `--print` / `--prompt` | Run a single prompt non-interactively and print response |
| `--print-timeout` | Timeout for print mode wait (default 5m0s) |
| `--project` | Project ID or name for the CLI session |
| `--sandbox` | Run in sandbox mode with terminal restrictions |

---

## 📦 Build & Installation

### Install via Go

```bash
go install github.com/CmfH009/microgravity@latest
```

### Local & Cross-Platform Builds

```bash
# Build from source in repository root
go build -ldflags="-s -w" -o mg .

# Cross-compile static binaries (CGO_ENABLED=0)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/mg-linux-amd64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/mg-windows-amd64.exe .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/mg-darwin-arm64 .
```

On Windows PowerShell:

```powershell
$env:CGO_ENABLED="0"; $env:GOOS="windows"; $env:GOARCH="amd64"; go build -ldflags="-s -w" -o dist/mg-windows-amd64.exe .
```

### Verification & Tests

```bash
go vet ./...
go test -race ./...
```

---

## ⚙️ Portable Sidecar Discovery

Python sidecar integrations (such as browser automation and Letta memory tools) locate the Python runtime and sidecar scripts dynamically without hardcoded paths:

| Variable | Purpose | Resolution Order |
| :--- | :--- | :--- |
| `MG_PYTHON` | Python interpreter path or command name | `MG_PYTHON` -> `VIRTUAL_ENV` -> local `.venv` (cwd or executable directory) -> system `PATH` -> `"python"` |
| `MG_SIDECAR_DIR` | Explicit directory containing sidecar scripts | Checked directly first (`<relPath>` / `<fallbackName>`), then `python/` and `scripts/` subdirectories (does not walk parent directories) |
| `MG_HOME` | Base Microgravity installation directory | Checked after `MG_SIDECAR_DIR` (in `python/`, root, and `scripts/`), before falling back to cwd, executable directory, and up to 4 parent directories |
