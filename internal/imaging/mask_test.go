package imaging

import (
	"image"
	"image/color"
	"testing"
)

func TestCheckAllowedMaskFindsPixelsOutsideMask(t *testing.T) {
	oldImage := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	newImage := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	allowedMask := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	deniedMask := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := range 3 {
		for x := range 3 {
			oldImage.SetNRGBA(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
			newImage.SetNRGBA(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	newImage.SetNRGBA(2, 2, color.NRGBA{R: 240, G: 220, B: 200, A: 255})
	allowedMask.SetNRGBA(2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	deniedMask.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	allowed := CheckAllowedMask(oldImage, newImage, allowedMask, DiffOptions{Threshold: 0})
	if allowed.ChangedPixels != 1 || allowed.OutsidePixels != 0 {
		t.Fatalf("allowed mask result = %#v", allowed)
	}
	denied := CheckAllowedMask(oldImage, newImage, deniedMask, DiffOptions{Threshold: 0})
	if denied.OutsidePixels != 1 || len(denied.OutsideRegions) != 1 {
		t.Fatalf("denied mask result = %#v", denied)
	}
	box := denied.OutsideRegions[0].BoundingBox
	if box.X != 2 || box.Y != 2 || box.Width != 1 || box.Height != 1 {
		t.Fatalf("outside box = %#v", box)
	}
}
