package recipe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/pixlog/pixlog/internal/imaging"
)

type InferenceResult struct {
	Recipe     []byte  `json:"recipe"`
	InputOID   string  `json:"input_oid"`
	OutputOID  string  `json:"output_oid"`
	Operation  string  `json:"operation"`
	Confidence float64 `json:"confidence"`
}

func InferFiles(beforePath, afterPath string, options imaging.DiffOptions) (InferenceResult, error) {
	inputOID, err := hashInferenceFile(beforePath)
	if err != nil {
		return InferenceResult{}, err
	}
	outputOID, err := hashInferenceFile(afterPath)
	if err != nil {
		return InferenceResult{}, err
	}
	before, err := os.Open(beforePath)
	if err != nil {
		return InferenceResult{}, fmt.Errorf("open inference input: %w", err)
	}
	defer before.Close()
	after, err := os.Open(afterPath)
	if err != nil {
		return InferenceResult{}, fmt.Errorf("open inference output: %w", err)
	}
	defer after.Close()
	visual, err := imaging.CompareReaders(before, after, options)
	if err != nil {
		return InferenceResult{}, err
	}
	beforeManifest, err := imaging.InspectFile(beforePath, inputOID)
	if err != nil {
		return InferenceResult{}, err
	}
	afterManifest, err := imaging.InspectFile(afterPath, outputOID)
	if err != nil {
		return InferenceResult{}, err
	}
	operation, confidence := classifyInferredOperation(inputOID, outputOID, visual)
	metadataKeys := changedMetadataKeys(beforeManifest.EmbeddedMetadata, afterManifest.EmbeddedMetadata)
	document := map[string]any{
		"schema": Schema,
		"kind":   "inferred-edit",
		"tool": map[string]any{
			"name":            "PixLog visual inference",
			"adapter_version": "1",
		},
		"normalized": map[string]any{
			"operation":  operation,
			"confidence": confidence,
			"geometry":   visual.Geometry,
			"visual": map[string]any{
				"comparable":          visual.Comparable,
				"changed_pixels":      visual.ChangedPixels,
				"compared_pixels":     visual.ComparedPixels,
				"visual_change_ratio": visual.VisualChangeRatio,
				"mean_channel_delta":  visual.MeanChannelDelta,
				"rmse":                visual.RMSE,
				"ssim":                visual.SSIM,
				"regions":             visual.Regions,
			},
			"metadata_changed_keys": metadataKeys,
		},
		"capture": map[string]any{
			"source":          "before-after-visual-inference",
			"adapter":         "pixlog-visual-inference",
			"adapter_version": "1",
			"fidelity":        FidelityInferred,
			"unknown_fields": []string{
				"exact_operation_parameters",
				"application_history",
				"vendor_model_revision",
				"prompt",
				"seed",
			},
		},
		"reproducibility": map[string]any{"status": ReproducibilityInferred},
		"parents": []any{map[string]any{
			"asset": inputOID,
			"role":  "before",
		}},
		"outputs": []any{map[string]any{
			"asset": outputOID,
			"role":  "after",
		}},
	}
	data, err := json.Marshal(document)
	if err != nil {
		return InferenceResult{}, fmt.Errorf("encode inferred recipe: %w", err)
	}
	normalized, err := Normalize(data)
	if err != nil {
		return InferenceResult{}, err
	}
	return InferenceResult{
		Recipe: normalized, InputOID: inputOID, OutputOID: outputOID,
		Operation: operation, Confidence: confidence,
	}, nil
}

func classifyInferredOperation(inputOID, outputOID string, visual imaging.VisualDiff) (string, float64) {
	if inputOID == outputOID {
		return "none", 1
	}
	switch visual.Geometry.Type {
	case "resize":
		return "geometry.resize", visual.Geometry.Confidence
	case "crop_possible":
		return "geometry.crop-possible", visual.Geometry.Confidence
	case "rotation_90_possible":
		return "geometry.rotation-90-possible", visual.Geometry.Confidence
	case "canvas_extension_possible":
		return "geometry.canvas-extension-possible", visual.Geometry.Confidence
	case "dimensions_changed":
		return "geometry.change", visual.Geometry.Confidence
	}
	if visual.ChangedPixels == 0 {
		return "encoding-or-metadata.change", 0.95
	}
	if visual.VisualChangeRatio <= 0.05 {
		return "visual.localized-edit", 0.8
	}
	if visual.SSIM >= 0.95 && visual.MeanChannelDelta > 0.01 {
		return "color.adjustment", 0.65
	}
	return "visual.edit", 0.55
}

func changedMetadataKeys(oldMetadata, newMetadata map[string]string) []string {
	keys := map[string]struct{}{}
	for key := range oldMetadata {
		keys[key] = struct{}{}
	}
	for key := range newMetadata {
		keys[key] = struct{}{}
	}
	changed := []string{}
	for key := range keys {
		if oldMetadata[key] != newMetadata[key] {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func hashInferenceFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
