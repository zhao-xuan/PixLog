package imaging

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"sort"
)

var ErrUnsupportedVisualFormat = errors.New("image format does not have a built-in visual decoder")

type DiffOptions struct {
	Threshold      uint8
	MaxDimension   int
	IncludePreview bool
}

type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type GeometryChange struct {
	Type       string     `json:"type"`
	Confidence float64    `json:"confidence"`
	Old        Dimensions `json:"old"`
	New        Dimensions `json:"new"`
}

type BoundingBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ChangedRegion struct {
	BoundingBox BoundingBox `json:"bbox"`
	Pixels      int         `json:"pixels"`
	Ratio       float64     `json:"ratio"`
}

type VisualDiff struct {
	Comparable        bool            `json:"comparable"`
	Geometry          GeometryChange  `json:"geometry"`
	ComparedPixels    int             `json:"compared_pixels"`
	ChangedPixels     int             `json:"changed_pixels"`
	VisualChangeRatio float64         `json:"visual_change_ratio"`
	MeanChannelDelta  float64         `json:"mean_channel_delta"`
	RMSE              float64         `json:"rmse"`
	SSIM              float64         `json:"ssim"`
	Regions           []ChangedRegion `json:"regions"`
	heatmap           *image.NRGBA
}

func CompareReaders(oldReader, newReader io.Reader, options DiffOptions) (VisualDiff, error) {
	oldImage, _, err := image.Decode(oldReader)
	if err != nil {
		return VisualDiff{}, fmt.Errorf("decode old image: %w: %w", ErrUnsupportedVisualFormat, err)
	}
	newImage, _, err := image.Decode(newReader)
	if err != nil {
		return VisualDiff{}, fmt.Errorf("decode new image: %w: %w", ErrUnsupportedVisualFormat, err)
	}
	return Compare(oldImage, newImage, options), nil
}

func Compare(oldImage, newImage image.Image, options DiffOptions) VisualDiff {
	if options.MaxDimension <= 0 {
		options.MaxDimension = 2048
	}
	oldBounds := oldImage.Bounds()
	newBounds := newImage.Bounds()
	oldDimensions := Dimensions{Width: oldBounds.Dx(), Height: oldBounds.Dy()}
	newDimensions := Dimensions{Width: newBounds.Dx(), Height: newBounds.Dy()}
	geometry := classifyGeometry(oldDimensions, newDimensions)
	if oldDimensions.Width == 0 || oldDimensions.Height == 0 || newDimensions.Width == 0 || newDimensions.Height == 0 {
		return VisualDiff{Comparable: false, Geometry: geometry, Regions: []ChangedRegion{}}
	}

	comparisonWidth, comparisonHeight := comparisonSize(newDimensions, options.MaxDimension)
	totalPixels := comparisonWidth * comparisonHeight
	changed := make([]bool, totalPixels)
	heatmap := image.NewNRGBA(image.Rect(0, 0, comparisonWidth, comparisonHeight))
	var changedPixels int
	var squaredError, absoluteError float64
	var luminanceOld, luminanceNew, luminanceOldSquared, luminanceNewSquared, luminanceProduct float64

	for y := range comparisonHeight {
		for x := range comparisonWidth {
			oldX := oldBounds.Min.X + sampleCoordinate(x, comparisonWidth, oldDimensions.Width)
			oldY := oldBounds.Min.Y + sampleCoordinate(y, comparisonHeight, oldDimensions.Height)
			newX := newBounds.Min.X + sampleCoordinate(x, comparisonWidth, newDimensions.Width)
			newY := newBounds.Min.Y + sampleCoordinate(y, comparisonHeight, newDimensions.Height)
			oldPixel := color.NRGBAModel.Convert(oldImage.At(oldX, oldY)).(color.NRGBA)
			newPixel := color.NRGBAModel.Convert(newImage.At(newX, newY)).(color.NRGBA)
			deltas := [4]int{
				absInt(int(oldPixel.R) - int(newPixel.R)),
				absInt(int(oldPixel.G) - int(newPixel.G)),
				absInt(int(oldPixel.B) - int(newPixel.B)),
				absInt(int(oldPixel.A) - int(newPixel.A)),
			}
			maximumDelta := max(deltas[0], deltas[1], deltas[2], deltas[3])
			index := y*comparisonWidth + x
			if maximumDelta > int(options.Threshold) {
				changed[index] = true
				changedPixels++
				heatmap.SetNRGBA(x, y, color.NRGBA{R: 255, G: uint8(maximumDelta / 4), B: 24, A: 255})
			} else {
				gray := uint8((uint16(newPixel.R) + uint16(newPixel.G) + uint16(newPixel.B)) / 6)
				heatmap.SetNRGBA(x, y, color.NRGBA{R: gray, G: gray, B: gray, A: 255})
			}
			for _, delta := range deltas {
				absoluteError += float64(delta)
				squaredError += float64(delta * delta)
			}
			oldLuma := luminance(oldPixel)
			newLuma := luminance(newPixel)
			luminanceOld += oldLuma
			luminanceNew += newLuma
			luminanceOldSquared += oldLuma * oldLuma
			luminanceNewSquared += newLuma * newLuma
			luminanceProduct += oldLuma * newLuma
		}
	}

	channels := float64(totalPixels * 4)
	result := VisualDiff{
		Comparable:        true,
		Geometry:          geometry,
		ComparedPixels:    totalPixels,
		ChangedPixels:     changedPixels,
		VisualChangeRatio: float64(changedPixels) / float64(totalPixels),
		MeanChannelDelta:  absoluteError / channels / 255,
		RMSE:              math.Sqrt(squaredError/channels) / 255,
		SSIM:              calculateSSIM(totalPixels, luminanceOld, luminanceNew, luminanceOldSquared, luminanceNewSquared, luminanceProduct),
		heatmap:           heatmap,
	}
	result.Regions = connectedRegions(changed, comparisonWidth, comparisonHeight, newDimensions)
	return result
}

