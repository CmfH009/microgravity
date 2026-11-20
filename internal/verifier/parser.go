package verifier

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Go patterns
	goFailHeaderRegex = regexp.MustCompile(`---\s+FAIL:\s+([A-Za-z0-9_]+)`)
	goFileLineRegex   = regexp.MustCompile(`^\s*([a-zA-Z0-9_\-\/\.\\]+\.go):(\d+):\s*(.*)$`)
	goCompileErrRegex = regexp.MustCompile(`^([a-zA-Z0-9_\-\/\.\\]+\.go):(\d+):(?:\d+:)?\s*(.*)$`)

	// JS/TS patterns
	jsFailFileRegex   = regexp.MustCompile(`FAIL\s+([a-zA-Z0-9_\-\/\.\\]+\.[jt]sx?)`)
	jsTestNameRegex   = regexp.MustCompile(`(?:✕|●)\s+(.+)`)
	jsStackLineRegex  = regexp.MustCompile(`at\s+.*\(?([a-zA-Z0-9_\-\/\.\\]+\.[jt]sx?):(\d+):(\d+)\)?`)
	jsAssertErrRegex  = regexp.MustCompile(`(AssertionError:[^\n]+|Expected:[^\n]+|Received:[^\n]+)`)

	// Pytest patterns
	pytestFailRegex        = regexp.MustCompile(`FAILED\s+([a-zA-Z0-9_\-\/\.\\]+\.py)::(\S+?)(?:\s+-\s+(.*))?$`)
	pytestFileLineRegex    = regexp.MustCompile(`File\s+"([a-zA-Z0-9_\-\/\.\\]+\.py)",\s+line\s+(\d+),\s+in\s+([A-Za-z0-9_<>]+)`)
	pytestShortFileRegex   = regexp.MustCompile(`^\s*([a-zA-Z0-9_\-\/\.\\]+\.py):(\d+):`)
	pytestErrLineRegex     = regexp.MustCompile(`^E\s+(.+)`)
	pytestSectionBannerRe  = regexp.MustCompile(`^={3,}\s*(.*?)\s*={3,}$`)
	pytestTestHeaderBanner = regexp.MustCompile(`^_{3,}\s+(.+?)\s+_{3,}$`)

	// Cargo patterns
	cargoTestFailRegex = regexp.MustCompile(`test\s+([a-zA-Z0-9_:]+)\s+\.\.\.\s+FAILED`)
	cargoPanicRegex    = regexp.MustCompile(`panicked at '([^']*)',\s*([a-zA-Z0-9_\-\/\.\\]+\.rs):(\d+):(\d+)`)
	cargoCompileRegex  = regexp.MustCompile(`error(?:\[E\d+\])?: (.*)`)
	cargoLocationRegex = regexp.MustCompile(`\s+-->\s+([a-zA-Z0-9_\-\/\.\\]+\.rs):(\d+):(\d+)`)
)

// DiagnoseOutput parses test output across Go, JS/TS, Python, and Rust to produce structured failure diagnoses.
func DiagnoseOutput(output string, fw FrameworkType) []FailureDiagnosis {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	switch fw {
	case FrameworkGo:
		return parseGoErrors(output)
	case FrameworkVitest, FrameworkJest, FrameworkNPM:
		return parseJSErrors(output, fw)
	case FrameworkPytest:
		return parsePytestErrors(output)
	case FrameworkCargo:
		return parseCargoErrors(output)
	default:
		// Attempt to run all parsers and aggregate diagnoses
		var diags []FailureDiagnosis
		diags = append(diags, parseGoErrors(output)...)
		diags = append(diags, parseJSErrors(output, fw)...)
		diags = append(diags, parsePytestErrors(output)...)
		diags = append(diags, parseCargoErrors(output)...)
		return deduplicateDiagnoses(diags)
	}
}

