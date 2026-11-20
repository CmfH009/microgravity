package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CmfH009/microgravity/internal/mcp"
	"github.com/CmfH009/microgravity/internal/vision"
)

// RegisterVisionTools registers local Vision & Perception tools (YOLO, SAM, Depth, VLM).
func RegisterVisionTools(r *Registry) {
	// ── 1. detect_objects ──
	r.Register(mcp.Tool{
		Name:        "detect_objects",
		Description: "Detect objects in an image using YOLO26. Returns bounding boxes, labels, and confidence scores.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ImagePath":   map[string]interface{}{"type": "string", "description": "Absolute path to image file"},
				"Confidence":  map[string]interface{}{"type": "number", "description": "Confidence threshold (0.0-1.0, default 0.25)"},
				"ModelSize":   map[string]interface{}{"type": "string", "description": "Model size: nano, small, medium, large. Default: small"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ImagePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		imagePath, _ := args["ImagePath"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("ImagePath is required")
		}
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			return "", fmt.Errorf("image file not found: %s", imagePath)
		}

		confidence := 0.25
		if c, ok := args["Confidence"].(float64); ok && c > 0 {
			confidence = c
		}
		modelSize := "small"
		if m, ok := args["ModelSize"].(string); ok && m != "" {
			modelSize = m
		}

		result, elapsed, err := vision.RunInference(ctx, vision.InferenceRequest{
			Op:         "detect",
			Image:      imagePath,
			Confidence: confidence,
			ModelSize:  modelSize,
		})
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("YOLO26 Object Detection (%s, %s)\n", result.Model, result.Device))
		sb.WriteString(fmt.Sprintf("Resolution: %s | Detections: %d | Inference: %s\n\n",
			result.Resolution, result.Count, elapsed.Round(time.Millisecond)))

		if len(result.Detections) > 0 {
			sb.WriteString("| # | Label | Confidence | Box [x1, y1, x2, y2] |\n")
			sb.WriteString("|---|-------|------------|----------------------|\n")
			for i, d := range result.Detections {
				sb.WriteString(fmt.Sprintf("| %d | %s | %.1f%% | [%.0f, %.0f, %.0f, %.0f] |\n",
					i+1, d.Label, d.Confidence*100, d.BBox[0], d.BBox[1], d.BBox[2], d.BBox[3]))
			}
		} else {
			sb.WriteString("No objects detected above confidence threshold.\n")
		}
		return sb.String(), nil
	})

	// ── 2. segment_image ──
	r.Register(mcp.Tool{
		Name:        "segment_image",
		Description: "Segment objects in an image using Segment Anything (SAM 3). Generates precise pixel-level binary masks.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ImagePath":   map[string]interface{}{"type": "string", "description": "Absolute path to image file"},
				"Prompt":      map[string]interface{}{"type": "string", "description": "Text prompt for segmenting specific concepts"},
				"ModelSize":   map[string]interface{}{"type": "string", "description": "Model size: tiny, small, base, large. Default: small"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ImagePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		imagePath, _ := args["ImagePath"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("ImagePath is required")
		}
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			return "", fmt.Errorf("image file not found: %s", imagePath)
		}

		prompt, _ := args["Prompt"].(string)
		modelSize := "small"
		if m, ok := args["ModelSize"].(string); ok && m != "" {
			modelSize = m
		}

		result, elapsed, err := vision.RunInference(ctx, vision.InferenceRequest{
			Op:        "segment",
			Image:     imagePath,
			Prompt:    prompt,
			ModelSize: modelSize,
		})
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("SAM 3 Segmentation (%s, %s)\n", result.Model, result.Device))
		sb.WriteString(fmt.Sprintf("Resolution: %s | Masks: %d | Inference: %s\n",
			result.Resolution, result.Count, elapsed.Round(time.Millisecond)))
		if result.PromptUsed != "" {
			sb.WriteString(fmt.Sprintf("Prompt: %s\n", result.PromptUsed))
		}
		sb.WriteString("\n")

		for _, m := range result.Masks {
			sb.WriteString(fmt.Sprintf("- Mask [%s]: %s (%.0f px)\n", m.Index, m.MaskPath, m.AreaPixels))
		}
		return sb.String(), nil
	})

	// ── 3. estimate_depth ──
	r.Register(mcp.Tool{
		Name:        "estimate_depth",
		Description: "Estimate monocular depth map from a single image using Depth Anything V2. Saves depth visualization as PNG.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ImagePath":   map[string]interface{}{"type": "string", "description": "Absolute path to image file"},
				"ModelSize":   map[string]interface{}{"type": "string", "description": "Model size: small, base, large. Default: small"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ImagePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		imagePath, _ := args["ImagePath"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("ImagePath is required")
		}
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			return "", fmt.Errorf("image file not found: %s", imagePath)
		}

		modelSize := "small"
		if m, ok := args["ModelSize"].(string); ok && m != "" {
			modelSize = m
		}

		result, elapsed, err := vision.RunInference(ctx, vision.InferenceRequest{
			Op:        "depth",
			Image:     imagePath,
			ModelSize: modelSize,
		})
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("Depth Anything V2 (%s, %s)\nResolution: %s | Inference: %s\nDepth map saved: %s",
			result.Model, result.Device, result.Resolution, elapsed.Round(time.Millisecond), result.DepthPath), nil
	})

	// ── 4. describe_image ──
	r.Register(mcp.Tool{
		Name:        "describe_image",
		Description: "Describe or answer questions about an image using a Vision Language Model (PaliGemma 2). Supports visual reasoning, VQA, and scene understanding.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ImagePath":   map[string]interface{}{"type": "string", "description": "Absolute path to image file"},
				"Question":    map[string]interface{}{"type": "string", "description": "Question to ask about the image (default: 'Describe this image in detail.')"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ImagePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		imagePath, _ := args["ImagePath"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("ImagePath is required")
		}
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			return "", fmt.Errorf("image file not found: %s", imagePath)
		}

		question := "Describe this image in detail."
		if q, ok := args["Question"].(string); ok && q != "" {
			question = q
		}

		result, elapsed, err := vision.RunInference(ctx, vision.InferenceRequest{
			Op:       "describe",
			Image:    imagePath,
			Question: question,
		})
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("VLM Response (%s, %s) | Inference: %s\nQ: %s\nA: %s",
			result.Model, result.Device, elapsed.Round(time.Millisecond), result.Question, result.Description), nil
	})

	// ── 5. detect_and_segment ──
	r.Register(mcp.Tool{
		Name:        "detect_and_segment",
		Description: "Full pipeline: detect objects with YOLO26, then segment each detection with SAM 3. Returns per-object masks with labels.",
		InputSchema: mcp.Schema{
			Type: "object",
			Properties: map[string]interface{}{
				"ImagePath":   map[string]interface{}{"type": "string", "description": "Absolute path to image file"},
				"Confidence":  map[string]interface{}{"type": "number", "description": "Detection confidence threshold (default 0.25)"},
				"toolAction":  map[string]interface{}{"type": "string"},
				"toolSummary": map[string]interface{}{"type": "string"},
			},
			Required: []string{"ImagePath"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		imagePath, _ := args["ImagePath"].(string)
		if imagePath == "" {
			return "", fmt.Errorf("ImagePath is required")
		}
		if _, err := os.Stat(imagePath); os.IsNotExist(err) {
			return "", fmt.Errorf("image file not found: %s", imagePath)
		}

		confidence := 0.25
		if c, ok := args["Confidence"].(float64); ok && c > 0 {
			confidence = c
		}

		result, elapsed, err := vision.RunInference(ctx, vision.InferenceRequest{
			Op:         "detect_and_segment",
			Image:      imagePath,
			Confidence: confidence,
		})
		if err != nil {
			return "", err
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("YOLO26 + SAM 3 Pipeline (%s)\n", result.Device))
		sb.WriteString(fmt.Sprintf("Segments: %d | Inference: %s\n\n", result.Count, elapsed.Round(time.Millisecond)))

		if result.Note != "" {
			sb.WriteString(result.Note + "\n")
		}

		if len(result.Segments) > 0 {
			sb.WriteString("| # | Label | Conf | Mask | Area (px) |\n")
			sb.WriteString("|---|-------|------|------|-----------|\n")
			for i, s := range result.Segments {
				sb.WriteString(fmt.Sprintf("| %d | %s | %.1f%% | %s | %.0f |\n",
					i+1, s.Label, s.Confidence*100, filepath.Base(s.MaskPath), s.AreaPixels))
			}
		}
		return sb.String(), nil
	})
}
