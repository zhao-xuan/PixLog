package repository

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitLockContentionAndFileRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	pointerData, _, err := repo.CleanFilter("hero.png", mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 0, Y: 0}: {R: 255, A: 255}}))
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	assetPath := filepath.Join(root, "hero.png")
	if err := os.WriteFile(assetPath, pointerData, 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	runGitTest(t, root, "add", "hero.png")

	local, err := repo.AcquireLock(assetPath, "alice", "")
	if err != nil {
		t.Fatalf("AcquireLock local: %v", err)
	}
	if local.Owner != "alice" || local.Token == "" {
		t.Fatalf("local lock = %#v", local)
	}
	if _, err := repo.AcquireLock(assetPath, "bob", ""); err == nil {
		t.Fatal("second local lock unexpectedly succeeded")
	}
	if _, err := repo.ReleaseLock(assetPath, "bob", "", false); err == nil {
		t.Fatal("local unlock by another owner unexpectedly succeeded")
	}
	if _, err := repo.ReleaseLock(assetPath, "alice", "", false); err != nil {
		t.Fatalf("ReleaseLock local: %v", err)
	}

	remoteRoot := filepath.Join(t.TempDir(), "media")
	runGitTest(t, root, "config", "pixlog.remote.origin.endpoint", remoteRoot)
	if _, err := repo.AcquireLock(assetPath, "alice", "origin"); err != nil {
		t.Fatalf("AcquireLock remote: %v", err)
	}
	if _, err := repo.AcquireLock(assetPath, "bob", "origin"); err == nil {
		t.Fatal("second remote lock unexpectedly succeeded")
	}
	locks, err := repo.ListLocks("origin")
	if err != nil || len(locks) != 1 || locks[0].Remote != "origin" {
		t.Fatalf("ListLocks remote = %#v, err = %v", locks, err)
	}
	if _, err := repo.ReleaseLock(assetPath, "alice", "origin", false); err != nil {
		t.Fatalf("ReleaseLock remote: %v", err)
	}
}
