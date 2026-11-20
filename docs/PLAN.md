# Microgravity (`mg`) Master Execution Plan

## 1. Project Overview
An autonomous AI agent CLI driven by local and cloud-routed Gemma models, implementing Model Context Protocol (MCP) for tool discovery.

## 2. Phased Execution Plan

### Phase 1: Foundation & CLI Scaffolding (Completed)
- [x] Scaffold Go CLI using `cobra` framework
- [x] Set up module (`github.com/purav/microgravity`)
- [x] Build and basic routing (`mg` / `microgravity`)

### Phase 2: Core Components Implementation
- [x] **LLM Router**
  - Implement heuristic/small-model routing for complex vs. simple requests.
  - Integrate with Google Cloud/Vertex AI for SOTA Gemma.
  - Integrate with `localhost:11434` (Ollama) for local Gemma.
- [x] **MCP (Model Context Protocol) Integration**
  - Implement MCP client for dynamic tool discovery and execution.
  - Support file reading, web searching, system commands via MCP servers.
- [x] **Auto-Updater Daemon**
  - Create goroutine to query Ollama registry for `gemma` tag updates.
  - Trigger background `ollama pull` on new versions.

### Phase 3: Advanced Features
- [x] **Interactive Plan Engine (`internal/plan`)**
  - Implement markdown plan parser.
  - Implement LipGloss visual task card renderer.
- [x] **Doubt-Driven Verification Loop (`internal/verifier`)**
  - Detect automated test runners (`go test`, `npm test`).
  - Implement self-correction context feedback loops.
- [x] **Subagent TUI & Management (`mg subagents`)**
  - Implement thread-safe background subagent tracking.
  - Build visual terminal manager for subagents.
- [x] **Global & Project Directives (`internal/rules`)**
  - Build multi-tiered system prompt injection engine.
- [x] **Token Budgeting & Context Pruning (`internal/pruner`)**
  - Implement ultra-fast token estimation.
  - Build dynamic middle-turn pruning logic.
- [x] **Workspace Auditor (`mg audit`)**
  - Implement setup auditor with A-F grading.
  - Develop auto-fix patching capabilities.

### Phase 4: Testing, Audit, & Release
- [x] Empirical Audit (Phase 1-4) for sandboxing, state persistence, I/O pipeline, and autonomous error recovery.
- [x] Write integration and chaos tests.
- [x] Finalize documentation and deployment binaries.
