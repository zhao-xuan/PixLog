package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/repository"
)

func TestRecipeInferAndHistoryImportThroughGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	runCLIGit(t, root, "config", "user.name", "PixLog Test")
	runCLIGit(t, root, "config", "user.email", "pixlog@example.test")
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
		t.Fatalf("init exit %d: %s", exitCode, stderr.String())
	}
	writeGitCLIImage(t, filepath.Join(root, "before.png"), imagePointNone)
	writeGitCLIImage(t, filepath.Join(root, "after.png"), imagePointChanged)
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"recipe", "infer", "before.png", "after.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("recipe infer exit %d: %s", exitCode, stderr.String())
	}
	repo, err := repository.OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	_, inferredData, err := repo.RecipeData("", "after.png")
	if err != nil {
		t.Fatalf("inferred RecipeData: %v", err)
	}
	var inferred map[string]any
	if err := json.Unmarshal(inferredData, &inferred); err != nil {
		t.Fatalf("decode inferred recipe: %v", err)
	}
	if inferred["capture"].(map[string]any)["fidelity"] != "inferred" {
		t.Fatalf("inferred recipe = %#v", inferred)
	}

	if err := os.WriteFile("photoshop-history.txt", []byte("Open\nAuthorization: Bearer history-secret\nCurves\n"), 0o600); err != nil {
		t.Fatalf("write history: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"capture", "history", "photoshop-history.txt", "after.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("capture history exit %d: %s", exitCode, stderr.String())
	}
	_, historyData, err := repo.RecipeData("", "after.png")
	if err != nil {
		t.Fatalf("history RecipeData: %v", err)
	}
	var history map[string]any
	if err := json.Unmarshal(historyData, &history); err != nil {
		t.Fatalf("decode history recipe: %v", err)
	}
	if history["capture"].(map[string]any)["fidelity"] != "application-history" {
		t.Fatalf("history recipe = %#v", history)
	}
	payloadOID := history["vendor"].(map[string]any)["raw_payload_oid"].(string)
	store, err := repository.OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	payload, err := store.Get(payloadOID)
	if err != nil {
		t.Fatalf("Get history payload: %v", err)
	}
	if strings.Contains(string(payload), "history-secret") || !strings.Contains(string(payload), "[REDACTED]") {
		t.Fatalf("history payload = %q", payload)
	}
}

var (
	imagePointNone    = image.Point{X: -1, Y: -1}
	imagePointChanged = image.Point{X: 1, Y: 1}
)
