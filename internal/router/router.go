package router

import (
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v3/mem"
)

type Complexity string

const (
	ComplexitySimple  Complexity = "simple"
	ComplexityComplex Complexity = "complex"
)

type Router struct{}

func NewRouter() *Router {
	return &Router{}
}

// Classify classifies a prompt to decide if it requires a local or cloud model.
func (r *Router) Classify(prompt string) Complexity {
	prompt = strings.ToLower(strings.TrimSpace(prompt))

	// If prompt is empty, default to simple
	if prompt == "" {
		return ComplexitySimple
	}

	// 0. Check System RAM first. If RAM is over 85%, route to Cloud to avoid crashing local Ollama.
	v, err := mem.VirtualMemory()
	if err == nil {
		if v.UsedPercent > 85.0 {
			fmt.Printf("\n⚠️ High memory usage detected (%.1f%%). Routing to Cloud Model.\n", v.UsedPercent)
			return ComplexityComplex
		}
	}

	// Heuristics for complex tasks:
	// 1. Prompt is long (> 150 characters)
	if len(prompt) > 150 {
		return ComplexityComplex
	}

	// 2. Contains complex intent keywords
	complexKeywords := []string{
		"refactor", "design", "optimize", "architecture",
		"explain", "debug", "benchmark", "implement", "analyze",
		"search", "web", "today", "date", "time", "weather", "latest", "news", "current", "who", "day", "year",
	}

	for _, kw := range complexKeywords {
		if strings.Contains(prompt, kw) {
			return ComplexityComplex
		}
	}

	return ComplexitySimple
}
