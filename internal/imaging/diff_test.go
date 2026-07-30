package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestCompareFindsChangedRegion(t *testing.T) {
	oldImage := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	newImage := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fill(oldImage, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	fill(newImage, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	newImage.SetNRGBA(2, 1, color.NRGBA{A: 255})
	newImage.SetNRGBA(2, 2, color.NRGBA{A: 255})

	diff := Compare(oldImage, newImage, DiffOptions{})
	if diff.ChangedPixels != 2 {
		t.Fatalf("ChangedPixels = %d, want 2", diff.ChangedPixels)
	}
	if diff.VisualChangeRatio != 0.125 {
		t.Fatalf("VisualChangeRatio = %f, want 0.125", diff.VisualChangeRatio)
	}
	if len(diff.Regions) != 1 {
		t.Fatalf("len(Regions) = %d, want 1", len(diff.Regions))
	}
	want := BoundingBox{X: 2, Y: 1, Width: 1, Height: 2}
	if diff.Regions[0].BoundingBox != want {
		t.Fatalf("BoundingBox = %#v, want %#v", diff.Regions[0].BoundingBox, want)
	}
	var heatmap bytes.Buffer
	if err := WriteHeatmap(&heatmap, diff); err != nil {
		t.Fatalf("WriteHeatmap: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(heatmap.Bytes())); err != nil {
		t.Fatalf("decode heatmap: %v", err)
	}
}

func TestCompareClassifiesResize(t *testing.T) {
	oldImage := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	newImage := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	fill(oldImage, color.NRGBA{R: 12, G: 80, B: 140, A: 255})
	fill(newImage, color.NRGBA{R: 12, G: 80, B: 140, A: 255})

	diff := Compare(oldImage, newImage, DiffOptions{})
	if diff.Geometry.Type != "resize" {
		t.Fatalf("Geometry.Type = %q, want resize", diff.Geometry.Type)
	}
	if diff.VisualChangeRatio != 0 {
		t.Fatalf("VisualChangeRatio = %f, want 0", diff.VisualChangeRatio)
	}
}

func fill(target *image.NRGBA, value color.NRGBA) {
	for y := target.Bounds().Min.Y; y < target.Bounds().Max.Y; y++ {
		for x := target.Bounds().Min.X; x < target.Bounds().Max.X; x++ {
			target.SetNRGBA(x, y, value)
		}
	}
}
