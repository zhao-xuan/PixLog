package repository

import (
	"image/color"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveCommitSupportsTagsAndRejectsUnsafeRefs(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	asset := filepath.Join(root, "hero.png")
	writeTestPNG(t, asset, color.NRGBA{R: 100, A: 255})
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitOID, _, err := repo.CreateCommit("release", "tester", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("CreateCommit: %v", err)
	}
	if _, err := repo.CreateTag("v1.0.0", "HEAD"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	resolvedOID, _, err := repo.ResolveCommit("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveCommit tag: %v", err)
	}
	if resolvedOID != commitOID {
		t.Fatalf("resolved tag = %s, want %s", resolvedOID, commitOID)
	}
	if _, _, err := repo.ResolveCommit("../../config.json"); err == nil {
		t.Fatal("unsafe revision unexpectedly resolved")
	}
}
