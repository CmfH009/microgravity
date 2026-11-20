package vision

import (
	"encoding/json"
	"testing"
)

func TestInferenceRequestSerialization(t *testing.T) {
	req := InferenceRequest{
		Op:         "detect",
		Image:      "/tmp/test.jpg",
		Confidence: 0.5,
		ModelSize:  "n",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal InferenceRequest: %v", err)
	}

	var decoded InferenceRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal InferenceRequest: %v", err)
	}

	if decoded.Op != "detect" {
		t.Errorf("expected op=detect, got %s", decoded.Op)
	}
	if decoded.Confidence != 0.5 {
		t.Errorf("expected confidence=0.5, got %f", decoded.Confidence)
	}
}

func TestInferenceResultDeserialization(t *testing.T) {
	raw := `{
		"detections": [
			{"label": "person", "confidence": 0.95, "bbox": [10.0, 20.0, 100.0, 200.0]},
			{"label": "car", "confidence": 0.87, "bbox": [50.0, 60.0, 300.0, 400.0]}
		],
		"count": 2,
		"device": "cuda",
		"model": "yolo11n.pt",
		"inference_time_s": 0.123
	}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal InferenceResult: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("expected count=2, got %d", result.Count)
	}
	if len(result.Detections) != 2 {
		t.Errorf("expected 2 detections, got %d", len(result.Detections))
	}
	if result.Detections[0].Label != "person" {
		t.Errorf("expected first label=person, got %s", result.Detections[0].Label)
	}
	if result.Device != "cuda" {
		t.Errorf("expected device=cuda, got %s", result.Device)
	}
	if result.InferenceTime != 0.123 {
		t.Errorf("expected inference_time_s=0.123, got %f", result.InferenceTime)
	}
}

func TestInferenceResultSegmentation(t *testing.T) {
	raw := `{
		"masks": [
			{"mask_path": "/tmp/out/mask_0_0.png", "area_pixels": 15000.5, "index": "0_0"}
		],
		"count": 1,
		"model": "sam2.1_b.pt",
		"device": "cpu",
		"prompt_used": "the blue container"
	}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal segmentation result: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("expected count=1, got %d", result.Count)
	}
	if len(result.Masks) != 1 {
		t.Errorf("expected 1 mask, got %d", len(result.Masks))
	}
	if result.Masks[0].MaskPath != "/tmp/out/mask_0_0.png" {
		t.Errorf("unexpected mask path: %s", result.Masks[0].MaskPath)
	}
	if result.PromptUsed != "the blue container" {
		t.Errorf("expected prompt_used='the blue container', got %s", result.PromptUsed)
	}
}

func TestInferenceResultDepth(t *testing.T) {
	raw := `{
		"depth_path": "/tmp/out/scene_depth.png",
		"model": "depth-anything/Depth-Anything-V2-Small-hf",
		"device": "cuda",
		"resolution": "1920x1080"
	}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal depth result: %v", err)
	}

	if result.DepthPath != "/tmp/out/scene_depth.png" {
		t.Errorf("unexpected depth path: %s", result.DepthPath)
	}
	if result.Resolution != "1920x1080" {
		t.Errorf("expected resolution=1920x1080, got %s", result.Resolution)
	}
}

func TestInferenceResultVLM(t *testing.T) {
	raw := `{
		"description": "A cat sitting on a windowsill looking outside.",
		"question": "What is in this image?",
		"model": "google/paligemma2-3b-mix-224",
		"device": "cuda"
	}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal VLM result: %v", err)
	}

	if result.Description != "A cat sitting on a windowsill looking outside." {
		t.Errorf("unexpected description: %s", result.Description)
	}
	if result.Question != "What is in this image?" {
		t.Errorf("unexpected question: %s", result.Question)
	}
}

func TestInferenceResultError(t *testing.T) {
	raw := `{"error": "Missing ultralytics. Install with: pip install -r scripts/requirements-cv.txt"}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal error result: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error field")
	}
}

func TestFindCVScript(t *testing.T) {
	// Just verify it returns a non-empty path without panicking
	path := FindCVScript()
	if path == "" {
		t.Error("FindCVScript returned empty path")
	}
}

func TestDetectAndSegmentResultDeserialization(t *testing.T) {
	raw := `{
		"segments": [
			{
				"label": "person",
				"confidence": 0.95,
				"bbox": [10.0, 20.0, 100.0, 200.0],
				"mask_path": "/tmp/out/seg_person_0.png",
				"area_pixels": 25000.0
			}
		],
		"count": 1,
		"device": "cuda"
	}`

	var result InferenceResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Failed to unmarshal detect+segment result: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("expected count=1, got %d", result.Count)
	}
	if len(result.Segments) != 1 {
		t.Errorf("expected 1 segment, got %d", len(result.Segments))
	}
	if result.Segments[0].Label != "person" {
		t.Errorf("expected label=person, got %s", result.Segments[0].Label)
	}
}
