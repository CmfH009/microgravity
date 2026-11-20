package vision

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type MediaAttachment struct {
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Base64   string `json:"base64"`
}

var mediaRegex = regexp.MustCompile(`(?i)("([^"]+\.(?:png|jpg|jpeg|webp|gif|wav|mp3|m4a|aac|flac|opus|ogg|oga|ogv|mp4|webm))"|'([^']+\.(?:png|jpg|jpeg|webp|gif|wav|mp3|m4a|aac|flac|opus|ogg|oga|ogv|mp4|webm))'|(\S+\.(?:png|jpg|jpeg|webp|gif|wav|mp3|m4a|aac|flac|opus|ogg|oga|ogv|mp4|webm)))`)

var mimeMap = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".wav":  "audio/wav",
	".mp3":  "audio/mp3",
	".m4a":  "audio/m4a",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".opus": "audio/opus",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".ogv":  "video/ogg",
	".mp4":  "video/mp4",
	".webm": "video/webm",
}

func DetectMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if m, ok := mimeMap[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

// ExtractImages removes image & audio media paths from prompt, reads them, and returns base64 encoded strings
func ExtractImages(prompt string) (string, []string, error) {
	var images []string
	var errOut error

	cleanPrompt := mediaRegex.ReplaceAllStringFunc(prompt, func(match string) string {
		if errOut != nil {
			return match
		}

		var path string
		if strings.HasPrefix(match, "\"") && strings.HasSuffix(match, "\"") {
			path = match[1 : len(match)-1]
		} else if strings.HasPrefix(match, "'") && strings.HasSuffix(match, "'") {
			path = match[1 : len(match)-1]
		} else {
			path = match
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return match
			}
			errOut = err
			return match
		}

		mime := DetectMimeType(path)
		images = append(images, base64.StdEncoding.EncodeToString(data))
		return fmt.Sprintf("[Media (%s): %s]", mime, filepath.Base(path))
	})

	if errOut != nil {
		return prompt, nil, errOut
	}

	return cleanPrompt, images, nil
}
