package repository

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitMergeDriverCombinesNonOverlappingPNGChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	base := mergeTestPNG(t, map[image.Point]color.NRGBA{})
	ours := mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 0, Y: 0}: {R: 255, A: 255}})
	theirs := mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 1, Y: 0}: {B: 255, A: 255}})
	basePath := writeMergePointer(t, repo, root, "base", base)
	oursPath := writeMergePointer(t, repo, root, "ours", ours)
	theirsPath := writeMergePointer(t, repo, root, "theirs", theirs)
	if err := repo.MergeDriver(basePath, oursPath, theirsPath, "art/hero.png"); err != nil {
		t.Fatalf("MergeDriver: %v", err)
	}
	mergedPointer, err := os.ReadFile(oursPath)
	if err != nil {
		t.Fatalf("read merged pointer: %v", err)
	}
	mergedData, pointer, err := repo.SmudgeFilter(mergedPointer)
	if err != nil || pointer.OID == "" {
		t.Fatalf("SmudgeFilter pointer = %#v, err = %v", pointer, err)
	}
	merged, err := png.Decode(bytes.NewReader(mergedData))
	if err != nil {
		t.Fatalf("decode merged PNG: %v", err)
	}
	if got := color.NRGBAModel.Convert(merged.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("ours pixel = %#v", got)
	}
	if got := color.NRGBAModel.Convert(merged.At(1, 0)).(color.NRGBA); got != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("theirs pixel = %#v", got)
	}
}

func TestGitMergeDriverRejectsOverlappingPNGChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	base := mergeTestPNG(t, map[image.Point]color.NRGBA{})
	ours := mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 0, Y: 0}: {R: 255, A: 255}})
	theirs := mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 0, Y: 0}: {B: 255, A: 255}})
	basePath := writeMergePointer(t, repo, root, "base", base)
	oursPath := writeMergePointer(t, repo, root, "ours", ours)
	theirsPath := writeMergePointer(t, repo, root, "theirs", theirs)
	originalOurs, err := os.ReadFile(oursPath)
	if err != nil {
		t.Fatalf("read ours pointer: %v", err)
	}
	if err := repo.MergeDriver(basePath, oursPath, theirsPath, "art/hero.png"); !errors.Is(err, ErrGitImageConflict) {
		t.Fatalf("MergeDriver error = %v", err)
	}
	currentOurs, err := os.ReadFile(oursPath)
	if err != nil {
		t.Fatalf("read ours pointer after conflict: %v", err)
	}
	if !bytes.Equal(currentOurs, originalOurs) {
		t.Fatal("conflicting merge modified the ours pointer")
	}
}

func writeMergePointer(t *testing.T, repo *GitRepository, root, name string, data []byte) string {
	t.Helper()
	pointerData, pointer, err := repo.CleanFilter(name+".png", data)
	if err != nil || pointer.OID == "" {
		t.Fatalf("CleanFilter pointer = %#v, err = %v", pointer, err)
	}
	path := filepath.Join(root, name+".pointer")
	if err := os.WriteFile(path, pointerData, 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	return path
}

func mergeTestPNG(t *testing.T, pixels map[image.Point]color.NRGBA) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for point, pixel := range pixels {
		value.SetNRGBA(point.X, point.Y, pixel)
	}
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}
