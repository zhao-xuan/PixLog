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

	"github.com/pixlog/pixlog/internal/recipe"
)

func TestProvenanceGraphTraversesAndVerifiesRecipeReferences(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	parentOID, err := store.Put([]byte("reference asset"))
	if err != nil {
		t.Fatalf("Put parent: %v", err)
	}
	rawOID, err := store.Put([]byte(`{"request":"captured"}`))
	if err != nil {
		t.Fatalf("Put raw payload: %v", err)
	}
	missingModelOID := "sha256:" + strings.Repeat("d", 64)
	recipeDocument := map[string]any{
		"schema":  recipe.Schema,
		"kind":    "ai-generation",
		"parents": []any{map[string]any{"asset": parentOID, "role": "reference-image"}},
		"model":   map[string]any{"digest": missingModelOID},
		"vendor":  map[string]any{"raw_payload_oid": rawOID},
	}
	recipeData, _ := json.Marshal(recipeDocument)
	recipeData, err = recipe.Normalize(recipeData)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	recipeOID, err := store.Put(recipeData)
	if err != nil {
		t.Fatalf("Put recipe: %v", err)
	}
	imageData := mergeTestPNG(t, map[image.Point]color.NRGBA{})
	contentOID, err := store.Put(imageData)
	if err != nil {
		t.Fatalf("Put content: %v", err)
	}
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	if err := journal.Record(contentOID, recipeOID, "hero.png"); err != nil {
		journal.Close()
		t.Fatalf("Record: %v", err)
	}
	journal.Close()
	pointerData, _, err := repo.CleanFilter("hero.png", imageData)
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hero.png"), pointerData, 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "generate hero")

	graph, err := repo.ProvenanceGraph(filepath.Join(root, "hero.png"), false)
	if err != nil {
		t.Fatalf("ProvenanceGraph: %v", err)
	}
	if len(graph.Missing) != 1 || graph.Missing[0] != missingModelOID {
		t.Fatalf("missing = %#v", graph.Missing)
	}
	nodes := map[string]ProvenanceGraphNode{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	if !nodes[parentOID].Verified || !nodes[rawOID].Verified || nodes[missingModelOID].Exists {
		t.Fatalf("nodes = %#v", nodes)
	}
}
