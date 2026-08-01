package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/repository"
)

func TestRunCapturedCommandStagesPointerAndDeletion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	writeCLIPNG(t, filepath.Join(root, "source.png"))
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".pixlog/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	runCLIGit(t, root, "init", "--quiet")
	runCLIGit(t, root, "config", "user.name", "PixLog Test")
	runCLIGit(t, root, "config", "user.email", "pixlog@example.test")
	runCLIGit(t, root, "add", "source.png", ".gitignore")
	runCLIGit(t, root, "commit", "--quiet", "-m", "initial")
	gitHead := strings.TrimSpace(runCLIGit(t, root, "rev-parse", "HEAD"))
	t.Setenv("PIXLOG_EXECUTABLE", writeCLIFilterHelper(t))

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"init", "--git"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("init --git exit %d, stderr %s", exitCode, stderr.String())
	}
	gitRepo, err := repository.OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode := Run([]string{"run", "--redact-args", "--", "cp", "source.png", "output.png"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run cp exit %d, stderr %s", exitCode, stderr.String())
	}
	entries, err := gitRepo.CaptureEntries()
	if err != nil {
		t.Fatalf("CaptureEntries: %v", err)
	}
	output, exists := entries["output.png"]
	if !exists || output.RecipeOID == "" {
		t.Fatalf("captured output = %#v, exists %v", output, exists)
	}
	staged := runCLIGit(t, root, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "output.png") {
		t.Fatalf("output was not staged:\n%s", staged)
	}
	_, recipeData, err := gitRepo.RecipeData("", filepath.Join(root, "output.png"))
	if err != nil {
		t.Fatalf("RecipeData: %v", err)
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
	sourceControl := captured["source_control"].(map[string]any)
	if sourceControl["provider"] != "git" || sourceControl["head_oid"] != gitHead || sourceControl["index_tree_oid"] == "" {
		t.Fatalf("source control = %#v", sourceControl)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"run", "--", "rm", "output.png"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run rm exit %d, stderr %s", exitCode, stderr.String())
	}
	entries, err = gitRepo.CaptureEntries()
	if err != nil {
		t.Fatalf("CaptureEntries after rm: %v", err)
	}
	if _, exists := entries["output.png"]; exists {
		t.Fatal("deleted output remains in Git index")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"run", "--", "sh", "-c", "cp source.png failed.png; exit 7"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("failed command exit %d, stderr %s", exitCode, stderr.String())
	}
	entries, err = gitRepo.CaptureEntries()
	if err != nil {
		t.Fatalf("CaptureEntries after failed command: %v", err)
	}
	if _, exists := entries["failed.png"]; exists {
		t.Fatal("failed command output was unexpectedly staged")
	}
}

func runCLIGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
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
