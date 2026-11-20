package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CmfH009/microgravity/internal/journal"
	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/policy"
	"github.com/CmfH009/microgravity/internal/preview"
	"github.com/CmfH009/microgravity/internal/secrets"
)

var errLimitReached = errors.New("search limit reached")

// atomicWriteFile writes data to targetPath atomically using a temp file rename.
func atomicWriteFile(targetPath string, data []byte, perm os.FileMode) error {
	return atomicWriteFileWithRename(targetPath, data, perm, os.Rename)
}

func atomicWriteFileWithRename(targetPath string, data []byte, perm os.FileMode, renameFn func(string, string) error) error {
	if info, statErr := os.Lstat(targetPath); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("target path %q is a directory", targetPath)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink %q", targetPath)
		}
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mg-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		// Non-fatal on Windows
		_ = err
	}

	// Try direct atomic rename first (works on POSIX and modern Windows when target is unlocked).
	renameErr := renameFn(tmpName, targetPath)
	if renameErr == nil {
		cleanupTmp = false
		return nil
	}

	// If target does not exist, direct rename failure cannot be recovered by staging a backup.
	if _, statErr := os.Lstat(targetPath); statErr != nil {
		return fmt.Errorf("atomic rename failed: %w", renameErr)
	}

	// Stage a unique backup of the existing target so we never silently lose it on Windows.
	bakTmp, bakErr := os.CreateTemp(dir, ".mg-bak-*")
	if bakErr != nil {
		return fmt.Errorf("stage backup before replace: %w (rename error: %v)", bakErr, renameErr)
	}
	backupPath := bakTmp.Name()
	_ = bakTmp.Close()
	_ = os.Remove(backupPath)

	if bakRenameErr := renameFn(targetPath, backupPath); bakRenameErr != nil {
		return fmt.Errorf("backup target before replace: %w (rename error: %v)", bakRenameErr, renameErr)
	}

	if replaceErr := renameFn(tmpName, targetPath); replaceErr != nil {
		if rbErr := renameFn(backupPath, targetPath); rbErr != nil {
			return fmt.Errorf("atomic replace failed (%w) and rollback from %q failed (%v)", replaceErr, backupPath, rbErr)
		}
		return fmt.Errorf("atomic replace failed (original file restored): %w", replaceErr)
	}

	cleanupTmp = false
	_ = os.Remove(backupPath)
	return nil
}