func parseGoErrors(output string) []FailureDiagnosis {
	var diags []FailureDiagnosis
	lines := strings.Split(output, "\n")

	var currentTest string
	for _, line := range lines {
		lineTrim := strings.TrimSpace(line)

		if matches := goFailHeaderRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentTest = matches[1]
			continue
		}

		if matches := goFileLineRegex.FindStringSubmatch(line); len(matches) > 3 {
			lineNum, _ := strconv.Atoi(matches[2])
			diags = append(diags, FailureDiagnosis{
				Framework:     FrameworkGo,
				TestName:      currentTest,
				FilePath:      matches[1],
				LineNumber:    lineNum,
				Message:       matches[3],
				OutputSnippet: lineTrim,
				SuggestedFix:  suggestFix(matches[3], FrameworkGo),
			})
			continue
		}

		if matches := goCompileErrRegex.FindStringSubmatch(line); len(matches) > 3 {
			lineNum, _ := strconv.Atoi(matches[2])
			diags = append(diags, FailureDiagnosis{
				Framework:     FrameworkGo,
				FilePath:      matches[1],
				LineNumber:    lineNum,
				Message:       matches[3],
				OutputSnippet: lineTrim,
				SuggestedFix:  suggestFix(matches[3], FrameworkGo),
			})
		}
	}

	return deduplicateDiagnoses(diags)
}

func parseJSErrors(output string, fw FrameworkType) []FailureDiagnosis {
	var diags []FailureDiagnosis
	lines := strings.Split(output, "\n")

	var currentFile string
	var currentTest string
	var currentMsg string

	for _, line := range lines {
		lineTrim := strings.TrimSpace(line)

		if matches := jsFailFileRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentFile = matches[1]
		}
		if matches := jsTestNameRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentTest = matches[1]
		}
		if matches := jsAssertErrRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentMsg = matches[1]
		}

		if matches := jsStackLineRegex.FindStringSubmatch(line); len(matches) > 3 {
			lineNum, _ := strconv.Atoi(matches[2])
			filePath := matches[1]
			if currentFile != "" && !strings.Contains(filePath, "node_modules") {
				filePath = currentFile
			}
			msg := currentMsg
			if msg == "" {
				msg = lineTrim
			}
			diags = append(diags, FailureDiagnosis{
				Framework:     fw,
				TestName:      currentTest,
				FilePath:      filePath,
				LineNumber:    lineNum,
				Message:       msg,
				OutputSnippet: lineTrim,
				SuggestedFix:  suggestFix(msg, fw),
			})
		}
	}

	if len(diags) == 0 && currentFile != "" {
		diags = append(diags, FailureDiagnosis{
			Framework:     fw,
			TestName:      currentTest,
			FilePath:      currentFile,
			Message:       "JS/TS test execution failed",
			OutputSnippet: output,
			SuggestedFix:  "Check test assertions and imports in " + currentFile,
		})
	}

	return deduplicateDiagnoses(diags)
}

func isPytestWarningNoise(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "site-packages") ||
		strings.Contains(lower, "dist-packages") ||
		strings.Contains(line, "Warning:")
}

