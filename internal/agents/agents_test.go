package agents

import (
	"testing"
)

func TestBuiltinAgents(t *testing.T) {
	agents := BuiltinAgents()
	if len(agents) < 6 {
		t.Fatalf("expected at least 6 builtin agents, got %d", len(agents))
	}

	expected := []string{"self", "research", "code-reviewer", "security-auditor", "test-engineer", "web-performance-auditor"}
	for _, exp := range expected {
		a, err := GetAgentByName(exp)
		if err != nil {
			t.Errorf("GetAgentByName(%q) returned unexpected error: %v", exp, err)
		}
		if a == nil || a.Name != exp {
			t.Errorf("expected agent name %q, got %+v", exp, a)
		}
	}
}

func TestGetAgentByNameNotFound(t *testing.T) {
	_, err := GetAgentByName("non-existent-agent")
	if err == nil {
		t.Errorf("expected error for non-existent agent, got nil")
	}
}
