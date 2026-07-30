package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pixlog/pixlog/internal/repository"
)

func TestRunCapturedCommandStagesOutputAndDeletion(t *testing.T) {
	root := t.TempDir()
	repo, err := repository.Init(root, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeCLIPNG(t, filepath.Join(root, "source.png"))

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"run", "--redact-args", "--", "cp", "source.png", "output.png"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run cp exit %d, stderr %s", exitCode, stderr.String())
	}
	index, err := repo.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}
	output, exists := index.Entries["output.png"]
	if !exists || output.RecipeOID == "" {
		t.Fatalf("captured output = %#v, exists %v", output, exists)
	}
	if _, exists := index.Entries["source.png"]; exists {
		t.Fatal("unchanged source was unexpectedly staged")
	}
	recipeData, err := repo.Load(output.RecipeOID)
	if err != nil {
		t.Fatalf("Load recipe: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(recipeData, &captured); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	if captured["kind"] != "command-edit" {
		t.Fatalf("recipe kind = %v", captured["kind"])
	}
	tool := captured["tool"].(map[string]any)
	if tool["name"] != "cp" {
		t.Fatalf("tool name = %v", tool["name"])
	}
	command := captured["command"].(map[string]any)
	arguments := command["arguments"].([]any)
	if len(arguments) != 2 || arguments[0] != "<redacted>" || arguments[1] != "<redacted>" {
		t.Fatalf("recorded arguments = %#v", arguments)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"run", "--", "rm", "output.png"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run rm exit %d, stderr %s", exitCode, stderr.String())
	}
	index, err = repo.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex after rm: %v", err)
	}
	if _, exists := index.Entries["output.png"]; exists {
		t.Fatal("deleted output remains in index")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"run", "--", "sh", "-c", "cp source.png failed.png; exit 7"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("failed command exit %d, stderr %s", exitCode, stderr.String())
	}
	index, err = repo.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex after failed command: %v", err)
	}
	if _, exists := index.Entries["failed.png"]; exists {
		t.Fatal("failed command output was unexpectedly staged")
	}
}

func writeCLIPNG(t *testing.T, path string) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			output.SetNRGBA(x, y, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
		}
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
