package compressor_test

import (
	"strings"
	"testing"

	"github.com/purav/microgravity/internal/compressor"
)

func TestCompressGoCode(t *testing.T) {
	inputCode := `package sample

import "fmt"

// Private function that can be pruned or stripped
func unexportedHelper(a int, b int) int {
	fmt.Println("internal verbose logic")
	x := a * 2
	y := b * 3
	return x + y
}

// Exported API function contract
func CalculateResult(a int, b int) int {
	return unexportedHelper(a, b)
}
`

	result, originalBytes, compressedBytes, err := compressor.CompressGoCode(inputCode)
	if err != nil {
		t.Fatalf("CompressGoCode failed: %v", err)
	}

	if compressedBytes >= originalBytes {
		t.Errorf("Expected compressed bytes (%d) to be less than original bytes (%d)", compressedBytes, originalBytes)
	}

	if !strings.Contains(result, "CalculateResult") {
		t.Errorf("Compressed result missing exported function: %s", result)
	}

	if strings.Contains(result, "internal verbose logic") {
		t.Errorf("Compressed result should have stripped unexported body: %s", result)
	}
}

func TestCompressText(t *testing.T) {
	input := "line 1\n\n\nline 2\nline 1\nline 3\n\n\nline 4"
	compressed := compressor.CompressText(input)

	if strings.Contains(compressed, "\n\n\n") {
		t.Errorf("Compressed text contains consecutive empty lines: %s", compressed)
	}
}