func parsePytestErrors(output string) []FailureDiagnosis {
	var diags []FailureDiagnosis
	var summaryCandidates []FailureDiagnosis
	lines := strings.Split(output, "\n")

	var currentFile string
	var currentLine int
	var currentTest string
	var errBuf []string
	var errSnippets []string
	var lastShortMsg string
	var blockHadDiag bool
	var inWarningsSummary bool

	flushBlock := func() {
		if currentFile == "" {
			errBuf = nil
			errSnippets = nil
			lastShortMsg = ""
			return
		}
		if len(errBuf) > 0 {
			msg := strings.Join(errBuf, "\n")
			snippet := strings.Join(errSnippets, "\n")
			diags = append(diags, FailureDiagnosis{
				Framework:     FrameworkPytest,
				TestName:      currentTest,
				FilePath:      currentFile,
				LineNumber:    currentLine,
				Message:       msg,
				OutputSnippet: snippet,
				SuggestedFix:  suggestFix(msg, FrameworkPytest),
			})
			blockHadDiag = true
		} else if !blockHadDiag && lastShortMsg != "" {
			diags = append(diags, FailureDiagnosis{
				Framework:     FrameworkPytest,
				TestName:      currentTest,
				FilePath:      currentFile,
				LineNumber:    currentLine,
				Message:       lastShortMsg,
				OutputSnippet: fmt.Sprintf("%s:%d: %s", currentFile, currentLine, lastShortMsg),
				SuggestedFix:  suggestFix(lastShortMsg, FrameworkPytest),
			})
			blockHadDiag = true
		}
		errBuf = nil
		errSnippets = nil
		lastShortMsg = ""
	}

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		lineTrim := strings.TrimSpace(line)
		if lineTrim == "" {
			continue
		}

		if matches := pytestSectionBannerRe.FindStringSubmatch(lineTrim); len(matches) > 1 {
			flushBlock()
			currentFile = ""
			currentLine = 0
			currentTest = ""
			blockHadDiag = false
			titleLower := strings.ToLower(matches[1])
			inWarningsSummary = strings.Contains(titleLower, "warnings summary")
			continue
		}

		if inWarningsSummary || isPytestWarningNoise(line) {
			continue
		}

		if matches := pytestTestHeaderBanner.FindStringSubmatch(lineTrim); len(matches) > 1 && strings.Trim(matches[1], "_ ") != "" {
			flushBlock()
			currentFile = ""
			currentLine = 0
			currentTest = strings.TrimSpace(matches[1])
			blockHadDiag = false
			continue
		}

		if matches := pytestFailRegex.FindStringSubmatch(lineTrim); len(matches) > 2 {
			msg := strings.TrimSpace(matches[3])
			if msg == "" {
				msg = "Pytest assertion/execution failed"
			}
			summaryCandidates = append(summaryCandidates, FailureDiagnosis{
				Framework:     FrameworkPytest,
				TestName:      matches[2],
				FilePath:      matches[1],
				Message:       msg,
				OutputSnippet: lineTrim,
				SuggestedFix:  suggestFix(msg, FrameworkPytest),
			})
			continue
		}

		if matches := pytestFileLineRegex.FindStringSubmatch(line); len(matches) > 3 {
			currentFile = matches[1]
			currentLine, _ = strconv.Atoi(matches[2])
			currentTest = matches[3]
			lastShortMsg = ""
			continue
		}

		if matches := pytestShortFileRegex.FindStringSubmatch(line); len(matches) > 2 {
			if len(errBuf) == 0 && !blockHadDiag && lastShortMsg != "" && currentFile != "" {
				flushBlock()
			}
			currentFile = matches[1]
			currentLine, _ = strconv.Atoi(matches[2])
			rest := strings.TrimSpace(line[len(matches[0]):])
			if len(errBuf) > 0 {
				flushBlock()
			} else if rest != "" && !strings.HasPrefix(rest, "in ") {
				lastShortMsg = rest
			}
			continue
		}

		if matches := pytestErrLineRegex.FindStringSubmatch(line); len(matches) > 1 {
			errText := strings.TrimSpace(matches[1])
			if errText != "" {
				errBuf = append(errBuf, errText)
				errSnippets = append(errSnippets, lineTrim)
			}
		}
	}

	flushBlock()

	diagnosedFiles := make(map[string]bool, len(diags))
	for _, d := range diags {
		if d.FilePath != "" {
			diagnosedFiles[d.FilePath] = true
		}
	}

	for _, cand := range summaryCandidates {
		if !diagnosedFiles[cand.FilePath] {
			diags = append(diags, cand)
			diagnosedFiles[cand.FilePath] = true
		}
	}

	if len(diags) == 0 && currentFile != "" {
		diags = append(diags, FailureDiagnosis{
			Framework:     FrameworkPytest,
			TestName:      currentTest,
			FilePath:      currentFile,
			LineNumber:    currentLine,
			Message:       "Pytest assertion/execution failed",
			OutputSnippet: output,
			SuggestedFix:  "Inspect test assertions in " + currentFile,
		})
	}

	return deduplicateDiagnoses(diags)
}

