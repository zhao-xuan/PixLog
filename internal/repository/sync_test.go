package repository

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPushCloneAndPull(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	remoteRoot := filepath.Join(root, "remote.pixlog")
	cloneRoot := filepath.Join(root, "clone")

	source, err := Init(sourceRoot, false)
	if err != nil {
		t.Fatalf("Init source: %v", err)
	}
	writeTestPNG(t, filepath.Join(sourceRoot, "hero.png"), color.NRGBA{R: 200, A: 255})
	if _, err := source.Add([]string{filepath.Join(sourceRoot, "hero.png")}, ""); err != nil {
		t.Fatalf("Add source: %v", err)
	}
	if _, _, err := source.CreateCommit("initial", "test", time.Unix(1, 0)); err != nil {
		t.Fatalf("Commit source: %v", err)
	}
	if _, err := Init(remoteRoot, true); err != nil {
		t.Fatalf("Init remote: %v", err)
	}
	if err := source.AddRemote("origin", remoteRoot); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	push, err := source.Push("origin")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if push.CopiedObjects == 0 {
		t.Fatal("Push copied no objects")
	}

	clone, result, err := Clone(remoteRoot, cloneRoot)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if result.LocalHead == "" {
		t.Fatal("Clone did not check out a commit")
	}
	sourceImage, _ := os.ReadFile(filepath.Join(sourceRoot, "hero.png"))
	clonedImage, _ := os.ReadFile(filepath.Join(cloneRoot, "hero.png"))
	if !bytes.Equal(sourceImage, clonedImage) {
		t.Fatal("cloned image does not match source")
	}

	writeTestPNG(t, filepath.Join(cloneRoot, "hero.png"), color.NRGBA{G: 180, A: 255})
	if _, err := clone.Add([]string{filepath.Join(cloneRoot, "hero.png")}, ""); err != nil {
		t.Fatalf("Add clone: %v", err)
	}
	if _, _, err := clone.CreateCommit("green", "test", time.Unix(2, 0)); err != nil {
		t.Fatalf("Commit clone: %v", err)
	}
	if _, err := clone.Push("origin"); err != nil {
		t.Fatalf("Push clone: %v", err)
	}
	if _, err := source.Pull("origin"); err != nil {
		t.Fatalf("Pull source: %v", err)
	}
	updatedSource, _ := os.ReadFile(filepath.Join(sourceRoot, "hero.png"))
	updatedClone, _ := os.ReadFile(filepath.Join(cloneRoot, "hero.png"))
	if !bytes.Equal(updatedSource, updatedClone) {
		t.Fatal("pulled image does not match remote version")
	}
	verification, err := source.VerifyObjects()
	if err != nil {
		t.Fatalf("VerifyObjects: %v", err)
	}
	if len(verification.Corrupt) != 0 {
		t.Fatalf("corrupt objects: %v", verification.Corrupt)
	}
}

func writeTestPNG(t *testing.T, path string, value color.NRGBA) {
	t.Helper()
	target := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := range 3 {
		for x := range 3 {
			target.SetNRGBA(x, y, value)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PNG: %v", err)
	}
	if err := png.Encode(file, target); err != nil {
		file.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close PNG: %v", err)
	}
}
