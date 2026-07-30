package repository

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pixlog/pixlog/internal/imaging"
)

func TestHistoryRestoreBranchAndBlame(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	asset := filepath.Join(root, "hero.png")
	writeTestPNG(t, asset, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	firstOID, _, err := repo.CreateCommit("white", "test", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Commit v1: %v", err)
	}
	writeTestPNG(t, asset, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
	if _, err := repo.Add([]string{asset}, ""); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	secondOID, _, err := repo.CreateCommit("dark", "test", time.Unix(2, 0))
	if err != nil {
		t.Fatalf("Commit v2: %v", err)
	}

	lineage, err := repo.Lineage(asset)
	if err != nil || len(lineage) != 2 {
		t.Fatalf("Lineage = %d nodes, err %v", len(lineage), err)
	}
	blame, err := repo.BlamePoint(asset, 1, 1, imaging.DiffOptions{})
	if err != nil {
		t.Fatalf("BlamePoint: %v", err)
	}
	if blame.CommitOID != secondOID {
		t.Fatalf("blame commit = %s, want %s", blame.CommitOID, secondOID)
	}

	if _, err := os.OpenFile(asset, os.O_WRONLY|os.O_APPEND, 0); err != nil {
		t.Fatalf("open for mutation: %v", err)
	}
	if err := os.WriteFile(asset, []byte("broken"), 0o644); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if _, err := repo.Restore("", []string{asset}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	status, err := repo.Status()
	if err != nil || len(status.Unstaged) != 0 {
		t.Fatalf("status after restore = %#v, err %v", status.Unstaged, err)
	}

	if _, err := repo.CreateBranch("old", firstOID); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := repo.SwitchBranch("old"); err != nil {
		t.Fatalf("SwitchBranch old: %v", err)
	}
	head, err := repo.HeadOID()
	if err != nil || head != firstOID {
		t.Fatalf("old HEAD = %s, err %v", head, err)
	}
	if err := repo.SwitchBranch("main"); err != nil {
		t.Fatalf("SwitchBranch main: %v", err)
	}
	head, err = repo.HeadOID()
	if err != nil || head != secondOID {
		t.Fatalf("main HEAD = %s, err %v", head, err)
	}
}
