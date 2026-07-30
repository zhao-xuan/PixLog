package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiscoverGitContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	trackedPath := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitTest(t, root, "add", "tracked.txt")
	runGitTest(t, root, "commit", "--quiet", "-m", "initial")

	context, err := DiscoverGitContext(root)
	if err != nil {
		t.Fatalf("DiscoverGitContext: %v", err)
	}
	if context == nil || context.Provider != "git" || context.HeadOID == "" || context.IndexTreeOID == "" {
		t.Fatalf("context = %#v", context)
	}
	if context.Branch == "" {
		t.Fatal("branch is empty for attached HEAD")
	}
	if context.WorktreeDirty {
		t.Fatal("clean worktree reported dirty")
	}

	if err := os.WriteFile(trackedPath, []byte("two\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	context, err = DiscoverGitContext(root)
	if err != nil {
		t.Fatalf("DiscoverGitContext after modification: %v", err)
	}
	if !context.WorktreeDirty {
		t.Fatal("modified worktree reported clean")
	}
}

func TestDiscoverGitContextOutsideRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	context, err := DiscoverGitContext(t.TempDir())
	if err != nil {
		t.Fatalf("DiscoverGitContext: %v", err)
	}
	if context != nil {
		t.Fatalf("context = %#v, want nil", context)
	}
}

func runGitTest(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
