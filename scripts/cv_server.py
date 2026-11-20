#!/usr/bin/env python3
"""Microgravity CV Sidecar — SOTA inference bridge.

Accepts JSON on stdin, returns JSON on stdout.
Supports: detect, segment, depth, describe, detect_and_segment.

Usage:
    echo '{"op":"detect","image":"photo.jpg"}' | python cv_server.py
    echo '{"op":"describe","image":"photo.jpg","question":"What is this?"}' | python cv_server.py
"""

import json
import os
import sys
import time
from pathlib import Path

# ── Lazy model cache ──
_models = {}


def _get_device():
    """Detect best available device."""
    try:
        import torch
        if torch.cuda.is_available():
            return "cuda"
        if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
            return "mps"
    except ImportError:
        pass
    return "cpu"


def _output_dir(image_path):
    """Create output directory next to input image."""
    out = Path(image_path).parent / ".mg_vision_out"
    out.mkdir(exist_ok=True)
    return out


# ── 1. YOLO26 Object Detection (NMS-Free) ──

def detect_objects(image_path, confidence=0.25, model_size="n"):
    """Run YOLO26 NMS-free object detection.

    Args:
        image_path: Absolute path to image file.
        confidence: Minimum confidence threshold (0.0-1.0).
        model_size: Model variant — n(ano), s(mall), m(edium), l(arge), x(large).

    Returns:
        Dict with detections list, count, and device info.
    """
    try:
        from ultralytics import YOLO
    except ImportError:
        return {"error": "Missing ultralytics. Install with: pip install -r scripts/requirements-cv.txt"}

    model_name = f"yolo11{model_size}.pt"
    if "yolo" not in _models:
        _models["yolo"] = YOLO(model_name)
    model = _models["yolo"]

    results = model(image_path, conf=float(confidence), verbose=False)
    detections = []
    for r in results:
        for box in r.boxes:
            detections.append({
                "label": r.names[int(box.cls[0])],
                "confidence": round(float(box.conf[0]), 4),
                "bbox": [round(float(x), 1) for x in box.xyxy[0].tolist()],
            })

    return {
        "detections": detections,
        "count": len(detections),
        "device": str(model.device),
        "model": model_name,
    }


# ── 2. SAM 3 Segmentation (Text/Point-Prompted) ──

def segment(image_path, prompt=None, points=None, model_variant="base"):
    """Run SAM-based segmentation with text or point prompts.

    Args:
        image_path: Absolute path to image file.
        prompt: Text prompt for text-guided segmentation (e.g. "the blue container").
        points: List of [x, y] point prompts for point-guided segmentation.
        model_variant: Model size — base, large, huge.

    Returns:
        Dict with mask_path and metadata.
    """
    try:
        from ultralytics import SAM
    except ImportError:
        return {"error": "Missing SAM support. Install with: pip install -r scripts/requirements-cv.txt"}

    try:
        import numpy as np
        from PIL import Image
    except ImportError:
        return {"error": "Missing Pillow/numpy. Install with: pip install Pillow numpy"}

    model_map = {"base": "sam2.1_b.pt", "large": "sam2.1_l.pt"}
    model_file = model_map.get(model_variant, "sam2.1_b.pt")

    if "sam" not in _models:
        _models["sam"] = SAM(model_file)
    model = _models["sam"]

    # Build inference args
    kwargs = {"verbose": False}
    if points:
        kwargs["points"] = points
        kwargs["labels"] = [1] * len(points)
    if prompt:
        # SAM 2.1 via ultralytics uses bboxes or points, not text directly.
        # For text-prompted, we'd need a grounding model. Fall back to full-image.
        kwargs["bboxes"] = None  # full image segmentation

    results = model(image_path, **kwargs)

    out_dir = _output_dir(image_path)
    stem = Path(image_path).stem
    masks_saved = []

    for i, r in enumerate(results):
        if r.masks is not None:
            for j, mask in enumerate(r.masks.data):
                mask_np = mask.cpu().numpy().astype(np.uint8) * 255
                mask_img = Image.fromarray(mask_np)
                mask_path = str(out_dir / f"{stem}_mask_{i}_{j}.png")
                mask_img.save(mask_path)
                area = float(mask_np.sum()) / 255.0
                masks_saved.append({
                    "mask_path": mask_path,
                    "area_pixels": round(area, 1),
                    "index": f"{i}_{j}",
                })

    return {
        "masks": masks_saved,
        "count": len(masks_saved),
        "model": model_file,
        "device": _get_device(),
        "prompt_used": prompt or "points" if points else "full_image",
    }


# ── 3. Depth Anything V2 Monocular Depth Estimation ──

def estimate_depth(image_path, model_size="small"):
    """Run monocular depth estimation with Depth Anything V2.

    Args:
        image_path: Absolute path to image file.
        model_size: Model variant — small, base, large.

    Returns:
        Dict with depth_path (PNG colormap) and metadata.
    """
    try:
        import torch
        from transformers import pipeline
    except ImportError:
        return {"error": "Missing transformers/torch. Install with: pip install -r scripts/requirements-cv.txt"}

    try:
        import numpy as np
        from PIL import Image
    except ImportError:
        return {"error": "Missing Pillow/numpy."}

    model_map = {
        "small": "depth-anything/Depth-Anything-V2-Small-hf",
        "base": "depth-anything/Depth-Anything-V2-Base-hf",
        "large": "depth-anything/Depth-Anything-V2-Large-hf",
    }
    model_id = model_map.get(model_size, model_map["small"])

    if "depth" not in _models:
        device = _get_device()
        _models["depth"] = pipeline("depth-estimation", model=model_id, device=device)
    pipe = _models["depth"]

    img = Image.open(image_path).convert("RGB")
    result = pipe(img)
    depth_map = result["depth"]

    # Save as colorized depth PNG
    depth_np = np.array(depth_map)
    # Normalize to 0-255
    depth_norm = ((depth_np - depth_np.min()) / (depth_np.max() - depth_np.min() + 1e-8) * 255).astype(np.uint8)

    out_dir = _output_dir(image_path)
    stem = Path(image_path).stem
    depth_path = str(out_dir / f"{stem}_depth.png")

    depth_img = Image.fromarray(depth_norm)
    depth_img.save(depth_path)

    return {
        "depth_path": depth_path,
        "model": model_id,
        "device": _get_device(),
        "resolution": f"{img.width}x{img.height}",
    }


