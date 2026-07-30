package repository

import (
	"os/exec"
	"strings"
	"testing"
)

func TestProvenanceJournalRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	journal, err := OpenProvenanceJournal(root)
	if err != nil {
		t.Fatalf("OpenProvenanceJournal: %v", err)
	}
	defer journal.Close()
	contentOID := "sha256:" + strings.Repeat("a", 64)
	recipeOID := "sha256:" + strings.Repeat("b", 64)
	if err := journal.Record(contentOID, recipeOID, "assets/hero.png"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	loaded, found, err := journal.RecipeForContent(contentOID)
	if err != nil {
		t.Fatalf("RecipeForContent: %v", err)
	}
	if !found || loaded != recipeOID {
		t.Fatalf("loaded = %q, found = %v", loaded, found)
	}
}
