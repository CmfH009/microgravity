package ui

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

var latexSymbolMap = map[string]string{
	`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ", `\epsilon`: "ε",
	`\theta`: "θ", `\lambda`: "λ", `\mu`: "μ", `\pi`: "π", `\sigma`: "σ", `\omega`: "ω",
	`\sum`: "∑", `\prod`: "∏", `\int`: "∫", `\sqrt`: "√", `\pm`: "±", `\times`: "×", `\div`: "÷",
	`\cdot`: "·", `\leq`: "≤", `\geq`: "≥", `\neq`: "≠", `\approx`: "≈", `\equiv`: "≡",
	`\infty`: "∞", `\partial`: "∂", `\nabla`: "∇", `\in`: "∈", `\notin`: "∉", `\subset`: "⊂",
	`\cup`: "∪", `\cap`: "∩", `\forall`: "∀", `\exists`: "∃", `\to`: "→", `\rightarrow`: "→",
	`\leftarrow`: "←", `\Leftarrow`: "⇐", `\Rightarrow`: "⇒", `\iff`: "⇔",
}

var superscriptMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ', 'x': 'ˣ', 'y': 'ʸ',
}

var latexCommands = func() []string {
	commands := make([]string, 0, len(latexSymbolMap))
	for command := range latexSymbolMap {
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return len(commands[i]) > len(commands[j]) })
	return commands
}()

func RenderLaTeX(text string) string {
	if os.Getenv("AGY_CLI_DISABLE_LATEX") == "1" || os.Getenv("AGY_CLI_DISABLE_LATEX") == "true" ||
		os.Getenv("MG_DISABLE_LATEX") == "1" || os.Getenv("MG_DISABLE_LATEX") == "true" {
		return text
	}

	for _, command := range latexCommands {
		text = strings.ReplaceAll(text, command, latexSymbolMap[command])
	}

	reSup := regexp.MustCompile(`\^([0-9nixy\+\-\(\)]+)`)
	return reSup.ReplaceAllStringFunc(text, func(match string) string {
		var sb strings.Builder
		for _, r := range match[1:] {
			if sup, ok := superscriptMap[r]; ok {
				sb.WriteRune(sup)
			} else {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	})
}
