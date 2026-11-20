package skills

import (
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		skill      *Skill
		wantIssues int
	}{
		{
			name: "valid prompt skill",
			skill: &Skill{
				Name:        "code-review",
				Description: "Reviews code",
				Body:        "# Guide\nReview the code.",
			},
			wantIssues: 0,
		},
		{
			name: "valid tool skill",
			skill: &Skill{
				Name:        "git-status",
				Description: "Show git status",
				Type:        "tool",
				Command:     "git status",
				Body:        "Runs git status.",
			},
			wantIssues: 0,
		},
		{
			name: "missing name",
			skill: &Skill{
				Description: "A skill without a name",
				Body:        "Some body.",
			},
			wantIssues: 1,
		},
		{
			name: "missing description",
			skill: &Skill{
				Name: "no-desc",
				Body: "Some body.",
			},
			wantIssues: 1,
		},
		{
			name: "empty body",
			skill: &Skill{
				Name:        "empty-body",
				Description: "Has empty body",
				Body:        "   ",
			},
			wantIssues: 1,
		},
		{
			name: "tool without command",
			skill: &Skill{
				Name:        "broken-tool",
				Description: "Tool with no command",
				Type:        "tool",
				Body:        "A broken tool.",
			},
			wantIssues: 1,
		},
		{
			name: "unknown type",
			skill: &Skill{
				Name:        "weird-type",
				Description: "Has unknown type",
				Type:        "banana",
				Body:        "Some body.",
			},
			wantIssues: 1,
		},
		{
			name: "multiple issues",
			skill: &Skill{
				Type: "tool",
			},
			wantIssues: 4, // missing name, missing description, empty body, tool without command
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := Validate(tt.skill)
			if len(issues) != tt.wantIssues {
				t.Errorf("Validate() returned %d issues, want %d:\n%v", len(issues), tt.wantIssues, issues)
			}
		})
	}
}

func TestFormatSkillSummary(t *testing.T) {
	s := &Skill{
		Name:        "code-review",
		Description: "Reviews code for quality",
		Type:        "prompt",
	}
	summary := FormatSkillSummary(s)
	if summary == "" {
		t.Error("FormatSkillSummary() returned empty string")
	}
	// Should contain the skill name and type
	if !contains(summary, "code-review") || !contains(summary, "prompt") {
		t.Errorf("FormatSkillSummary() = %q, missing name or type", summary)
	}
}

func TestFormatSkillSummaryDefaultType(t *testing.T) {
	s := &Skill{
		Name:        "my-skill",
		Description: "A skill with no type set",
	}
	summary := FormatSkillSummary(s)
	if !contains(summary, "prompt") {
		t.Errorf("FormatSkillSummary() should default to 'prompt', got %q", summary)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
