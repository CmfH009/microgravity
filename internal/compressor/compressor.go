package compressor

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// CompressGoCode performs AST-aware pruning of Go source code by stubbing unexported function bodies.
func CompressGoCode(src string) (string, int, int, error) {
	fset := token.NewFileSet()
	fileAst, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src, len(src), len(src), err
	}

	ast.Inspect(fileAst, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			// If function is unexported, stub out its body
			if fn.Name != nil && !ast.IsExported(fn.Name.Name) {
				fn.Body = &ast.BlockStmt{
					List: []ast.Stmt{},
				}
			}
		}
		return true
	})

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, fileAst); err != nil {
		return src, len(src), len(src), err
	}

	compressedStr := buf.String()
	originalBytes := len(src)
	compressedBytes := len(compressedStr)

	return compressedStr, originalBytes, compressedBytes, nil
}

// CompressText removes redundant consecutive empty lines and trailing whitespace.
func CompressText(text string) string {
	lines := strings.Split(text, "\n")
	var cleaned []string
	lastWasEmpty := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == "" {
			if !lastWasEmpty {
				cleaned = append(cleaned, "")
				lastWasEmpty = true
			}
		} else {
			cleaned = append(cleaned, trimmed)
			lastWasEmpty = false
		}
	}

	return strings.Join(cleaned, "\n")
}
