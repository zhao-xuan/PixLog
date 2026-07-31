package capture

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

func TestFinalizeSessionCreatesAndStagesCanonicalRecipe(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCaptureGit(t, root, "init", "--quiet")
	runCaptureGit(t, root, "config", "user.name", "PixLog Test")
	runCaptureGit(t, root, "config", "user.email", "pixlog@example.test")
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	t.Setenv("PIXLOG_CAPTURE_TEST_FILTER", "1")
	filterCommand := shellQuoteCapture(testExecutable) + " -test.run=^TestFinalizeFilterHelper$ --"
	runCaptureGit(t, root, "config", "filter.pixlog.process", filterCommand)
	runCaptureGit(t, root, "config", "filter.pixlog.required", "true")
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.png filter=pixlog -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	assetPath := filepath.Join(root, "hero.png")
	writeFinalizePNG(t, assetPath)
	journal, err := repository.OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	session, err := journal.StartCaptureSession(repository.CaptureSession{
		Adapter: "photoshop-uxp", AdapterVersion: "0.1.0", Application: "Adobe Photoshop",
	})
	if err != nil {
		journal.Close()
		t.Fatalf("StartCaptureSession: %v", err)
	}
	rawOID, err := repository.OpenGitMediaStore(root)
	if err != nil {
		journal.Close()
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	descriptorOID, err := rawOID.Put([]byte(`{"_obj":"curves"}`))
	if err != nil {
		journal.Close()
		t.Fatalf("Put descriptor: %v", err)
	}
	if _, err := journal.AppendCaptureEvent(repository.CaptureEvent{
		SessionID: session.ID, EventType: "curves", Fidelity: recipe.FidelityExactCommand,
		RawPayloadOID: descriptorOID, Normalized: json.RawMessage(`{"operation":"color.curves"}`),
	}); err != nil {
		journal.Close()
		t.Fatalf("AppendCaptureEvent: %v", err)
	}
	journal.Close()

	result, err := FinalizeSession(root, session.ID, assetPath, "")
	if err != nil {
		t.Fatalf("FinalizeSession: %v", err)
	}
	if result.RecipeOID == "" || result.ContentOID == "" {
		t.Fatalf("result = %#v", result)
	}
	repo, err := repository.OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	_, recipeData, err := repo.RecipeData("", assetPath)
	if err != nil {
		t.Fatalf("RecipeData: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(recipeData, &document); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	captureData := document["capture"].(map[string]any)
	if document["kind"] != "image-edit" || captureData["fidelity"] != "exact-command" {
		t.Fatalf("recipe = %#v", document)
	}
}

func TestFinalizeFilterHelper(t *testing.T) {
	if os.Getenv("PIXLOG_CAPTURE_TEST_FILTER") != "1" {
		return
	}
	if err := repository.ServeGitFilterProcess("", os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func shellQuoteCapture(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeFinalizePNG(t *testing.T, path string) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	output.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 255})
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
