package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/tools"
)

// LoadDir scans a single directory for skills.
func LoadDir(dir string) ([]*Skill, error) {
	var loadedSkills []*Skill

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer file.Close()

			skill, err := ParseSkill(file)
			if err == nil && skill != nil {
				loadedSkills = append(loadedSkills, skill)
			}
		}
		return nil
	})

	return loadedSkills, err
}

// LoadAll scans the target directory as well as global AGY skill & plugin directories.
func LoadAll(dir string) ([]*Skill, error) {
	loadedSkills, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, s := range loadedSkills {
		if s != nil && s.Name != "" {
			seen[s.Name] = true
		}
	}

	var globalDirs []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates := []string{
			filepath.Join(home, ".gemini", "antigravity-cli", "skills"),
			filepath.Join(home, ".gemini", "antigravity-cli", "builtin", "skills"),
			filepath.Join(home, ".gemini", "config", "skills"),
			filepath.Join(home, ".microgravity", "skills"),
		}
		for _, c := range candidates {
			if _, errStat := os.Stat(c); errStat == nil {
				globalDirs = append(globalDirs, c)
			}
		}

		// Plugin skills discovery
		pluginBases := []string{
			filepath.Join(home, ".gemini", "config", "plugins"),
			filepath.Join(home, ".microgravity", "plugins"),
		}
		for _, pb := range pluginBases {
			if entries, err := os.ReadDir(pb); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						pSkills := filepath.Join(pb, e.Name(), "skills")
						if _, errStat := os.Stat(pSkills); errStat == nil {
							globalDirs = append(globalDirs, pSkills)
						}
					}
				}
			}
		}
	}

	for _, scanDir := range globalDirs {
		extra, errExtra := LoadDir(scanDir)
		if errExtra == nil {
			for _, s := range extra {
				if s != nil && s.Name != "" && !seen[s.Name] {
					seen[s.Name] = true
					loadedSkills = append(loadedSkills, s)
				}
			}
		}
	}

	return loadedSkills, nil
}

func RegisterTools(registry *tools.Registry, loadedSkills []*Skill) {
	for _, s := range loadedSkills {
		if s.Type == "tool" && s.Command != "" {
			skill := s
			schema := mcp.Tool{
				Name:        skill.Name,
				Description: skill.Description,
				InputSchema: mcp.Schema{
					Type: "object",
					Properties: map[string]interface{}{
						"args": map[string]interface{}{
							"type":        "string",
							"description": "Arguments for the tool",
						},
					},
				},
			}
			registry.Register(schema, func(ctx context.Context, args map[string]interface{}) (string, error) {
				cmdStr := skill.Command
				if argStr, ok := args["args"].(string); ok && argStr != "" {
					cmdStr += " " + argStr
				}
				var c *exec.Cmd
				if os.PathSeparator == '\\' {
					c = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
				} else {
					c = exec.CommandContext(ctx, "sh", "-c", cmdStr)
				}
				out, err := c.CombinedOutput()
				if err != nil {
					return string(out) + "\nError: " + err.Error(), nil
				}
				return string(out), nil
			})
		}
	}
}
