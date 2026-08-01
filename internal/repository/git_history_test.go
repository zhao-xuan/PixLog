package repository

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

func TestGitLineageAndBlameFollowRename(t *testing.T) {
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
	writeHistoryPointer(t, repo, filepath.Join(root, "hero.png"), mergeTestPNG(t, map[image.Point]color.NRGBA{}))
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "add hero")
	writeHistoryPointer(t, repo, filepath.Join(root, "hero.png"), mergeTestPNG(t, map[image.Point]color.NRGBA{{X: 1, Y: 1}: {G: 255, A: 255}}))
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "paint corner")
	paintCommit, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("resolve paint commit: %v", err)
	}
	runGitTest(t, root, "mv", "hero.png", "art.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "rename artwork")

	lineage, err := repo.Lineage(filepath.Join(root, "art.png"))
	if err != nil {
		t.Fatalf("Lineage: %v", err)
	}
	if len(lineage) != 3 {
		t.Fatalf("lineage length = %d, want 3: %#v", len(lineage), lineage)
	}
	if lineage[1].CommitOID != paintCommit || lineage[1].Message != "paint corner" {
		t.Fatalf("paint lineage node = %#v", lineage[1])
	}
	blame, err := repo.BlamePoint(filepath.Join(root, "art.png"), 1, 1, imaging.DiffOptions{Threshold: 8})
	if err != nil {
		t.Fatalf("BlamePoint: %v", err)
	}
	if blame.CommitOID != paintCommit || blame.Confidence != "exact-pixel-region" {
		t.Fatalf("blame = %#v", blame)
	}
}

func writeHistoryPointer(t *testing.T, repo *GitRepository, path string, data []byte) {
	t.Helper()
	pointerData, pointer, err := repo.CleanFilter(filepath.Base(path), data)
	if err != nil || pointer.OID == "" {
		t.Fatalf("CleanFilter pointer = %#v, err = %v", pointer, err)
	}
	if err := os.WriteFile(path, pointerData, 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
}
