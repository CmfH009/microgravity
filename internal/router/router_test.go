package router

import "testing"

func TestRouter_Classify(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected Complexity
	}{
		{"Simple prompt", "what is 2 + 2?", ComplexitySimple},
		{"Short hello", "hi", ComplexitySimple},
		{"Keyword refactor", "please refactor this code block", ComplexityComplex},
		{"Keyword design", "design a software architecture for a chat server", ComplexityComplex},
		{"Long prompt", "can you tell me a very long story about how the world works, explaining all details, and also providing some code snippet representing standard math equations?", ComplexityComplex},
	}

	r := NewRouter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Classify(tt.prompt)
			if got != tt.expected {
				t.Errorf("Classify(%q) = %v; expected %v", tt.prompt, got, tt.expected)
			}
		})
	}
}
