package repository

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

func TestVisualBisectFindsFirstThresholdCrossing(t *testing.T) {
	root := t.TempDir()
	repositoryRoot := filepath.Join(root, "repo")
	repo, err := Init(repositoryRoot, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	baseline := filepath.Join(root, "baseline.png")
	asset := filepath.Join(repositoryRoot, "hero.png")
	writeBisectPNG(t, baseline, 0)
	writeBisectPNG(t, asset, 0)
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	if _, _, err := repo.CreateCommit("baseline", "tester", time.Unix(1, 0)); err != nil {
		t.Fatalf("Commit v1: %v", err)
	}

	writeBisectPNG(t, asset, 1)
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	if _, _, err := repo.CreateCommit("small change", "tester", time.Unix(2, 0)); err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	writeBisectPNG(t, asset, 16)
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add v3: %v", err)
	}
	thirdOID, _, err := repo.CreateCommit("regression", "tester", time.Unix(3, 0))
	if err != nil {
		t.Fatalf("Commit v3: %v", err)
	}

	result, err := repo.VisualBisect(asset, baseline, "change", 0.5, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("VisualBisect: %v", err)
	}
	if !result.Found || result.CommitOID != thirdOID {
		t.Fatalf("result = %#v, want commit %s", result, thirdOID)
	}
	if result.Value != 1 {
		t.Fatalf("change ratio = %v, want 1", result.Value)
	}
}

func writeBisectPNG(t *testing.T, filePath string, changedPixels int) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for index := range 16 {
		pixel := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if index < changedPixels {
			pixel = color.NRGBA{A: 255}
		}
		output.SetNRGBA(index%4, index/4, pixel)
	}
	file, err := os.Create(filePath)
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