// RegisterFSTools registers all filesystem tools (view, write, replace, grep, find, list).
func RegisterFSTools(r *Registry, defaultPolicy *policy.Policy) {
	// ── 1. view_file ──
	r.Register(mcp.Tool{
		Name:        "view_file",
		Description: "View contents of a file with line numbers, 1-indexed slicing, and 46KB truncation",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"AbsolutePath":  map[string]interface{}{"type": "string", "description": "Path to file to view. Must be an absolute path or relative to workspace."},
				"StartLine":     map[string]interface{}{"type": "integer", "description": "1-indexed start line"},
				"EndLine":       map[string]interface{}{"type": "integer", "description": "1-indexed end line (inclusive)"},
				"ContentOffset": map[string]interface{}{"type": "integer", "description": "Byte offset into file content"},
				"toolAction":    map[string]interface{}{"type": "string"},
				"toolSummary":   map[string]interface{}{"type": "string"},
			},
			Required: []string{"AbsolutePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		path, _ := args["AbsolutePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		if path == "" {
			path, _ = args["file"].(string)
		}
		if path == "" {
			path, _ = args["file_path"].(string)
		}
		if path == "" {
			path, _ = args["filePath"].(string)
		}
		if path == "" {
			path, _ = args["filename"].(string)
		}
		if path == "" {
			return "", fmt.Errorf("AbsolutePath is required")
		}
		resolvedPath, err := defaultPolicy.ResolvePath(path)
		if err != nil {
			return "", err
		}
		path = resolvedPath

		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}

		byteOffset := 0
		if o, ok := args["ContentOffset"].(float64); ok && int(o) > 0 {
			byteOffset = int(o)
		} else if oInt, ok := args["ContentOffset"].(int); ok && oInt > 0 {
			byteOffset = oInt
		}

		firstLineNum := 1
		if byteOffset > 0 && byteOffset < len(data) {
			firstLineNum = strings.Count(string(data[:byteOffset]), "\n") + 1
			data = data[byteOffset:]
		}

		truncated := false
		if len(data) > 46080 {
			data = data[:46080]
			truncated = true
		}

		lines := strings.Split(string(data), "\n")
		startLine := 1
		endLine := len(lines)

		if s, ok := args["StartLine"].(float64); ok && int(s) > 0 {
			startLine = int(s)
		} else if sInt, ok := args["StartLine"].(int); ok && sInt > 0 {
			startLine = sInt
		}
		if e, ok := args["EndLine"].(float64); ok && int(e) > 0 && int(e) <= len(lines) {
			endLine = int(e)
		} else if eInt, ok := args["EndLine"].(int); ok && eInt > 0 && eInt <= len(lines) {
			endLine = eInt
		}

		if startLine > len(lines) {
			return "", fmt.Errorf("StartLine %d exceeds file line count %d", startLine, len(lines))
		}

		var sb strings.Builder
		for i := startLine - 1; i < endLine && i < len(lines); i++ {
			sb.WriteString(fmt.Sprintf("%d: %s\n", firstLineNum+i, lines[i]))
		}

		if truncated {
			sb.WriteString("\n[Content truncated at 46KB limit. Use ContentOffset to read further.]\n")
		}

		return sb.String(), nil
	})

	// ── 2. write_to_file ──
	r.Register(mcp.Tool{
		Name:        "write_to_file",
		Description: "Create new file or overwrite existing file with artifact metadata support",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"TargetFile":       map[string]interface{}{"type": "string"},
				"CodeContent":      map[string]interface{}{"type": "string"},
				"Overwrite":        map[string]interface{}{"type": "boolean"},
				"Description":      map[string]interface{}{"type": "string"},
				"ArtifactMetadata": map[string]interface{}{"type": "object"},
				"AllowSecrets":     map[string]interface{}{"type": "boolean"},
				"toolAction":       map[string]interface{}{"type": "string"},
				"toolSummary":      map[string]interface{}{"type": "string"},
			},
			Required: []string{"TargetFile", "CodeContent", "Overwrite"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		target, _ := args["TargetFile"].(string)
		if target == "" {
			target, _ = args["path"].(string)
		}
		if target == "" {
			target, _ = args["file"].(string)
		}
		if target == "" {
			target, _ = args["file_path"].(string)
		}
		if target == "" {
			target, _ = args["filePath"].(string)
		}
		if target == "" {
			target, _ = args["target_file"].(string)
		}
		if target == "" {
			target, _ = args["filename"].(string)
		}

		content, _ := args["CodeContent"].(string)
		if content == "" {
			content, _ = args["content"].(string)
		}
		if content == "" {
			content, _ = args["code"].(string)
		}

		overwrite := true
		if o, ok := args["Overwrite"].(bool); ok {
			overwrite = o
		} else if o, ok := args["overwrite"].(bool); ok {
			overwrite = o
		}

		if target == "" {
			return "", fmt.Errorf("TargetFile is required")
		}
		resolvedTarget, err := defaultPolicy.ResolvePath(target)
		if err != nil {
			return "", err
		}
		target = resolvedTarget

		if _, err := os.Stat(target); err == nil && !overwrite {
			return "", fmt.Errorf("file %s already exists, set Overwrite=true to overwrite", target)
		}

		allowSecrets, _ := args["AllowSecrets"].(bool)
		if !allowSecrets && !secrets.Allowed() {
			if findings := secrets.Scan(content); len(findings) > 0 {
				return "", secrets.BlockError(findings)
			}
		}

		// Journal: capture original content before write (supports mg undo)
		var original string
		if data, err := os.ReadFile(target); err == nil {
			original = string(data)
		}
		if err := journal.Default().Record("write", target, original, content); err != nil {
			// Journaling failure should not block the write
		}

		if err := atomicWriteFile(target, []byte(content), 0644); err != nil {
			return "", err
		}

		preview.NotifyFileChanged(target)
		return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), target), nil
	})

	// ── 3. replace_file_content ──
	r.Register(mcp.Tool{
		Name:        "replace_file_content",
		Description: "Edit existing file by replacing a contiguous target block of text with replacement content",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"TargetFile":         map[string]interface{}{"type": "string"},
				"TargetContent":      map[string]interface{}{"type": "string"},
				"ReplacementContent": map[string]interface{}{"type": "string"},
				"AllowMultiple":      map[string]interface{}{"type": "boolean"},
				"StartLine":          map[string]interface{}{"type": "integer"},
				"EndLine":            map[string]interface{}{"type": "integer"},
				"Instruction":        map[string]interface{}{"type": "string"},
				"Description":        map[string]interface{}{"type": "string"},
				"AllowSecrets":       map[string]interface{}{"type": "boolean"},
				"TargetLintErrorIds": map[string]interface{}{"type": "array"},
				"toolAction":         map[string]interface{}{"type": "string"},
				"toolSummary":        map[string]interface{}{"type": "string"},
			},
			Required: []string{"TargetFile", "TargetContent", "ReplacementContent"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		targetFile, _ := args["TargetFile"].(string)
		if targetFile == "" {
			targetFile, _ = args["path"].(string)
		}
		if targetFile == "" {
			targetFile, _ = args["file"].(string)
		}
		if targetFile == "" {
			targetFile, _ = args["file_path"].(string)
		}
		if targetFile == "" {
			targetFile, _ = args["filePath"].(string)
		}
		if targetFile == "" {
			targetFile, _ = args["target_file"].(string)
		}
		if targetFile == "" {
			targetFile, _ = args["filename"].(string)
		}

		targetContent, _ := args["TargetContent"].(string)
		if targetContent == "" {
			targetContent, _ = args["target"].(string)
		}
		if targetContent == "" {
			targetContent, _ = args["target_content"].(string)
		}
		if targetContent == "" {
			targetContent, _ = args["old_str"].(string)
		}

		replacement, _ := args["ReplacementContent"].(string)
		if replacement == "" {
			replacement, _ = args["replacement"].(string)
		}
		if replacement == "" {
			replacement, _ = args["replacement_content"].(string)
		}
		if replacement == "" {
			replacement, _ = args["new_str"].(string)
		}

		allowMultiple, ok := args["AllowMultiple"].(bool)
		if !ok {
			if a, ok := args["allow_multiple"].(bool); ok {
				allowMultiple = a
			}
		}

		if targetFile == "" || targetContent == "" {
			return "", fmt.Errorf("TargetFile and TargetContent are required")
		}
		resolvedTargetFile, err := defaultPolicy.ResolvePath(targetFile)
		if err != nil {
			return "", err
		}
		targetFile = resolvedTargetFile

		data, err := os.ReadFile(targetFile)
		if err != nil {
			return "", err
		}

		strData := string(data)
		hasCRLF := strings.Contains(strData, "\r\n")

		// Normalize CRLF to LF for matching and editing
		strDataNorm := strings.ReplaceAll(strData, "\r\n", "\n")
		targetNorm := strings.ReplaceAll(targetContent, "\r\n", "\n")
		replacementNorm := strings.ReplaceAll(replacement, "\r\n", "\n")

		// Parse StartLine & EndLine if provided
		startLine := 0
		endLine := 0
		if s, ok := args["StartLine"].(float64); ok && int(s) > 0 {
			startLine = int(s)
		} else if sInt, ok := args["StartLine"].(int); ok && sInt > 0 {
			startLine = sInt
		}
		if e, ok := args["EndLine"].(float64); ok && int(e) > 0 {
			endLine = int(e)
		} else if eInt, ok := args["EndLine"].(int); ok && eInt > 0 {
			endLine = eInt
		}

		var updated string
		if startLine > 0 && endLine >= startLine {
			// Line-range restricted replacement
			lines := strings.Split(strDataNorm, "\n")
			if startLine > len(lines) {
				return "", fmt.Errorf("StartLine %d exceeds line count %d", startLine, len(lines))
			}
			if endLine > len(lines) {
				endLine = len(lines)
			}

			before := strings.Join(lines[:startLine-1], "\n")
			if startLine > 1 {
				before += "\n"
			}
			chunk := strings.Join(lines[startLine-1:endLine], "\n")
			after := ""
			if endLine < len(lines) {
				after = "\n" + strings.Join(lines[endLine:], "\n")
			}

			if !strings.Contains(chunk, targetNorm) {
				return "", fmt.Errorf("TargetContent not found within lines %d-%d of %s", startLine, endLine, targetFile)
			}

			var replacedChunk string
			if allowMultiple {
				replacedChunk = strings.ReplaceAll(chunk, targetNorm, replacementNorm)
			} else {
				replacedChunk = strings.Replace(chunk, targetNorm, replacementNorm, 1)
			}
			updated = before + replacedChunk + after
		} else {
			// Global file replacement
			count := strings.Count(strDataNorm, targetNorm)
			if count == 0 {
				return "", fmt.Errorf("TargetContent not found in %s", targetFile)
			}
			if count > 1 && !allowMultiple {
				return "", fmt.Errorf("TargetContent found %d times in %s, set AllowMultiple=true to replace all", count, targetFile)
			}

			if allowMultiple {
				updated = strings.ReplaceAll(strDataNorm, targetNorm, replacementNorm)
			} else {
				updated = strings.Replace(strDataNorm, targetNorm, replacementNorm, 1)
			}
		}

		// Restore original CRLF convention if source file used CRLF
		if hasCRLF {
			updated = strings.ReplaceAll(strings.ReplaceAll(updated, "\r\n", "\n"), "\n", "\r\n")
		}

		// Secret-scan only the newly inserted text so files that already
		// contain credentials remain editable without the override.
		allowSecrets, _ := args["AllowSecrets"].(bool)
		if !allowSecrets && !secrets.Allowed() {
			if findings := secrets.Scan(replacementNorm); len(findings) > 0 {
				return "", secrets.BlockError(findings)
			}
		}

		// Journal: capture original content before replace (supports mg undo)
		if err := journal.Default().Record("replace", targetFile, strData, updated); err != nil {
			// Journaling failure should not block the write
		}

		if err := atomicWriteFile(targetFile, []byte(updated), 0644); err != nil {
			return "", err
		}

		preview.NotifyFileChanged(targetFile)
		return fmt.Sprintf("Successfully replaced content in %s", targetFile), nil
	})

	// ── 4. grep_search ──
	r.Register(mcp.Tool{
		Name:        "grep_search",
		Description: "Find exact or regex pattern matches within files in a path",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"SearchPath":      map[string]interface{}{"type": "string"},
				"Query":           map[string]interface{}{"type": "string"},
				"CaseInsensitive": map[string]interface{}{"type": "boolean"},
				"IsRegex":         map[string]interface{}{"type": "boolean"},
				"MatchPerLine":    map[string]interface{}{"type": "boolean"},
				"Includes":        map[string]interface{}{"type": "array"},
				"toolAction":      map[string]interface{}{"type": "string"},
				"toolSummary":     map[string]interface{}{"type": "string"},
			},
			Required: []string{"SearchPath", "Query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		searchPath, _ := args["SearchPath"].(string)
		query, _ := args["Query"].(string)
		caseInsensitive, _ := args["CaseInsensitive"].(bool)
		isRegex, _ := args["IsRegex"].(bool)

		if searchPath == "" || query == "" {
			return "", fmt.Errorf("SearchPath and Query are required")
		}

		resolvedPath, err := defaultPolicy.ResolvePath(searchPath)
		if err != nil {
			return "", err
		}
		searchPath = resolvedPath

		var re *regexp.Regexp
		pattern := query
		if !isRegex {
			pattern = regexp.QuoteMeta(query)
		}
		if caseInsensitive {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid regexp: %w", err)
		}

		type matchResult struct {
			Filename    string `json:"Filename"`
			LineNumber  int    `json:"LineNumber"`
			LineContent string `json:"LineContent"`
		}

		var results []matchResult
		errWalk := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == ".git" || defaultPolicy.CheckPathDenied(path) != nil {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if _, rerr := defaultPolicy.ResolvePath(path); rerr != nil {
				return nil
			}
			if info.Size() > 2*1024*1024 {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
			lineNum := 1
			for scanner.Scan() {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				line := scanner.Text()
				if re.MatchString(line) {
					results = append(results, matchResult{
						Filename:    path,
						LineNumber:  lineNum,
						LineContent: line,
					})
					if len(results) >= 50 {
						return errLimitReached
					}
				}
				lineNum++
			}
			return nil
		})
		if errWalk != nil && !errors.Is(errWalk, errLimitReached) {
			return "", errWalk
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return string(data), nil
	})

	// ── 5. find_by_name ──
	r.Register(mcp.Tool{
		Name:        "find_by_name",
		Description: "Search for files and directories within a specified directory using glob pattern matching",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"SearchDirectory": map[string]interface{}{"type": "string"},
				"Pattern":         map[string]interface{}{"type": "string"},
				"Extensions":      map[string]interface{}{"type": "array"},
				"Excludes":        map[string]interface{}{"type": "array"},
				"FullPath":        map[string]interface{}{"type": "boolean"},
				"MaxDepth":        map[string]interface{}{"type": "integer"},
				"Type":            map[string]interface{}{"type": "string"},
				"toolAction":      map[string]interface{}{"type": "string"},
				"toolSummary":     map[string]interface{}{"type": "string"},
			},
			Required: []string{"SearchDirectory", "Pattern"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		searchDir, _ := args["SearchDirectory"].(string)
		pattern, _ := args["Pattern"].(string)

		if searchDir == "" {
			searchDir = "."
		}
		resolved, err := defaultPolicy.ResolvePath(searchDir)
		if err != nil {
			return "", err
		}
		searchDir = resolved

		var matches []string
		errWalk := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == ".git" || defaultPolicy.CheckPathDenied(path) != nil {
					return filepath.SkipDir
				}
			} else if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if _, rerr := defaultPolicy.ResolvePath(path); rerr != nil {
				return nil
			}
			matched, _ := filepath.Match(pattern, info.Name())
			if matched || strings.Contains(strings.ToLower(info.Name()), strings.ToLower(pattern)) {
				matches = append(matches, path)
				if len(matches) >= 50 {
					return errLimitReached
				}
			}
			return nil
		})
		if errWalk != nil && !errors.Is(errWalk, errLimitReached) {
			return "", errWalk
		}

		var sb strings.Builder
		for _, m := range matches {
			sb.WriteString(m + "\n")
		}
		return sb.String(), nil
	})

	// ── 6. list_dir ──
	r.Register(mcp.Tool{
		Name:        "list_dir",
		Description: "List children of a directory path with file sizes and type tags",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"DirectoryPath": map[string]interface{}{"type": "string"},
				"toolAction":    map[string]interface{}{"type": "string"},
				"toolSummary":   map[string]interface{}{"type": "string"},
			},
			Required: []string{"DirectoryPath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		dir, _ := args["DirectoryPath"].(string)
		if dir == "" {
			dir = "."
		}
		resolved, err := defaultPolicy.ResolvePath(dir)
		if err != nil {
			return "", err
		}
		dir = resolved

		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		for _, e := range entries {
			if defaultPolicy.CheckPathDenied(filepath.Join(dir, e.Name())) != nil {
				continue
			}
			tag := "[FILE]"
			if e.IsDir() {
				tag = "[DIR] "
			}
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("%s %-30s %8d bytes\n", tag, e.Name(), size))
		}
		return sb.String(), nil
	})
}
