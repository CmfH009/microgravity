package vision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractImagesAndAudio(t *testing.T) {
	tempDir := t.TempDir()

	imgFile := filepath.Join(tempDir, "screenshot.png")
	_ = os.WriteFile(imgFile, []byte("fake png binary"), 0644)

	audioFile := filepath.Join(tempDir, "voice_memo.mp3")
	_ = os.WriteFile(audioFile, []byte("fake mp3 audio"), 0644)

	prompt := "Check this image " + imgFile + " and listen to " + audioFile
	clean, attachments, err := ExtractImages(prompt)
	if err != nil {
		t.Fatalf("ExtractImages failed: %v", err)
	}

	if len(attachments) != 2 {
		t.Errorf("expected 2 media attachments, got %d", len(attachments))
	}
	if !strings.Contains(clean, "[Media (image/png): screenshot.png]") {
		t.Errorf("clean prompt missing image tag: %s", clean)
	}
	if !strings.Contains(clean, "[Media (audio/mp3): voice_memo.mp3]") {
		t.Errorf("clean prompt missing audio tag: %s", clean)
	}
}
