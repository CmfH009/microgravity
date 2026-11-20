package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// InferenceRequest is the JSON payload sent to cv_server.py.
type InferenceRequest struct {
	Op         string    `json:"op"`
	Image      string    `json:"image"`
	Confidence float64   `json:"confidence,omitempty"`
	ModelSize  string    `json:"model_size,omitempty"`
	Prompt     string    `json:"prompt,omitempty"`
	Points     [][]int   `json:"points,omitempty"`
	Question   string    `json:"question,omitempty"`
}

// Detection represents a single detected object.
type Detection struct {
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	BBox       []float64 `json:"bbox"`
}

// SegmentResult represents a single segmented region.
type SegmentResult struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence,omitempty"`
	MaskPath   string  `json:"mask_path"`
	AreaPixels float64 `json:"area_pixels"`
	BBox       []float64 `json:"bbox,omitempty"`
}

// MaskResult represents a single mask from segmentation.
type MaskResult struct {
	MaskPath   string  `json:"mask_path"`
	AreaPixels float64 `json:"area_pixels"`
	Index      string  `json:"index"`
}

// InferenceResult is the parsed JSON response from cv_server.py.
type InferenceResult struct {
	// Detection fields
	Detections []Detection `json:"detections,omitempty"`
	Count      int         `json:"count,omitempty"`

	// Segmentation fields
	Masks    []MaskResult    `json:"masks,omitempty"`
	Segments []SegmentResult `json:"segments,omitempty"`

	// Depth fields
	DepthPath string `json:"depth_path,omitempty"`

	// VLM fields
	Description string `json:"description,omitempty"`
	Question    string `json:"question,omitempty"`

	// Common metadata
	Device        string  `json:"device,omitempty"`
	Model         string  `json:"model,omitempty"`
	Resolution    string  `json:"resolution,omitempty"`
	PromptUsed    string  `json:"prompt_used,omitempty"`
	Note          string  `json:"note,omitempty"`
	InferenceTime float64 `json:"inference_time_s,omitempty"`
	Error         string  `json:"error,omitempty"`
}

// RunInference shells out to cv_server.py and returns the parsed result.
func RunInference(ctx context.Context, req InferenceRequest) (*InferenceResult, time.Duration, error) {
	start := time.Now()

	scriptPath := FindCVScript()
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, time.Since(start), fmt.Errorf("failed to marshal inference request: %w", err)
	}

	cmd := exec.CommandContext(ctx, python, scriptPath)
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		// Provide actionable error message
		if stderrStr != "" {
			return nil, time.Since(start), fmt.Errorf("cv_server.py failed: %v\nstderr: %s", err, stderrStr)
		}
		return nil, time.Since(start), fmt.Errorf("cv_server.py failed: %v (is Python installed and scripts/cv_server.py accessible?)", err)
	}

	var result InferenceResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, time.Since(start), fmt.Errorf("failed to parse cv_server.py output: %v\nraw output: %s", err, stdout.String())
	}

	if result.Error != "" {
		return nil, time.Since(start), fmt.Errorf("cv inference error: %s", result.Error)
	}

	return &result, time.Since(start), nil
}

// FindCVScript locates cv_server.py relative to the binary or working directory.
func FindCVScript() string {
	exe, _ := os.Executable()
	candidates := []string{
		filepath.Join(filepath.Dir(exe), "scripts", "cv_server.py"),
		filepath.Join(filepath.Dir(exe), "..", "scripts", "cv_server.py"),
	}

	// Also check working directory
	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, "scripts", "cv_server.py"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Absolute fallback for dev
	return filepath.Join(cwd, "scripts", "cv_server.py")
}