func WriteHeatmap(writer io.Writer, diff VisualDiff) error {
	if diff.heatmap == nil {
		return errors.New("visual diff does not contain a heatmap")
	}
	if err := png.Encode(writer, diff.heatmap); err != nil {
		return fmt.Errorf("encode heatmap: %w", err)
	}
	return nil
}

func classifyGeometry(oldDimensions, newDimensions Dimensions) GeometryChange {
	change := GeometryChange{Type: "dimensions_changed", Confidence: 0.7, Old: oldDimensions, New: newDimensions}
	if oldDimensions == newDimensions {
		change.Type = "identity"
		change.Confidence = 1
		return change
	}
	if oldDimensions.Width == 0 || oldDimensions.Height == 0 || newDimensions.Width == 0 || newDimensions.Height == 0 {
		change.Confidence = 0
		return change
	}
	oldRatio := float64(oldDimensions.Width) / float64(oldDimensions.Height)
	newRatio := float64(newDimensions.Width) / float64(newDimensions.Height)
	if math.Abs(oldRatio-newRatio)/oldRatio < 0.002 {
		change.Type = "resize"
		change.Confidence = 0.99
		return change
	}
	if oldDimensions.Width == newDimensions.Height && oldDimensions.Height == newDimensions.Width {
		change.Type = "rotation_90_possible"
		change.Confidence = 0.65
		return change
	}
	if newDimensions.Width <= oldDimensions.Width && newDimensions.Height <= oldDimensions.Height {
		change.Type = "crop_possible"
		change.Confidence = 0.55
		return change
	}
	if newDimensions.Width >= oldDimensions.Width && newDimensions.Height >= oldDimensions.Height {
		change.Type = "canvas_extension_possible"
		change.Confidence = 0.55
	}
	return change
}

func comparisonSize(dimensions Dimensions, maximum int) (int, int) {
	width, height := dimensions.Width, dimensions.Height
	largest := max(width, height)
	if largest <= maximum {
		return width, height
	}
	scale := float64(maximum) / float64(largest)
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

func sampleCoordinate(coordinate, sampleSize, sourceSize int) int {
	if sampleSize <= 1 || sourceSize <= 1 {
		return 0
	}
	return coordinate * (sourceSize - 1) / (sampleSize - 1)
}

func luminance(pixel color.NRGBA) float64 {
	return 0.299*float64(pixel.R) + 0.587*float64(pixel.G) + 0.114*float64(pixel.B)
}

func calculateSSIM(count int, sumOld, sumNew, sumOldSquared, sumNewSquared, sumProduct float64) float64 {
	if count <= 1 {
		if sumOld == sumNew {
			return 1
		}
		return 0
	}
	n := float64(count)
	meanOld, meanNew := sumOld/n, sumNew/n
	varianceOld := maxFloat(0, (sumOldSquared-n*meanOld*meanOld)/(n-1))
	varianceNew := maxFloat(0, (sumNewSquared-n*meanNew*meanNew)/(n-1))
	covariance := (sumProduct - n*meanOld*meanNew) / (n - 1)
	c1 := math.Pow(0.01*255, 2)
	c2 := math.Pow(0.03*255, 2)
	numerator := (2*meanOld*meanNew + c1) * (2*covariance + c2)
	denominator := (meanOld*meanOld + meanNew*meanNew + c1) * (varianceOld + varianceNew + c2)
	if denominator == 0 {
		return 1
	}
	return maxFloat(-1, minFloat(1, numerator/denominator))
}

func connectedRegions(changed []bool, width, height int, output Dimensions) []ChangedRegion {
	visited := make([]bool, len(changed))
	regions := []ChangedRegion{}
	queue := make([]int, 0, 256)
	for start, isChanged := range changed {
		if !isChanged || visited[start] {
			continue
		}
		visited[start] = true
		queue = append(queue[:0], start)
		minX, maxX := start%width, start%width
		minY, maxY := start/width, start/width
		pixels := 0
		for len(queue) > 0 {
			current := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			x, y := current%width, current/width
			pixels++
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
			neighbors := [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}}
			for _, neighbor := range neighbors {
				nextX, nextY := neighbor[0], neighbor[1]
				if nextX < 0 || nextX >= width || nextY < 0 || nextY >= height {
					continue
				}
				next := nextY*width + nextX
				if changed[next] && !visited[next] {
					visited[next] = true
					queue = append(queue, next)
				}
			}
		}
		boxX := minX * output.Width / width
		boxY := minY * output.Height / height
		boxRight := min(output.Width, (maxX+1)*output.Width/width)
		boxBottom := min(output.Height, (maxY+1)*output.Height/height)
		regions = append(regions, ChangedRegion{
			BoundingBox: BoundingBox{X: boxX, Y: boxY, Width: max(1, boxRight-boxX), Height: max(1, boxBottom-boxY)},
			Pixels:      pixels,
			Ratio:       float64(pixels) / float64(width*height),
		})
	}
	sort.Slice(regions, func(left, right int) bool { return regions[left].Pixels > regions[right].Pixels })
	if len(regions) > 32 {
		regions = regions[:32]
	}
	return regions
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
