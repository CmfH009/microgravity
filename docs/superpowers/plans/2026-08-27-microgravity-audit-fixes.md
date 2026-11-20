# Microgravity Audit Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove confirmed security and reliability hazards from Microgravity while preserving its agent, MCP, model, and developer-workflow capabilities.

**Architecture:** Add centralized workspace and execution-policy boundaries at the tool/executor layer; make external operations context-aware and bounded; harden persistence, plugin, preview, and task lifecycles; replace fragile model-output parsing with validated JSON extraction. Keep existing public commands and package structure where practical.

**Tech Stack:** Go 1.26.4, Cobra, SQLite, Docker SDK, Ollama/Gemini HTTP APIs, MCP JSON-RPC, existing Go test suite.

---

### Task 1: Establish a green baseline and fix LaTeX regression

**Files:**
- Modify: `internal/ui/latex.go`
- Test: `internal/ui/latex_test.go`

- [ ] Write a regression test asserting `\\infty` is converted to `∞` while `\\in` remains `∈`.
- [ ] Run `go test ./internal/ui -run TestRenderLaTeX -count=1` and verify the current failure.
- [ ] Implement deterministic longest-token replacement or a single command tokenizer.
- [ ] Run the focused test and then `go test ./internal/ui`.

### Task 2: Remove embedded credentials and harden config persistence

**Files:**
- Modify: `internal/authutil/keygen.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Test: `internal/authutil/keygen_test.go`

- [ ] Add tests proving OAuth configuration is sourced from environment/config and that missing credentials produce a clear error.
- [ ] Remove the literal OAuth secret from source; use `MG_GOOGLE_CLIENT_ID`, `MG_GOOGLE_CLIENT_SECRET`, and existing local config conventions.
- [ ] Make config JSON writes atomic through a same-directory temporary file and rename.
- [ ] Return malformed JSON errors instead of silently replacing configuration with an empty config.
- [ ] Preserve restrictive `0600` permissions.
- [ ] Run focused config/auth tests.

### Task 3: Add central workspace and command execution policy

**Files:**
- Create: `internal/policy/policy.go`
- Test: `internal/policy/policy_test.go`
- Modify: `internal/executor/executor.go`
- Modify: `internal/tools/builtins.go`
- Modify: `cmd/run.go`
- Modify: `cmd/chat.go`

- [ ] Test path containment for approved roots, traversal, absolute paths, and symlink escape handling.
- [ ] Test command policy defaults, explicit unsafe mode, and sandbox routing.
- [ ] Implement `Policy` with approved roots, `AllowHostCommands`, `AllowExternalPaths`, and `SandboxEnabled`.
- [ ] Enforce the policy before native/MCP tool execution.
- [ ] Require explicit unsafe policy for shell command tools and reject commands outside policy.
- [ ] Route Python/Node execution through the existing Docker sandbox when sandbox mode is active.
- [ ] Pass policy through the runtime builder instead of relying on prompt instructions.

### Task 4: Harden task lifecycle and notifications

**Files:**
- Modify: `internal/tasks/tasks.go`
- Test: `internal/tasks/tasks_test.go`

- [ ] Add tests for final output completeness, cancellation state, stdin closure, notification backpressure, and task cleanup.
- [ ] Use `0600` task logs.
- [ ] Join output collection before synchronous completion returns.
- [ ] Make notification sends context-aware/non-blocking.
- [ ] Make task completion state transitions idempotent.
- [ ] Close stdin and remove completed tasks according to a bounded retention policy.
- [ ] Synchronize channel snapshots and task metadata access.

### Task 5: Fix MCP request cancellation and process failure handling

**Files:**
- Modify: `internal/mcp/client.go`
- Test: `internal/mcp/client_test.go`

- [ ] Add tests for request timeout, context cancellation, closed stdout, malformed response, and concurrent requests.
- [ ] Change request dispatch to accept context and select on response/context/timeout.
- [ ] Fail all pending requests when the read loop exits.
- [ ] Serialize stdin writes and protect command lifecycle state.
- [ ] Add a dedicated bounded HTTP client and response-size limit.
- [ ] Validate MCP URL scheme and preserve caller context.

### Task 6: Secure preview server and plugin manager

**Files:**
- Modify: `internal/preview/server.go`
- Test: `internal/preview/server_test.go`
- Modify: `internal/plugins/manager.go`
- Modify: `internal/plugins/manifest.go`
- Test: `internal/plugins/plugins_test.go`

- [ ] Add tests for origin rejection, reload method restrictions, path traversal, symlink escape, invalid plugin names, and atomic install behavior.
- [ ] Restrict WebSocket origins to configured/local origins.
- [ ] Restrict reload to localhost POST requests and add a process token where needed.
- [ ] Enforce canonical path containment before serving files.
- [ ] Validate plugin names and manifests strictly.
- [ ] Reject symlink escapes, copy atomically, record source/hash metadata, and separate installed/enabled state.

### Task 7: Repair lock, model clients, fallback logic, and output handling

**Files:**
- Modify: `internal/lock/lock.go`
- Test: `internal/lock/lock_test.go`
- Modify: `internal/cloud/client.go`
- Test: `internal/cloud/client_test.go`
- Modify: `internal/ollama/client.go`
- Test: `internal/ollama/client_test.go`

- [ ] Test concurrent lock acquisition and stale lock handling.
- [ ] Replace unconditional lock deletion with real stale-lock detection.
- [ ] Treat nil writers as `io.Discard`.
- [ ] Make retry backoff context-aware and honor `Retry-After`.
- [ ] Avoid model fallback on quota/authentication errors unless configured.
- [ ] Preserve useful error categories and response-size limits.

### Task 8: Make indexing, RAG, and protocol handling durable

**Files:**
- Modify: `internal/indexer/indexer.go`
- Modify: `internal/rag/rag.go`
- Test: existing indexer/RAG tests plus new regression cases
- Modify: `cmd/chat.go`
- Modify: `cmd/run.go`
- Test: `cmd/chat_test.go` or focused parser test file

- [ ] Test stale chunk deletion, deleted-file reconciliation, embedding failure propagation, and manifest save failure.
- [ ] Reconcile document IDs per file before inserting new chunks.
- [ ] Return indexing errors instead of silently continuing.
- [ ] Add bounded query behavior and deterministic score ordering.
- [ ] Replace greedy tool-call regex with fenced JSON extraction and schema/name validation.
- [ ] Ensure machine-readable output keeps diagnostics/UI on stderr.

### Task 9: Remove misleading placeholders and update documentation

**Files:**
- Modify: `README.md`
- Modify: `HANDOVER.md`
- Modify: `ARCHITECTURE.md`
- Modify: `PLAN.md`
- Modify: `cmd/root.go`
- Modify: `internal/tools/builtins.go`

- [ ] Implement actual JSON-schema validation or clearly downgrade the flag description.
- [ ] Make log-file behavior match its documentation or rename/document it accurately.
- [ ] Make `ask_question`, `/dashboard`, `/fix`, and `/ci` behavior honest and testable.
- [ ] Document the execution policy, sandbox limitations, plugin trust model, and required credentials.
- [ ] Remove “100% tests”/“A+” claims until CI verifies them.

### Task 10: Full verification and release gate

**Files:**
- Modify/create CI configuration only if the repository already uses it.

- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `go build ./...`.
- [ ] Run `CGO_ENABLED=1 go test -race ./...` where the environment supports cgo.
- [ ] Run targeted security regression tests.
- [ ] Review the final diff for secrets, ignored errors, and unrelated changes.