func parseCargoErrors(output string) []FailureDiagnosis {
	var diags []FailureDiagnosis
	lines := strings.Split(output, "\n")

	var currentTest string
	var lastCompileMsg string

	for i, line := range lines {
		lineTrim := strings.TrimSpace(line)

		if matches := cargoTestFailRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentTest = matches[1]
		}

		if matches := cargoPanicRegex.FindStringSubmatch(line); len(matches) > 3 {
			lineNum, _ := strconv.Atoi(matches[3])
			diags = append(diags, FailureDiagnosis{
				Framework:     FrameworkCargo,
				TestName:      currentTest,
				FilePath:      matches[2],
				LineNumber:    lineNum,
				Message:       matches[1],
				OutputSnippet: lineTrim,
				SuggestedFix:  suggestFix(matches[1], FrameworkCargo),
			})
		}

		if matches := cargoCompileRegex.FindStringSubmatch(line); len(matches) > 1 {
			lastCompileMsg = matches[1]
			// check next lines for location
			if i+1 < len(lines) {
				if locMatches := cargoLocationRegex.FindStringSubmatch(lines[i+1]); len(locMatches) > 2 {
					lineNum, _ := strconv.Atoi(locMatches[2])
					diags = append(diags, FailureDiagnosis{
						Framework:     FrameworkCargo,
						FilePath:      locMatches[1],
						LineNumber:    lineNum,
						Message:       lastCompileMsg,
						OutputSnippet: lineTrim,
						SuggestedFix:  suggestFix(lastCompileMsg, FrameworkCargo),
					})
				}
			}
		}
	}

	return deduplicateDiagnoses(diags)
}

func suggestFix(msg string, fw FrameworkType) string {
	msgLower := strings.ToLower(msg)
	if strings.Contains(msgLower, "got") && strings.Contains(msgLower, "expected") {
		return "Update test assertion value or function return value to match expected output"
	}
	if strings.Contains(msgLower, "assertion") || strings.Contains(msgLower, "assert") {
		return "Fix failing test assertion or underlying calculation"
	}
	if strings.Contains(msgLower, "undefined") || strings.Contains(msgLower, "not defined") || strings.Contains(msgLower, "cannot find") {
		return "Check for missing imports, variable definitions, or typos"
	}
	if strings.Contains(msgLower, "type") || strings.Contains(msgLower, "cannot convert") {
		return "Fix type mismatch or interface implementation"
	}
	return "Inspect code logic around failure point"
}

func deduplicateDiagnoses(diags []FailureDiagnosis) []FailureDiagnosis {
	seen := make(map[string]bool)
	var result []FailureDiagnosis
	for _, d := range diags {
		key := fmt.Sprintf("%s|%s|%s|%d|%s", d.Framework, d.FilePath, d.TestName, d.LineNumber, d.Message)
		if !seen[key] {
			seen[key] = true
			result = append(result, d)
		}
	}
	return result
}

var (
	diagHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true)
	diagFileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Underline(true)
)

func (d FailureDiagnosis) RenderSummary() string {
	loc := d.FilePath
	if d.LineNumber > 0 {
		loc += fmt.Sprintf(":%d", d.LineNumber)
	}
	testInfo := ""
	if d.TestName != "" {
		testInfo = fmt.Sprintf(" in %s()", d.TestName)
	}

	fwStr := strings.ToUpper(string(d.Framework))
	return fmt.Sprintf("%s %s%s\n   ├─ Message: %s\n   └─ Suggested Fix: %s",
		diagHeaderStyle.Render(fmt.Sprintf("[%s FAIL]", fwStr)),
		diagFileStyle.Render(loc),
		testInfo,
		d.Message,
		d.SuggestedFix,
	)
}
