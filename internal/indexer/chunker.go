package indexer

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

type Chunk struct {
	ID        string `json:"id"`
	FilePath  string `json:"filepath"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

// ChunkFile splits text into chunks of chunkSize lines with overlap lines.
func ChunkFile(filePath string, content string, chunkSize, overlap int) []Chunk {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(content) == "" {
		return nil
	}

	if chunkSize <= 0 {
		chunkSize = 30
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 5
	}

	var chunks []Chunk
	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}

	for i := 0; i < len(lines); i += step {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunkLines := lines[i:end]
		chunkText := strings.Join(chunkLines, "\n")

		if strings.TrimSpace(chunkText) == "" {
			if end >= len(lines) {
				break
			}
			continue
		}

		startLine := i + 1
		endLine := end

		h := sha256.New()
		h.Write([]byte(fmt.Sprintf("%s:%d-%d:%s", filePath, startLine, endLine, chunkText)))
		id := fmt.Sprintf("%x", h.Sum(nil))[:16]

		chunks = append(chunks, Chunk{
			ID:        id,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Content:   chunkText,
		})

		if end >= len(lines) {
			break
		}
	}

	return chunks
}
