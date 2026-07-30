package imaging

import (
	"fmt"
	"image"
	"image/color"
	"io"
)

type MaskCheck struct {
	ChangedPixels  int             `json:"changed_pixels"`
	OutsidePixels  int             `json:"outside_pixels"`
	OutsideRatio   float64         `json:"outside_ratio"`
	OutsideRegions []ChangedRegion `json:"outside_regions"`
}

func CheckAllowedMaskReaders(oldReader, newReader, maskReader io.Reader, options DiffOptions) (MaskCheck, error) {
	oldImage, _, err := image.Decode(oldReader)
	if err != nil {
		return MaskCheck{}, fmt.Errorf("decode old image: %w: %w", ErrUnsupportedVisualFormat, err)
	}
	newImage, _, err := image.Decode(newReader)
	if err != nil {
		return MaskCheck{}, fmt.Errorf("decode new image: %w: %w", ErrUnsupportedVisualFormat, err)
	}
	maskImage, _, err := image.Decode(maskReader)
	if err != nil {
		return MaskCheck{}, fmt.Errorf("decode allowed-change mask: %w", err)
	}
	return CheckAllowedMask(oldImage, newImage, maskImage, options), nil
}

func CheckAllowedMask(oldImage, newImage, maskImage image.Image, options DiffOptions) MaskCheck {
	if options.MaxDimension <= 0 {
		options.MaxDimension = 2048
	}
	oldBounds := oldImage.Bounds()
	newBounds := newImage.Bounds()
	maskBounds := maskImage.Bounds()
	oldDimensions := Dimensions{Width: oldBounds.Dx(), Height: oldBounds.Dy()}
	newDimensions := Dimensions{Width: newBounds.Dx(), Height: newBounds.Dy()}
	if oldDimensions.Width == 0 || oldDimensions.Height == 0 || newDimensions.Width == 0 || newDimensions.Height == 0 || maskBounds.Dx() == 0 || maskBounds.Dy() == 0 {
		return MaskCheck{OutsideRegions: []ChangedRegion{}}
	}

	comparisonWidth, comparisonHeight := comparisonSize(newDimensions, options.MaxDimension)
	outside := make([]bool, comparisonWidth*comparisonHeight)
	result := MaskCheck{OutsideRegions: []ChangedRegion{}}
	for y := range comparisonHeight {
		for x := range comparisonWidth {
			oldX := oldBounds.Min.X + sampleCoordinate(x, comparisonWidth, oldDimensions.Width)
			oldY := oldBounds.Min.Y + sampleCoordinate(y, comparisonHeight, oldDimensions.Height)
			newX := newBounds.Min.X + sampleCoordinate(x, comparisonWidth, newDimensions.Width)
			newY := newBounds.Min.Y + sampleCoordinate(y, comparisonHeight, newDimensions.Height)
			oldPixel := color.NRGBAModel.Convert(oldImage.At(oldX, oldY)).(color.NRGBA)
			newPixel := color.NRGBAModel.Convert(newImage.At(newX, newY)).(color.NRGBA)
			maximumDelta := max(
				absInt(int(oldPixel.R)-int(newPixel.R)),
				absInt(int(oldPixel.G)-int(newPixel.G)),
				absInt(int(oldPixel.B)-int(newPixel.B)),
				absInt(int(oldPixel.A)-int(newPixel.A)),
			)
			if maximumDelta <= int(options.Threshold) {
				continue
			}
			result.ChangedPixels++
			maskX := maskBounds.Min.X + sampleCoordinate(x, comparisonWidth, maskBounds.Dx())
			maskY := maskBounds.Min.Y + sampleCoordinate(y, comparisonHeight, maskBounds.Dy())
			maskPixel := color.NRGBAModel.Convert(maskImage.At(maskX, maskY)).(color.NRGBA)
			brightness := (int(maskPixel.R) + int(maskPixel.G) + int(maskPixel.B)) / 3
			if maskPixel.A < 128 || brightness < 128 {
				outside[y*comparisonWidth+x] = true
				result.OutsidePixels++
			}
		}
	}
	if result.ChangedPixels > 0 {
		result.OutsideRatio = float64(result.OutsidePixels) / float64(result.ChangedPixels)
	}
	result.OutsideRegions = connectedRegions(outside, comparisonWidth, comparisonHeight, newDimensions)
	return result
}
