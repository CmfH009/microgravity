# Gemma CLI Architecture Spec

## 1. Core Goal
An open-source, CLI-based agent tool heavily inspired by Antigravity, but entirely driven by the Gemma model family. The CLI intelligently routes requests between cloud models (for complex reasoning) and local on-device models (for fast, simple tasks).

## 2. Tech Stack
- **Language**: Go (Golang) for speed, concurrency, and cross-platform binary compilation.
- **Local Inference Engine**: Ollama REST API integration (leveraging existing local `.ollama` cache).

## 3. Key Components
### A. The LLM Router
A lightweight Go layer that intercepts user prompts. It uses a very tiny local model (or fast heuristic) to classify the complexity of the request.
- **Complex**: Routes to Google Cloud / Vertex AI for SOTA Gemma execution.
- **Simple**: Routes to `localhost:11434` (Ollama) for instant local execution.

### B. Tool Calling (Model Context Protocol)
The CLI implements the **Model Context Protocol (MCP)** standard. 
Instead of hardcoding tools, the CLI dynamically discovers and loads tools (file reading, web searching, system commands) using MCP servers. This allows seamless plug-and-play expandability without recompiling the binary.

### C. Auto-Updater Daemon
A background Go routine (`goroutine`) runs on CLI startup.
It queries the official Ollama library/registry for the `gemma` tag. If a new version (e.g., a newer quantization or instruct tune) is detected, it triggers a background `ollama pull` so the user is always running the best local Gemma variant without manual intervention.

## Next Steps for Development
1. Install Go (`winget install GoLang.Go`).
2. Run `go mod init github.com/purav/microgravity`.
3. Scaffold the Go command-line interface using the `cobra` or `urfave/cli` framework.
