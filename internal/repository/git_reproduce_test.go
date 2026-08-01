package repository

import (
	"encoding/json"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/recipe"
)

func TestGitReproductionPlanRequiresCapturedSourceState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("input\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitTest(t, root, "add", "source.txt")
	runGitTest(t, root, "commit", "--quiet", "-m", "source state")
	sourceContext, err := DiscoverGitContext(root)
	if err != nil {
		t.Fatalf("DiscoverGitContext: %v", err)
	}
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	recipeDocument := map[string]any{
		"schema": recipe.Schema,
		"kind":   "command-edit",
		"command": map[string]any{
			"executable":        "generator",
			"arguments":         []string{"--output", "hero.png"},
			"working_directory": ".",
		},
		"source_control": sourceContext,
	}
	recipeData, err := json.Marshal(recipeDocument)
	if err != nil {
		t.Fatalf("marshal recipe: %v", err)
	}
	normalized, err := recipe.Normalize(recipeData)
	if err != nil {
		t.Fatalf("normalize recipe: %v", err)
	}
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	recipeOID, err := store.Put(normalized)
	if err != nil {
		t.Fatalf("store recipe: %v", err)
	}
	imageData := mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 0, Y: 0}: {R: 255, A: 255}})
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	if err := journal.Record(hashBytes(imageData), recipeOID, "hero.png"); err != nil {
		t.Fatalf("record provenance: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}
	pointerData, pointer, err := repo.CleanFilter("hero.png", imageData)
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	if pointer.RecipeOID != recipeOID {
		t.Fatalf("pointer recipe = %q, want %q", pointer.RecipeOID, recipeOID)
	}
	if err := os.WriteFile(filepath.Join(root, "hero.png"), pointerData, 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "generated output")
	plan, err := repo.PlanReproduction("HEAD", filepath.Join(root, "hero.png"))
	if err != nil {
		t.Fatalf("PlanReproduction: %v", err)
	}
	if plan.Command == nil || plan.Command.Executable != "generator" || plan.RecipeOID != recipeOID {
		t.Fatalf("plan = %#v", plan)
	}
	if err := repo.ValidateReproduction(plan); err == nil {
		t.Fatal("ValidateReproduction accepted the output commit instead of the captured source state")
	}
	runGitTest(t, root, "checkout", "--quiet", sourceContext.HeadOID)
	if err := repo.ValidateReproduction(plan); err != nil {
		t.Fatalf("ValidateReproduction at source state: %v", err)
	}
}

func TestValidateRequestReproductionRejectsRedactedPathAndNonOriginBase(t *testing.T) {
	repo := &GitRepository{}
	plan := ReproductionPlan{
		RecipeOID: "sha256:" + strings.Repeat("a", 64),
		Request: &ReproductionRequest{
			Method: "POST", Path: "/v1/images?api_key=%5BREDACTED%5D",
			RequestOID: "sha256:" + strings.Repeat("b", 64), Fidelity: recipe.FidelityExactRequest,
		},
	}
	if _, err := repo.ValidateRequestReproduction(plan, "https://provider.example"); err == nil {
		t.Fatal("ValidateRequestReproduction accepted a redacted query")
	}
	plan.Request.Path = "/v1/images"
	if _, err := repo.ValidateRequestReproduction(plan, "https://provider.example/api"); err == nil {
		t.Fatal("ValidateRequestReproduction accepted a base URL with a path")
	}
}

func TestReproductionRequestFromOperationsSkipsUnsupportedMethods(t *testing.T) {
	operations := []struct {
		Fidelity   recipe.CaptureFidelity `json:"fidelity"`
		Normalized json.RawMessage        `json:"normalized"`
	}{
		{Fidelity: recipe.FidelityExactRequest, Normalized: json.RawMessage(`{"platform":"comfyui","method":"GET","path":"/history","request_oid":"sha256:` + strings.Repeat("a", 64) + `"}`)},
		{Fidelity: recipe.FidelityExactRequest, Normalized: json.RawMessage(`{"platform":"comfyui","method":"POST","path":"/prompt","request_oid":"sha256:` + strings.Repeat("b", 64) + `"}`)},
	}
	request := reproductionRequestFromOperations("comfyui-proxy", recipe.FidelityExactRequest, operations)
	if request == nil || request.Method != "POST" || request.Path != "/prompt" {
		t.Fatalf("request = %#v", request)
	}
}
