package repository

import (
	"image/color"
	"path/filepath"
	"testing"
)

func TestLockContentionAndOwnership(t *testing.T) {
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
	lock, err := repo.AcquireLock(asset, "alice", "")
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if lock.Owner != "alice" || lock.Token == "" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	if _, err := repo.AcquireLock(asset, "bob", ""); err == nil {
		t.Fatal("second lock unexpectedly succeeded")
	}
	if _, err := repo.ReleaseLock(asset, "bob", "", false); err == nil {
		t.Fatal("unlock by another owner unexpectedly succeeded")
	}
	locks, err := repo.ListLocks("")
	if err != nil || len(locks) != 1 {
		t.Fatalf("ListLocks = %d, err %v", len(locks), err)
	}
	if _, err := repo.ReleaseLock(asset, "alice", "", false); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	locks, err = repo.ListLocks("")
	if err != nil || len(locks) != 0 {
		t.Fatalf("ListLocks after release = %d, err %v", len(locks), err)
	}
}
