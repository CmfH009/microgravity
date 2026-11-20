package cmdparse

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ToolCall struct {
	ToolCall  string                 `json:"tool_call"`
	Arguments map[string]interface{} `json:"arguments"`
}

// rawCandidate is used to flexibly unmarshal various LLM tool calling dialects.
type rawCandidate struct {
	ToolCall   string                 `json:"tool_call"`
	Name       string                 `json:"name"`
	Tool       string                 `json:"tool"`
	Function   string                 `json:"function"`
	Arguments  map[string]interface{} `json:"arguments"`
	Args       map[string]interface{} `json:"args"`
	Parameters map[string]interface{} `json:"parameters"`
}

func (r rawCandidate) toToolCall() (ToolCall, bool) {
	name := r.ToolCall
	if name == "" {
		name = r.Name
	}
	if name == "" {
		name = r.Tool
	}
	if name == "" {
		name = r.Function
	}
	if name == "" {
		return ToolCall{}, false
	}

	args := r.Arguments
	if args == nil {
		args = r.Args
	}
	if args == nil {
		args = r.Parameters
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	return ToolCall{
		ToolCall:  name,
		Arguments: args,
	}, true
}

// ExtractToolCall extracts the first valid tool call from text (backward compatible).
func ExtractToolCall(text string) (ToolCall, bool, error) {
	calls, ok, err := ExtractToolCalls(text)
	if !ok || len(calls) == 0 {
		return ToolCall{}, false, err
	}
	return calls[0], true, nil
}

// ExtractToolCalls extracts all valid tool calls from text, supporting arrays, markdown fences, and multiple JSON blocks.
func ExtractToolCalls(text string) ([]ToolCall, bool, error) {
	var results []ToolCall

	// 0. Check for markdown code fences (```json ... ``` or ``` ...)
	fenceRe := regexp.MustCompile("(?s)```(?:json)?\\s*\\n([\\s\\S]*?)\\n```")
	matches := fenceRe.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) > 1 {
			fenceBlock := strings.TrimSpace(m[1])
			if strings.HasPrefix(fenceBlock, "[") {
				var rawList []rawCandidate
				err := json.Unmarshal([]byte(fenceBlock), &rawList)
				if err != nil {
					err = json.Unmarshal([]byte(cleanJSONString(fenceBlock)), &rawList)
				}
				if err == nil {
					for _, r := range rawList {
						if tc, valid := r.toToolCall(); valid {
							results = append(results, tc)
						}
					}
				}
			} else {
				if tc, ok := parseLooseToolCall(fenceBlock); ok {
					results = append(results, tc)
				}
			}
		}
	}
	if len(results) > 0 {
		return results, true, nil
	}

	// 1. Check for JSON array of tool calls
	arrStart := strings.Index(text, "[")
	for arrStart >= 0 {
		depth := 0
		inString := false
		escaped := false
		matched := false

		for i := arrStart; i < len(text); i++ {
			ch := text[i]
			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					candidate := text[arrStart : i+1]
					var rawList []rawCandidate
					err := json.Unmarshal([]byte(candidate), &rawList)
					if err != nil {
						err = json.Unmarshal([]byte(cleanJSONString(candidate)), &rawList)
					}
					if err == nil && len(rawList) > 0 {
						for _, r := range rawList {
							if tc, valid := r.toToolCall(); valid {
								results = append(results, tc)
							}
						}
						if len(results) > 0 {
							return results, true, nil
						}
					}
					matched = true
					break
				}
			}
		}
		if matched {
			break
		}
		next := strings.Index(text[arrStart+1:], "[")
		if next < 0 {
			break
		}
		arrStart += next + 1
	}

	// 2. Scan for individual JSON objects
	start := strings.Index(text, "{")
	for start >= 0 {
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
			if depth == 0 {
				candidate := text[start : i+1]
				var raw rawCandidate
				err := json.Unmarshal([]byte(candidate), &raw)
				if err != nil {
					err = json.Unmarshal([]byte(cleanJSONString(candidate)), &raw)
				}
				if err == nil {
					if tc, valid := raw.toToolCall(); valid {
						results = append(results, tc)
						start = i // advance scanner
					}
				}
				break
			}
			}
		}
		next := strings.Index(text[start+1:], "{")
		if next < 0 {
			break
		}
		start += next + 1
	}

	if len(results) > 0 {
		return results, true, nil
	}

	return nil, false, fmt.Errorf("no valid tool call found")
}

func cleanJSONString(s string) string {
	var buf strings.Builder
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				buf.WriteByte(ch)
				continue
			}
			if ch == '\\' {
				escaped = true
				buf.WriteByte(ch)
				continue
			}
			if ch == '"' {
				inString = false
				buf.WriteByte(ch)
				continue
			}
			if ch == '\n' {
				buf.WriteString("\\n")
				continue
			}
			if ch == '\r' {
				buf.WriteString("\\r")
				continue
			}
			if ch == '\t' {
				buf.WriteString("\\t")
				continue
			}
			buf.WriteByte(ch)
		} else {
			if ch == '"' {
				inString = true
			}
			buf.WriteByte(ch)
		}
	}
	return buf.String()
}

func parseLooseToolCall(s string) (ToolCall, bool) {
	var raw rawCandidate
	if err := json.Unmarshal([]byte(s), &raw); err == nil {
		if tc, ok := raw.toToolCall(); ok {
			return tc, true
		}
	}
	if err := json.Unmarshal([]byte(cleanJSONString(s)), &raw); err == nil {
		if tc, ok := raw.toToolCall(); ok {
			return tc, true
		}
	}

	lower := strings.ToLower(s)
	if strings.Contains(lower, "write_to_file") || strings.Contains(lower, "write_file") {
		pathPatterns := []string{
			`"path"\s*:\s*"([^"]+)"`,
			`"file_path"\s*:\s*"([^"]+)"`,
			`"filePath"\s*:\s*"([^"]+)"`,
			`"TargetFile"\s*:\s*"([^"]+)"`,
			`"file"\s*:\s*"([^"]+)"`,
		}
		var targetPath string
		for _, pat := range pathPatterns {
			re := regexp.MustCompile(pat)
			m := re.FindStringSubmatch(s)
			if len(m) > 1 {
				targetPath = m[1]
				break
			}
		}

		contentIdx := strings.Index(s, `"content"`)
		if contentIdx == -1 {
			contentIdx = strings.Index(s, `"CodeContent"`)
		}
		if contentIdx == -1 {
			contentIdx = strings.Index(s, `"code"`)
		}

		if targetPath != "" && contentIdx != -1 {
			colonIdx := strings.Index(s[contentIdx:], ":")
			if colonIdx != -1 {
				valStart := contentIdx + colonIdx + 1
				qStart := strings.Index(s[valStart:], "\"")
				if qStart != -1 {
					bodyStart := valStart + qStart + 1
					lastQ := strings.LastIndex(s, "\"")
					if lastQ > bodyStart {
						contentVal := s[bodyStart:lastQ]
						contentVal = strings.ReplaceAll(contentVal, `\n`, "\n")
						contentVal = strings.ReplaceAll(contentVal, `\t`, "\t")
						return ToolCall{
							ToolCall: "write_to_file",
							Arguments: map[string]interface{}{
								"path":    targetPath,
								"content": contentVal,
							},
						}, true
					}
				}
			}
		}
	}

	return ToolCall{}, false
}


