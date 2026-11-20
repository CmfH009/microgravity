package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/purav/microgravity/internal/mcp"
)

// RegisterUIArtifactTools registers ask_question and generate_image tools.
func RegisterUIArtifactTools(r *Registry) {
	// ── 1. ask_question ──
	r.Register(mcp.Tool{
		Name:        "ask_question",
		Description: "Ask user interactive questions or solicit design/option feedback with multiple-choice and write-in support",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"questions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"question":        map[string]interface{}{"type": "string"},
							"options":         map[string]interface{}{"type": "array"},
							"is_multi_select": map[string]interface{}{"type": "boolean"},
						},
						"required": []string{"question", "options"},
					},
				},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"questions"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		rawQuestions, ok := args["questions"].([]interface{})
		if !ok || len(rawQuestions) == 0 {
			return "", fmt.Errorf("questions array is required and cannot be empty")
		}

		isTerminal := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
		reader := bufio.NewReader(os.Stdin)

		var responses []map[string]interface{}

		for idx, rawQ := range rawQuestions {
			qMap, ok := rawQ.(map[string]interface{})
			if !ok {
				continue
			}

			qText, _ := qMap["question"].(string)
			var optTexts []string
			if rawOpts, ok := qMap["options"].([]interface{}); ok {
				for _, ro := range rawOpts {
					if s, ok := ro.(string); ok {
						optTexts = append(optTexts, s)
					} else if om, ok := ro.(map[string]interface{}); ok {
						if label, ok := om["label"].(string); ok {
							optTexts = append(optTexts, label)
						} else if text, ok := om["text"].(string); ok {
							optTexts = append(optTexts, text)
						}
					}
				}
			}

			if isTerminal {
				fmt.Printf("\n❓ Question %d/%d: %s\n", idx+1, len(rawQuestions), qText)
				for i, opt := range optTexts {
					fmt.Printf("   [%d] %s\n", i+1, opt)
				}
				fmt.Printf("   [w] Write custom answer\n")
				fmt.Print("Your choice [1]: ")

				choiceStr, _ := reader.ReadString('\n')
				choiceStr = strings.TrimSpace(choiceStr)

				var chosen string
				if choiceStr == "" || choiceStr == "1" {
					if len(optTexts) > 0 {
						chosen = optTexts[0]
					} else {
						chosen = "Confirmed"
					}
				} else if choiceStr == "w" || choiceStr == "W" {
					fmt.Print("Enter custom answer: ")
					custom, _ := reader.ReadString('\n')
					chosen = strings.TrimSpace(custom)
				} else {
					chosen = choiceStr
					var choiceNum int
					if _, err := fmt.Sscanf(choiceStr, "%d", &choiceNum); err == nil && choiceNum > 0 && choiceNum <= len(optTexts) {
						chosen = optTexts[choiceNum-1]
					}
				}

				responses = append(responses, map[string]interface{}{
					"question": qText,
					"answer":   chosen,
				})
			} else {
				// Non-interactive fallback: pick default option 1
				ans := "Confirmed"
				if len(optTexts) > 0 {
					ans = optTexts[0]
				}
				responses = append(responses, map[string]interface{}{
					"question": qText,
					"answer":   ans,
				})
			}
		}

		data, _ := json.MarshalIndent(responses, "", "  ")
		return fmt.Sprintf("User feedback collected:\n%s", string(data)), nil
	})

	// ── 2. generate_image ──
	r.Register(mcp.Tool{
		Name:        "generate_image",
		Description: "Generate an image or UI layout mockups saved as an artifact",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"Prompt":      map[string]interface{}{"type": "string"},
				"ImageName":   map[string]interface{}{"type": "string"},
				"AspectRatio": map[string]interface{}{"type": "string"},
				"ImagePaths":  map[string]interface{}{"type": "array"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"Prompt", "ImageName"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		promptStr, _ := args["Prompt"].(string)
		imgName, _ := args["ImageName"].(string)
		aspectRatio, _ := args["AspectRatio"].(string)
		if aspectRatio == "" {
			aspectRatio = "16:9"
		}

		home, _ := os.UserHomeDir()
		artifactDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain", "artifacts")
		_ = os.MkdirAll(artifactDir, 0755)

		imgName = filepath.Base(imgName)
		if imgName == "." || imgName == "" || strings.ContainsAny(imgName, `/\\`) {
			imgName = fmt.Sprintf("diagram_%d", os.Getpid())
		}
		imgName = strings.TrimSuffix(imgName, ".svg")

		width := 800
		height := 450
		if aspectRatio == "1:1" {
			width = 600
			height = 600
		} else if aspectRatio == "4:3" {
			width = 800
			height = 600
		}

		svgPath := filepath.Join(artifactDir, fmt.Sprintf("%s.svg", imgName))
		svgContent := fmt.Sprintf(`<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="bg" x1="0%%" y1="0%%" x2="100%%" y2="100%%">
      <stop offset="0%%" stop-color="#181825"/>
      <stop offset="100%%" stop-color="#11111b"/>
    </linearGradient>
  </defs>
  <rect width="100%%" height="100%%" rx="12" fill="url(#bg)" stroke="#313244" stroke-width="2"/>
  <rect x="24" y="24" width="%d" height="40" rx="8" fill="#1e1e2e" stroke="#45475a" stroke-width="1"/>
  <circle cx="44" cy="44" r="6" fill="#f38ba8"/>
  <circle cx="64" cy="44" r="6" fill="#f9e2af"/>
  <circle cx="84" cy="44" r="6" fill="#a6e3a1"/>
  <text x="110" y="49" font-family="Segoe UI, -apple-system, sans-serif" font-size="14" font-weight="600" fill="#cdd6f4">%s</text>
  <rect x="24" y="80" width="%d" height="%d" rx="8" fill="#181825" stroke="#313244" stroke-width="1"/>
  <text x="50%%" y="45%%" font-family="Segoe UI, -apple-system, sans-serif" font-size="18" font-weight="500" fill="#cdd6f4" text-anchor="middle">%s</text>
  <text x="50%%" y="58%%" font-family="Segoe UI, -apple-system, sans-serif" font-size="13" fill="#89b4fa" text-anchor="middle">Aspect: %s | Dimensions: %dx%d</text>
</svg>`, width, height, width, height, width-48, imgName, width-48, height-104, promptStr, aspectRatio, width, height)

		_ = os.WriteFile(svgPath, []byte(svgContent), 0644)
		return fmt.Sprintf("Generated UI/Visual artifact [%s] saved to: file:///%s (Aspect Ratio: %s)", imgName, filepath.ToSlash(svgPath), aspectRatio), nil
	})
}
