package skills

import (
	"strings"
	"testing"
)

func TestParseSkill(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
		wantBody string
		wantErr  bool
	}{
		{
			name: "valid frontmatter",
			content: `---
name: test-skill
description: A test skill
---
# Content
Some markdown here.`,
			wantName: "test-skill",
			wantDesc: "A test skill",
			wantBody: "# Content\nSome markdown here.",
			wantErr:  false,
		},
		{
			name: "missing frontmatter",
			content: `# Content
No frontmatter here.`,
			wantErr: true,
		},
		{
			name: "invalid yaml",
			content: `---
name: : invalid
---
Content`,
			wantErr: true,
		},
		{
			name: "missing name",
			content: `---
description: No name
---
Content`,
			wantErr: true,
		},
		{
			name: "unclosed frontmatter",
			content: `---
name: unclosed
description: No closing dashes
Content`,
			wantErr: true,
		},
		{
			name: "empty body",
			content: `---
name: empty-body
description: Empty body
---`,
			wantName: "empty-body",
			wantDesc: "Empty body",
			wantBody: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.content)
			skill, err := ParseSkill(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSkill() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if skill.Name != tt.wantName {
				t.Errorf("ParseSkill() Name = %v, want %v", skill.Name, tt.wantName)
			}
			if skill.Description != tt.wantDesc {
				t.Errorf("ParseSkill() Description = %v, want %v", skill.Description, tt.wantDesc)
			}
			if strings.TrimSpace(skill.Body) != strings.TrimSpace(tt.wantBody) {
				t.Errorf("ParseSkill() Body = %q, want %q", skill.Body, tt.wantBody)
			}
		})
	}
}
