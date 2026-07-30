package repository

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGitMediaCleanAndSmudgeRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	imageData := encodeMediaTestPNG(t)
	pointerData, pointer, err := repo.CleanFilter("assets/hero.png", imageData)
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	if bytes.Equal(pointerData, imageData) || pointer.OID == "" || pointer.ManifestOID == "" {
		t.Fatalf("pointer = %#v\n%s", pointer, pointerData)
	}
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	if !store.Has(pointer.OID) || !store.Has(pointer.ManifestOID) {
		t.Fatalf("media objects were not stored: %#v", pointer)
	}
	restored, restoredPointer, err := repo.SmudgeFilter(pointerData)
	if err != nil {
		t.Fatalf("SmudgeFilter: %v", err)
	}
	if restoredPointer != pointer || !bytes.Equal(restored, imageData) {
		t.Fatal("smudge did not restore the exact image bytes")
	}
}

func TestGitFilterProcessStoresPointerInIndex(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	filterCommand := shellQuote(testExecutable) + " -test.run=^TestGitFilterProcessHelper$ --"
	runGitTest(t, root, "config", "filter.pixlog.process", filterCommand)
	runGitTest(t, root, "config", "filter.pixlog.required", "true")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png filter=pixlog -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	imageData := encodeMediaTestPNG(t)
	assetPath := filepath.Join(root, "hero.png")
	if err := os.WriteFile(assetPath, imageData, 0o644); err != nil {
		t.Fatalf("write hero.png: %v", err)
	}
	runGitFilterTestCommand(t, root, "add", ".gitattributes", "hero.png")
	indexData, err := gitBytes(root, "show", ":hero.png")
	if err != nil {
		t.Fatalf("read staged hero.png: %v", err)
	}
	pointer, found, err := ParsePixLogPointer(indexData)
	if err != nil || !found {
		t.Fatalf("staged pointer found = %v, err = %v\n%s", found, err, indexData)
	}
	if pointer.Size != int64(len(imageData)) {
		t.Fatalf("pointer size = %d, want %d", pointer.Size, len(imageData))
	}
	if err := os.Remove(assetPath); err != nil {
		t.Fatalf("remove worktree image: %v", err)
	}
	runGitFilterTestCommand(t, root, "checkout", "--", "hero.png")
	restored, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatalf("read restored image: %v", err)
	}
	if !bytes.Equal(restored, imageData) {
		t.Fatal("Git checkout did not restore the exact image bytes")
	}
}

func TestGitFilterProcessHelper(t *testing.T) {
	if os.Getenv("PIXLOG_TEST_FILTER_PROCESS") != "1" {
		return
	}
	if err := ServeGitFilterProcess("", os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runGitFilterTestCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	context, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(context, "git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "PIXLOG_TEST_FILTER_PROCESS=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func encodeMediaTestPNG(t *testing.T) []byte {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	output.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, output); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return data.Bytes()
}
