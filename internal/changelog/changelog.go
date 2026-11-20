package changelog

import (
	"fmt"
	"strings"
)

// ReleaseNote represents release notes for a specific version.
type ReleaseNote struct {
	Version string   `json:"version"`
	Date    string   `json:"date,omitempty"`
	Notes   []string `json:"notes"`
}

// GetChangelog returns structured release notes for Microgravity.
func GetChangelog() []ReleaseNote {
	return []ReleaseNote{
		{
			Version: "0.2.2",
			Date:    "2026-11-19",
			Notes: []string{
				"Expanded model catalog to 30+ local and cloud models (Gemma 4/3/2, Qwen 2.5/3, DeepSeek-R1/Coder-V2, Llama 3.2/3.3, Mistral/Devstral, Phi-4, GPT-OSS) plus Hugging Face Hub GGUF search and RAM-aware auto-downloader (`mg models search`, `hf-download`, `auto`).",
				"Overhauled `/model`, `/skills`, and `/sessions` TUI pickers with alternate-screen buffer (`tea.WithAltScreen()`), bounded viewport pagination, clean status coloring, and non-overlapping ANSI formatting.",
				"Added bounded tool-card line truncation, rounded durations, and clean HTML snippet unescaping for `search_web`.",
				"Hardened cross-session semantic memory filtering (`ShouldInjectMemory`, `FilterRelevant`) and fixed `mg memory --clear` on Windows.",
				"Fixed local active-model direct Ollama routing and cold-load `ResponseHeaderTimeout` (3m).",
			},
		},
		{
			Version: "1.1.18",
			Date:    "2026-08-22",
			Notes: []string{
				"Added support for passing project name or ID to `--project` with centralized `projects.json` cache resolution.",
				"Added typo-tolerant fuzzy lookup for `@` file path completions.",
				"Added multimodal media attachment pipeline supporting audio (`wav`, `mp3`, `m4a`, `aac`, `flac`, `opus`, `ogg`), video (`mp4`, `webm`, `ogv`), and images.",
				"Added `disable-slash-command: true` frontmatter support in `SKILL.md`.",
				"Added rich terminal Alert Callout rendering (`[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`) and LaTeX math rendering.",
			},
		},
		{
			Version: "1.1.16",
			Date:    "2026-08-20",
			Notes: []string{
				"Added complete MCP server subcommands (`add`, `remove`, `list`, `enable`, `disable`) supporting HTTP/SSE endpoints, `--header`, `--env`, and `--type`.",
				"Added live reasoning effort controls with `/effort` in chat and `--effort` flag on CLI root.",
				"Preserved unparsed fields in `mcp_config.json` across configuration updates.",
			},
		},
		{
			Version: "1.1.12",
			Date:    "2026-08-18",
			Notes: []string{
				"Added non-interactive output for read-only slash commands in print mode with `--output-format json` and `stream-json`.",
				"Added full AGY agent persona catalog (`mg agent` / `agents`): self, research, code-reviewer, security-auditor, test-engineer, web-performance-auditor.",
				"Added lifecycle hooks engine (`hooks.json`) for pre/post tool calls and agent turn boundaries.",
			},
		},
		{
			Version: "1.1.0",
			Date:    "2026-08-10",
			Notes: []string{
				"Added full AGY CLI flag parity: `--add-dir`, `--agent`, `-c`/`--continue`, `--conversation`, `--dangerously-skip-permissions`, `--disable-slash-commands`, `--mode`, `--sandbox`.",
				"Added plugin management suite (`mg plugin list|import|install|uninstall|enable|disable|validate|link`).",
				"Added structured JSON schema validation (`--json-schema`).",
			},
		},
		{
			Version: "1.0.4",
			Date:    "2026-08-05",
			Notes: []string{
				"Integrated real-time AST Context Compressor (`mg compress`) yielding up to 60% token savings.",
				"Added Web & Terminal Telemetry Control Center (`mg dashboard`) with WebSocket streaming.",
				"Implemented multi-line paste buffer (`mg paste` / `/paste`).",
			},
		},
		{
			Version: "1.0.0",
			Date:    "2026-08-01",
			Notes: []string{
				"Initial release of Microgravity CLI & multi-agent runtime.",
				"Local (Ollama/Gemma) and cloud (Gemini) routing engine.",
				"Autonomous test self-healing loop (`mg fix`).",
			},
		},
	}
}

// Render returns formatted text representation of the changelog.
func Render() string {
	var sb strings.Builder
	sb.WriteString("📜 MICROGRAVITY RELEASE NOTES & CHANGELOG\n\n")
	for _, r := range GetChangelog() {
		sb.WriteString(fmt.Sprintf("%s (%s):\n", r.Version, r.Date))
		for _, note := range r.Notes {
			sb.WriteString(fmt.Sprintf("  • %s\n", note))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
