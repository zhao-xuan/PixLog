package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitTrackPatternsPersistAcrossInstall(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	repo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	patterns, err := repo.TrackPatterns([]string{"art/**/*.kra", "renders/*.bmp", "art/**/*.kra"})
	if err != nil {
		t.Fatalf("TrackPatterns: %v", err)
	}
	if len(patterns) != 2 || patterns[0] != "art/**/*.kra" || patterns[1] != "renders/*.bmp" {
		t.Fatalf("patterns = %#v", patterns)
	}
	if _, err := InstallGitIntegration(root, "/usr/local/bin/pixlog"); err != nil {
		t.Fatalf("InstallGitIntegration: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	content := string(data)
	for _, expected := range []string{
		"art/**/*.kra filter=pixlog diff=pixlog merge=pixlog -text",
		"renders/*.bmp filter=pixlog diff=pixlog merge=pixlog -text",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("attributes missing %q:\n%s", expected, content)
		}
	}
	if strings.Count(content, gitTrackingStart) != 1 {
		t.Fatalf("custom tracking block count = %d\n%s", strings.Count(content, gitTrackingStart), content)
	}
}
