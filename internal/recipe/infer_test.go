package recipe

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

func TestInferFilesLabelsLocalizedEditAsInferred(t *testing.T) {
	directory := t.TempDir()
	beforePath := filepath.Join(directory, "before.png")
	afterPath := filepath.Join(directory, "after.png")
	writeInferencePNG(t, beforePath, false)
	writeInferencePNG(t, afterPath, true)
	result, err := InferFiles(beforePath, afterPath, imaging.DiffOptions{Threshold: 8})
	if err != nil {
		t.Fatalf("InferFiles: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(result.Recipe, &document); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	capture := document["capture"].(map[string]any)
	reproducibility := document["reproducibility"].(map[string]any)
	if capture["fidelity"] != "inferred" || reproducibility["status"] != "inferred" || result.Operation != "visual.localized-edit" {
		t.Fatalf("result = %#v, recipe = %#v", result, document)
	}
}

func TestClassifyInferredOperationUsesVisualGeometryLabels(t *testing.T) {
	tests := map[string]string{
		"resize":                    "geometry.resize",
		"crop_possible":             "geometry.crop-possible",
		"rotation_90_possible":      "geometry.rotation-90-possible",
		"canvas_extension_possible": "geometry.canvas-extension-possible",
		"dimensions_changed":        "geometry.change",
	}
	for geometry, expected := range tests {
		operation, _ := classifyInferredOperation("sha256:input", "sha256:output", imaging.VisualDiff{
			Geometry: imaging.GeometryChange{Type: geometry, Confidence: 0.7},
		})
		if operation != expected {
			t.Errorf("geometry %s = %s, want %s", geometry, operation, expected)
		}
	}
}

func writeInferencePNG(t *testing.T, path string, changed bool) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := range 10 {
		for x := range 10 {
			output.SetNRGBA(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	if changed {
		output.SetNRGBA(2, 3, color.NRGBA{R: 220, G: 40, B: 60, A: 255})
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PNG: %v", err)
	}
	if err := png.Encode(file, output); err != nil {
		file.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close PNG: %v", err)
	}
}