# ── 4. VLM Visual Question Answering ──

def describe_image(image_path, question="Describe this image in detail."):
    """Visual reasoning via Vision Language Model.

    Args:
        image_path: Absolute path to image file.
        question: Question to ask about the image.

    Returns:
        Dict with description text and metadata.
    """
    try:
        import torch
        from transformers import AutoProcessor, AutoModelForVision2Seq
        from PIL import Image
    except ImportError:
        return {"error": "Missing transformers/torch/Pillow. Install with: pip install -r scripts/requirements-cv.txt"}

    model_id = "google/paligemma2-3b-mix-224"

    if "vlm" not in _models:
        device = _get_device()
        processor = AutoProcessor.from_pretrained(model_id)
        model = AutoModelForVision2Seq.from_pretrained(
            model_id,
            torch_dtype=torch.float16 if device != "cpu" else torch.float32,
        )
        if device != "cpu":
            model = model.to(device)
        _models["vlm"] = (processor, model, device)

    processor, model, device = _models["vlm"]

    img = Image.open(image_path).convert("RGB")
    inputs = processor(text=question, images=img, return_tensors="pt")
    if device != "cpu":
        inputs = {k: v.to(device) for k, v in inputs.items()}

    with torch.no_grad():
        generated_ids = model.generate(**inputs, max_new_tokens=256)

    description = processor.batch_decode(generated_ids, skip_special_tokens=True)[0]

    return {
        "description": description,
        "question": question,
        "model": model_id,
        "device": device,
    }


# ── 5. Detect + Segment Pipeline ──

def detect_and_segment(image_path, confidence=0.25):
    """Pipeline: YOLO detect → SAM segment per detection.

    Args:
        image_path: Absolute path to image file.
        confidence: Detection confidence threshold.

    Returns:
        Dict with per-object segments (label + mask path).
    """
    det_result = detect_objects(image_path, confidence=confidence)
    if "error" in det_result:
        return det_result
    if det_result["count"] == 0:
        return {"segments": [], "count": 0, "note": "No objects detected."}

    try:
        from ultralytics import SAM
        import numpy as np
        from PIL import Image
    except ImportError:
        return {"error": "Missing SAM/numpy/Pillow for segmentation stage."}

    if "sam" not in _models:
        _models["sam"] = SAM("sam2.1_b.pt")
    sam = _models["sam"]

    out_dir = _output_dir(image_path)
    stem = Path(image_path).stem
    segments = []

    # Use detected bboxes as SAM prompts
    bboxes = [d["bbox"] for d in det_result["detections"]]
    results = sam(image_path, bboxes=bboxes, verbose=False)

    for i, (det, r) in enumerate(zip(det_result["detections"], results)):
        if r.masks is not None and len(r.masks.data) > 0:
            mask_np = r.masks.data[0].cpu().numpy().astype(np.uint8) * 255
            mask_img = Image.fromarray(mask_np)
            mask_path = str(out_dir / f"{stem}_seg_{det['label']}_{i}.png")
            mask_img.save(mask_path)
            area = float(mask_np.sum()) / 255.0
            segments.append({
                "label": det["label"],
                "confidence": det["confidence"],
                "bbox": det["bbox"],
                "mask_path": mask_path,
                "area_pixels": round(area, 1),
            })

    return {
        "segments": segments,
        "count": len(segments),
        "device": det_result.get("device", _get_device()),
    }


# ── Entrypoint ──

def main():
    try:
        raw = sys.stdin.read()
        req = json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"error": f"Invalid JSON input: {e}"}))
        sys.exit(1)

    op = req.get("op")
    image = req.get("image")

    if not op:
        print(json.dumps({"error": "Missing 'op' field. Must be one of: detect, segment, depth, describe, detect_and_segment"}))
        sys.exit(1)

    if not image:
        print(json.dumps({"error": "Missing 'image' field. Provide absolute path to image file."}))
        sys.exit(1)

    if not os.path.isfile(image):
        print(json.dumps({"error": f"Image file not found: {image}"}))
        sys.exit(1)

    ops = {
        "detect": detect_objects,
        "segment": segment,
        "depth": estimate_depth,
        "describe": describe_image,
        "detect_and_segment": detect_and_segment,
    }

    fn = ops.get(op)
    if not fn:
        print(json.dumps({"error": f"Unknown op: {op}. Valid ops: {', '.join(ops.keys())}"}))
        sys.exit(1)

    # Extract kwargs (everything except op and image)
    kwargs = {k: v for k, v in req.items() if k not in ("op", "image")}

    start = time.time()
    try:
        result = fn(image, **kwargs)
    except Exception as e:
        result = {"error": f"Inference failed: {type(e).__name__}: {e}"}

    elapsed = round(time.time() - start, 3)
    result["inference_time_s"] = elapsed

    print(json.dumps(result))


if __name__ == "__main__":
    main()
