# Microgravity (`mg`) Architecture

## 1. Overview

Microgravity is a Go-based autonomous AI agent CLI and multi-agent runtime that unifies local inference (Ollama, downloaded `.gguf` models, and OpenAI-compatible HTTP endpoints) with cloud Gemini models under a single tool-calling loop.

## 2. Core Subsystems

### CLI & TUI (`cmd/`, `internal/ui/`)
- **`cmd/`**: Cobra command hierarchy (`root`, `chat`, `tui`, `models`, `memory`, `mcp`, `swarm`, `dashboard`, `audit`, `doctor`, `budget`, `replay`, `sessions`, `undo`, `diff`, etc.) and `BuildEngineSession` wiring in `cmd/session_factory.go`.
- **`internal/ui/`**: Lip Gloss theme engine (`dark`, `light`, `auto`, `tokyo-night`, `catppuccin`, `high-contrast`), reusable bordered panels, braille sparklines, diff approval modals, and bounded collapsible tool cards.

### Turn Loop & Failover Dispatch (`internal/engine/`, `internal/pruner/`)
- **`internal/engine/turn.go`**: Executes the multi-turn agent loop (`ExecuteTurn`), processing `@file` / `@dir` references, pruning conversation history (`internal/pruner`) to preserve Turn 0 intent within model context budgets, streaming LLM responses, executing native or MCP tool calls, running doubt-driven post-edit test verification (`internal/verifier`), and triggering placeholder fallback only when model output emits literal placeholder tokens.
- **Provider Dispatch (`generateWithFailover`)**: Respects `--routing-mode` (`hybrid`, `local-only`, `cloud-only`), OpenAI-compatible endpoints (`--base-url`), and explicit local Ollama/GGUF active models without dialing cloud endpoints unnecessarily.

### Model Routing, Catalog & Hugging Face Hub (`internal/router/`, `internal/models/`, `internal/huggingface/`)
- **`internal/router/`**: Classifies prompt complexity (`Simple` vs. `Complex`) and selects fast/heavy local models or cloud endpoints according to active policy.
- **`internal/models/`**: Scans installed Ollama tags (`/api/tags`), local `.gguf` files in `~/.microgravity/models`, and built-in Gemini/Gemma/Qwen/DeepSeek/Llama/Mistral/Phi catalogs, exposed through an alternate-screen Bubble Tea selector (`/model` / `mg models`).
- **`internal/huggingface/`**: Queries Hugging Face Hub GGUF repositories, picks quantizations that fit system RAM, and performs resumable downloads.

### Tools, Policy, Cache & Safety Guards (`internal/tools/`, `internal/executor/`, `internal/policy/`, `internal/cache/`, `internal/secrets/`, `internal/journal/`)
- **`internal/tools/` & `internal/executor/`**: Registers built-in filesystem, command, web (`search_web`, `read_url_content`), git, scheduler, task, subagent, Letta memory, and browser tools; supports sequential or bounded parallel execution (`MG_MAX_PARALLEL_TOOLS`).
- **`internal/policy/`**: Enforces approved workspace roots (`--add-dir`), `--sandbox` shell restrictions, and tool/path allow/deny rules.
- **`internal/secrets/` & `internal/journal/`**: Scans file writes for high-confidence credentials before persisting and records pre-write snapshots to `~/.microgravity/journal.json` for `mg undo`.
- **`internal/cache/`**: TTL-memoizes idempotent tool executions keyed by tool name and sorted SHA-256 argument digest.

### MCP, Memory & Subagent Swarm (`internal/mcp/`, `internal/memory/`, `internal/rag/`, `internal/subagent/`, `internal/swarm/`)
- **`internal/mcp/`**: Connects to external stdio/HTTP MCP servers and exposes native Microgravity tools over stdio JSON-RPC (`mg mcp serve`).
- **`internal/memory/` & `internal/rag/`**: Implements offline cross-session semantic retrieval in SQLite (`~/.microgravity/memory.db`) using deterministic 256-dimensional FNV-32a feature hashing, gating injection via `ShouldInjectMemory` and `FilterRelevant` so trivial prompts and previously injected headers are filtered out.
- **`internal/subagent/` & `internal/swarm/`**: Manages background subagents with `inherit` or isolated `git worktree` workspaces (`branch`), wired to the internal EventBus (`internal/events/`) and AGY JSONL trajectory logs (`internal/trajectory/`).

### Python Sidecars (`python/`, `internal/sidecar/`)
- **`internal/sidecar/`**: Portably resolves Python interpreters (`MG_PYTHON`, `VIRTUAL_ENV`, `.venv`, `PATH`) and sidecar scripts (`python/browser/mg_tool_browser.py`, `python/letta/mg_memory_letta.py`, `python/eventbus/backpressure_queue.py`) for browser automation, Letta hierarchical memory, and WebSocket EventBus streaming.
