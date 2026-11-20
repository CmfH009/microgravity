// Package secrets scans text for credential material before it is persisted
// to disk by agent write tools. The guard is intentionally conservative:
// high-confidence credential formats are blocked, while placeholder-looking
// values ("YOUR_KEY_HERE", prose passwords, template variables) are allowed
// so scaffolding and example files are not falsely rejected.
//
// Overrides:
//
//	AllowSecrets=true   per tool call (write_to_file / replace_file_content)
//	MG_ALLOW_SECRET_WRITE=1  process-wide disable for trusted workflows
package secrets

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Finding describes one detected credential-like value in scanned text.
type Finding struct {
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	Preview string `json:"preview"` // masked; never contains the full secret
}

// rule pairs a credential format with its human-readable kind.
type rule struct {
	kind string
	re   *regexp.Regexp
}

// hasCapture marks rules whose first capture group holds the secret value
// (assignment-style patterns) so placeholder validation inspects that value.
type captureRule struct {
	rule
	capture bool
}

var highConfidence = []rule{
	{kind: "AWS access key ID", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{kind: "GitHub token", re: regexp.MustCompile(`\b(?:ghp|gho|ghs|ghr)_[A-Za-z0-9]{36,}\b`)},
	{kind: "GitHub fine-grained PAT", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{kind: "OpenAI API key", re: regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}\b`)},
	{kind: "Google API key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{kind: "Slack token", re: regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
	{kind: "Stripe live key", re: regexp.MustCompile(`\b[srk]k_live_[0-9A-Za-z]{16,}\b`)},
	{kind: "JWT", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{kind: "PEM private key", re: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`)},
}

// assignment-style patterns: the capture group is the candidate secret value.
var assignments = []captureRule{
	{rule: rule{kind: "credential assignment", re: regexp.MustCompile(
		`(?i)\b(?:api[_-]?key|api[_-]?secret|access[_-]?token|auth[_-]?token|client[_-]?secret|password|passwd|secret[_-]?key)\b\s*[:=]\s*["']([^"']{8,})["']`)}, capture: true},
}

// Scan splits text into lines and returns every high-confidence credential
// finding, skipping placeholder-looking values.
func Scan(text string) []Finding {
	var findings []Finding
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		for _, r := range highConfidence {
			for _, m := range r.re.FindAllString(line, -1) {
				if isPlaceholder(m) {
					continue
				}
				findings = append(findings, Finding{
					Kind:    r.kind,
					Line:    i + 1,
					Preview: Mask(m),
				})
			}
		}
		for _, r := range assignments {
			for _, sub := range r.re.FindAllStringSubmatch(line, -1) {
				value := sub[0]
				if len(sub) > 1 && sub[1] != "" {
					value = sub[1]
				}
				// Assignment values containing whitespace are prose
				// ("correct horse battery staple"), not machine secrets.
				if isPlaceholder(value) || strings.ContainsAny(value, " \t") {
					continue
				}
				findings = append(findings, Finding{
					Kind:    r.kind,
					Line:    i + 1,
					Preview: Mask(sub[0]),
				})
			}
		}
	}
	return findings
}

// Mask redacts a matched secret, keeping only a short prefix/suffix so the
// operator can identify which credential tripped the guard without the error
// message re-leaking it.
func Mask(secret string) string {
	s := strings.TrimSpace(secret)
	if strings.HasPrefix(s, "-----BEGIN") {
		return "-----BEGIN ... PRIVATE KEY-----"
	}
	if len(s) <= 10 {
		return strings.Repeat("*", len(s))
	}
	return s[:6] + strings.Repeat("*", 6) + s[len(s)-4:]
}

// isPlaceholder filters scaffold/template values that are not real secrets.
func isPlaceholder(v string) bool {
	if len(v) < 8 {
		return true
	}
	low := strings.ToLower(v)

	// Template/scaffold markers.
	for _, marker := range []string{
		"<", ">", "{{", "}}", "$", "%s", "`",
		"your-", "your_", "yourkey", "your key", "insert", "enter ",
		"placeholder", "example", "changeme", "change-me", "change_me",
		"replace_me", "replace-me", "replace this", "redacted", "dummy",
		"sample", "fake", "xxxxxxxx", "00000000", "11111111", "********",
		"not-a-real", "not_a_real", "password123", "secret123",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}

	// Very low entropy: fewer than 5 distinct characters.
	distinct := map[rune]bool{}
	for _, r := range v {
		distinct[r] = true
		if len(distinct) >= 5 {
			return false
		}
	}
	return true
}

// BlockError formats the tool-facing refusal for detected findings.
func BlockError(findings []Finding) error {
	var b strings.Builder
	fmt.Fprintf(&b, "blocked by secret scan: refusing to persist %d credential-like value(s):", len(findings))
	max := len(findings)
	shown := max
	if shown > 5 {
		shown = 5
	}
	for i := 0; i < shown; i++ {
		f := findings[i]
		fmt.Fprintf(&b, "\n  - %s at line %d (%s)", f.Kind, f.Line, f.Preview)
	}
	if max > shown {
		fmt.Fprintf(&b, "\n  - ...and %d more", max-shown)
	}
	b.WriteString("\nRemove the credential or reference it via an environment variable. ")
	b.WriteString("If writing it is intentional, retry with AllowSecrets=true or set MG_ALLOW_SECRET_WRITE=1.")
	return errors.New(b.String())
}

// Allowed reports whether the process-wide write guard is disabled.
func Allowed() bool {
	v := os.Getenv("MG_ALLOW_SECRET_WRITE")
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}
