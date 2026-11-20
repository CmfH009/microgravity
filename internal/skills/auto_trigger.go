package skills

import (
	"fmt"
	"strings"

	"github.com/purav/microgravity/internal/ui"
)

// AutoRecommend inspects prompt text and returns top recommended skill matches.
func AutoRecommend(prompt string, loaded []*Skill) []*Skill {
	if prompt == "" || len(loaded) == 0 {
		return nil
	}

	pLower := strings.ToLower(prompt)
	var matches []*Skill

	for _, s := range loaded {
		sLower := strings.ToLower(s.Name + " " + s.Description)

		// Check key triggers
		if strings.Contains(pLower, "bug") || strings.Contains(pLower, "fail") || strings.Contains(pLower, "error") {
			if strings.Contains(sLower, "debug") || strings.Contains(sLower, "error") {
				matches = append(matches, s)
				continue
			}
		}
		if strings.Contains(pLower, "test") || strings.Contains(pLower, "tdd") {
			if strings.Contains(sLower, "test") || strings.Contains(sLower, "qa") {
				matches = append(matches, s)
				continue
			}
		}
		if strings.Contains(pLower, "perf") || strings.Contains(pLower, "slow") || strings.Contains(pLower, "memory") {
			if strings.Contains(sLower, "perf") || strings.Contains(sLower, "memory") || strings.Contains(sLower, "lcp") {
				matches = append(matches, s)
				continue
			}
		}
		if strings.Contains(pLower, "review") || strings.Contains(pLower, "audit") {
			if strings.Contains(sLower, "review") || strings.Contains(sLower, "audit") || strings.Contains(sLower, "quality") {
				matches = append(matches, s)
				continue
			}
		}
	}

	if len(matches) > 3 {
		matches = matches[:3]
	}
	return matches
}

// PrintRecommendations renders a clean recommendation banner if matching skills found.
func PrintRecommendations(recs []*Skill) {
	if len(recs) == 0 {
		return
	}
	fmt.Println(ui.PrintSystemInfo("💡 Smart Skill Recommendation:"))
	for _, s := range recs {
		fmt.Printf("   • %s [%s] - %s\n", s.Name, s.Category, s.Description)
	}
}
