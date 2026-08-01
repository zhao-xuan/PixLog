package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/repository"
)

func TestStatusAndDiffUseGitWithoutPixLogRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	runCLIGit(t, root, "config", "user.name", "PixLog Test")
	runCLIGit(t, root, "config", "user.email", "pixlog@example.test")
	asset := filepath.Join(root, "hero.png")
	writeGitCLIImage(t, asset, image.Point{X: -1, Y: -1})
	runCLIGit(t, root, "add", "hero.png")
	runCLIGit(t, root, "commit", "--quiet", "-m", "base")

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	writeGitCLIImage(t, asset, image.Point{X: 3, Y: 3})
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"status"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("status exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Changes not staged for commit") || !strings.Contains(stdout.String(), "hero.png") {
		t.Fatalf("status output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"diff", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("diff exit %d, stderr %s", exitCode, stderr.String())
	}
	var report repository.DiffReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode diff: %v\n%s", err, stdout.String())
	}
	if len(report.Assets) != 1 || report.Assets[0].Visual == nil || report.Assets[0].Visual.ChangedPixels != 1 {
		t.Fatalf("diff report = %#v", report)
	}

	runCLIGit(t, root, "add", "hero.png")
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"diff", "--staged", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("staged diff exit %d, stderr %s", exitCode, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode staged diff: %v\n%s", err, stdout.String())
	}
	if len(report.Assets) != 1 || report.Assets[0].Visual == nil {
		t.Fatalf("staged diff report = %#v", report)
	}
}

func TestCheckUsesGitRevisionRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	runCLIGit(t, root, "config", "user.name", "PixLog Test")
	runCLIGit(t, root, "config", "user.email", "pixlog@example.test")
	asset := filepath.Join(root, "hero.png")
	writeGitCLIImage(t, asset, image.Point{X: -1, Y: -1})
	runCLIGit(t, root, "add", "hero.png")
	runCLIGit(t, root, "commit", "--quiet", "-m", "base")
	writeGitCLIImage(t, asset, image.Point{X: 3, Y: 3})
	runCLIGit(t, root, "add", "hero.png")
	runCLIGit(t, root, "commit", "--quiet", "-m", "change")
	policy := `{"schema":"pixlog.policy/v1","rules":[{"asset":"hero.png","max_visual_change":0.01}]}`
	if err := os.WriteFile(filepath.Join(root, ".pixlog-policy.json"), []byte(policy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"check", "--range", "HEAD~1..HEAD", "--threshold", "0"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("check exit %d, stdout %s, stderr %s", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "max_visual_change") || !strings.Contains(stderr.String(), "policy check failed") {
		t.Fatalf("check output:\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

func TestInitGitInstallsCompanionWithoutPixLogRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"init", "--git", root}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("init --git exit %d, stderr %s", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".pixlog")); !os.IsNotExist(err) {
		t.Fatalf("standalone .pixlog directory was created: %v", err)
	}
	attributes, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if !strings.Contains(string(attributes), "*.png filter=pixlog diff=pixlog merge=pixlog -text") {
		t.Fatalf("attributes:\n%s", attributes)
	}
	driver := strings.TrimSpace(runCLIGit(t, root, "config", "--local", "--get", "diff.pixlog.command"))
	if !strings.Contains(driver, "git-diff") {
		t.Fatalf("driver command = %q", driver)
	}
}

func TestCompareAndGitDiffDriver(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.png")
	newPath := filepath.Join(root, "new.png")
	writeGitCLIImage(t, oldPath, image.Point{X: -1, Y: -1})
	writeGitCLIImage(t, newPath, image.Point{X: 3, Y: 3})

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"compare", "--json", "--threshold", "0", oldPath, newPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("compare exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"changed_pixels": 1`) {
		t.Fatalf("compare output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	oldOID := strings.Repeat("1", 40)
	newOID := strings.Repeat("2", 40)
	exitCode = Run([]string{"git-diff", "hero.png", oldPath, oldOID, "100644", newPath, newOID, "100644"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("git-diff exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "visual change") || !strings.Contains(stdout.String(), "hero.png") {
		t.Fatalf("git-diff output:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"git-diff", "hero.png", oldPath, oldOID, "100644", newPath, strings.Repeat("0", 40), "100644"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("worktree git-diff exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "M hero.png") || !strings.Contains(stdout.String(), "-> worktree") {
		t.Fatalf("worktree git-diff output:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"git-diff", "hero.png", oldPath, oldOID, "100644", "/dev/null", strings.Repeat("0", 40), "000000"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("deleted git-diff exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "- hero.png") || !strings.Contains(stdout.String(), "-> (none)") {
		t.Fatalf("deleted git-diff output:\n%s", stdout.String())
	}
}

func TestComparePreviewInvokesChafaWithThreeImages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell")
	}
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.png")
	newPath := filepath.Join(root, "new.png")
	writeGitCLIImage(t, oldPath, image.Point{X: -1, Y: -1})
	writeGitCLIImage(t, newPath, image.Point{X: 3, Y: 3})

	previewer := filepath.Join(root, "chafa")
	script := "#!/bin/sh\nprintf 'CHAFA_ARGS=%s\\n' \"$*\"\n"
	if err := os.WriteFile(previewer, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake chafa: %v", err)
	}
	t.Setenv("PIXLOG_CHAFA", previewer)
	t.Setenv("PIXLOG_CHAFA_FORMAT", "symbols")

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"compare", "--preview", "--threshold", "0", oldPath, newPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("compare preview exit %d, stderr %s", exitCode, stderr.String())
	}
	for _, expected := range []string{"preview", "CHAFA_ARGS=", "--grid 3x1", "--format symbols", "before.png", "after.png", "heatmap.png"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("preview output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestRunUsesGitCompanionAndStagesMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	runCLIGit(t, root, "config", "user.name", "PixLog Test")
	runCLIGit(t, root, "config", "user.email", "pixlog@example.test")
	writeGitCLIImage(t, filepath.Join(root, "source.png"), image.Point{X: -1, Y: -1})
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.png\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	runCLIGit(t, root, "add", "source.png", ".gitignore")
	runCLIGit(t, root, "commit", "--quiet", "-m", "base")
	gitHead := strings.TrimSpace(runCLIGit(t, root, "rev-parse", "HEAD"))
	t.Setenv("PIXLOG_EXECUTABLE", writeCLIFilterHelper(t))

	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"init", "--git"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("init --git exit %d, stderr %s", exitCode, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"run", "--", "cp", "source.png", "output.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run exit %d, stderr %s", exitCode, stderr.String())
	}
	staged := runCLIGit(t, root, "diff", "--cached", "--name-only")
	for _, expected := range []string{"output.png", ".gitattributes", ".pixlog.toml"} {
		if !strings.Contains(staged, expected) {
			t.Errorf("staged paths missing %q:\n%s", expected, staged)
		}
	}
	pointerData := []byte(runCLIGit(t, root, "show", ":output.png"))
	pointer, found, err := repository.ParsePixLogPointer(pointerData)
	if err != nil || !found || pointer.RecipeOID == "" {
		t.Fatalf("staged pointer = %#v, found = %v, err = %v", pointer, found, err)
	}
	gitRepo, err := repository.OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	_, recipeData, err := gitRepo.RecipeData("", filepath.Join(root, "output.png"))
	if err != nil {
		t.Fatalf("RecipeData: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(recipeData, &captured); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	sourceControl := captured["source_control"].(map[string]any)
	if sourceControl["head_oid"] != gitHead {
		t.Fatalf("source control = %#v", sourceControl)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"recipe", "show", "output.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("recipe show exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), gitHead) {
		t.Fatalf("recipe show output:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"inspect", "--json", "output.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("inspect exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"recipe_oid": "sha256:`) {
		t.Fatalf("inspect output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"run", "--", "cp", "source.png", "ignored.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("ignored run exit %d, stderr %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no image changes detected") {
		t.Fatalf("ignored run output:\n%s", stdout.String())
	}
}

func TestAddUsesGitCompanion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	t.Setenv("PIXLOG_EXECUTABLE", writeCLIFilterHelper(t))
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"init", "--git"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("init --git exit %d, stderr %s", exitCode, stderr.String())
	}
	writeGitCLIImage(t, filepath.Join(root, "hero.png"), image.Point{X: -1, Y: -1})
	stdout.Reset()
	stderr.Reset()
	if exitCode := Run([]string{"add", "hero.png"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("add exit %d, stderr %s", exitCode, stderr.String())
	}
	staged := runCLIGit(t, root, "diff", "--cached", "--name-only")
	for _, expected := range []string{"hero.png", ".gitattributes", ".pixlog.toml"} {
		if !strings.Contains(staged, expected) {
			t.Errorf("staged paths missing %q:\n%s", expected, staged)
		}
	}
	pointerData := []byte(runCLIGit(t, root, "show", ":hero.png"))
	if _, found, err := repository.ParsePixLogPointer(pointerData); err != nil || !found {
		t.Fatalf("staged hero pointer found = %v, err = %v", found, err)
	}
}

func TestGitCompanionDelegatesVersionControlCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runCLIGit(t, root, "init", "--quiet")
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"branch", "--show-current"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("branch exit %d, stderr %s", exitCode, stderr.String())
	}
	wantBranch := strings.TrimSpace(runCLIGit(t, root, "branch", "--show-current"))
	if strings.TrimSpace(stdout.String()) != wantBranch {
		t.Fatalf("branch output = %q, want %q", stdout.String(), wantBranch)
	}
	if command, delegated := gitProxyCommand("bisect"); !delegated || command != "bisect" {
		t.Fatalf("bisect proxy = %q, delegated = %v", command, delegated)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	wantStatus := runCLIGit(t, root, "status", "--porcelain=v2", "-z")
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"status", "--porcelain=v2", "-z"}, &stdout, &stderr)
	if exitCode != 0 || stdout.String() != wantStatus {
		t.Fatalf("porcelain status exit %d, output %q, want %q, stderr %s", exitCode, stdout.String(), wantStatus, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"git", "rev-parse", "--is-inside-work-tree"}, &stdout, &stderr)
	if exitCode != 0 || strings.TrimSpace(stdout.String()) != "true" {
		t.Fatalf("git escape exit %d, stdout %s, stderr %s", exitCode, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"commit", "--definitely-invalid-option"}, &stdout, &stderr)
	if exitCode != 129 {
		t.Fatalf("invalid git option exit %d, stderr %s", exitCode, stderr.String())
	}
}

func TestPixLogCloneHydratesAndInstallsIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	helperPath := writeCLIFilterHelper(t)
	t.Setenv("PIXLOG_EXECUTABLE", helperPath)

	remoteRoot := filepath.Join(t.TempDir(), "artwork.git")
	if err := os.MkdirAll(remoteRoot, 0o755); err != nil {
		t.Fatalf("create bare remote: %v", err)
	}
	runCLIGit(t, remoteRoot, "init", "--quiet", "--bare", "--initial-branch=main")
	source := t.TempDir()
	runCLIGit(t, source, "init", "--quiet", "--initial-branch=main")
	runCLIGit(t, source, "config", "user.name", "PixLog Test")
	runCLIGit(t, source, "config", "user.email", "pixlog@example.test")
	runCLIGit(t, source, "remote", "add", "origin", remoteRoot)
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("*.png filter=pixlog -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	imagePath := filepath.Join(source, "hero.png")
	writeGitCLIImage(t, imagePath, image.Point{X: 2, Y: 2})
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("read source image: %v", err)
	}
	sourceRepo, err := repository.OpenGit(source)
	if err != nil {
		t.Fatalf("OpenGit source: %v", err)
	}
	pointerData, _, err := sourceRepo.CleanFilter("hero.png", imageData)
	if err != nil {
		t.Fatalf("CleanFilter: %v", err)
	}
	if err := os.WriteFile(imagePath, pointerData, 0o644); err != nil {
		t.Fatalf("write source pointer: %v", err)
	}
	runCLIGit(t, source, "add", ".gitattributes", "hero.png")
	runCLIGit(t, source, "commit", "--quiet", "-m", "add artwork")
	head := strings.TrimSpace(runCLIGit(t, source, "rev-parse", "HEAD"))
	updates := []repository.GitRefUpdate{{LocalRef: "refs/heads/main", LocalOID: head, RemoteRef: "refs/heads/main", RemoteOID: strings.Repeat("0", 40)}}
	if _, err := sourceRepo.PushMediaObjects("origin", updates); err != nil {
		t.Fatalf("PushMediaObjects: %v", err)
	}
	runCLIGit(t, source, "push", "--quiet", "origin", "main")

	parent := t.TempDir()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })
	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"clone", remoteRoot, "checkout"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("clone exit %d, stdout %s, stderr %s", exitCode, stdout.String(), stderr.String())
	}
	clonedData, err := os.ReadFile(filepath.Join(parent, "checkout", "hero.png"))
	if err != nil {
		t.Fatalf("read cloned image: %v", err)
	}
	if !bytes.Equal(clonedData, imageData) {
		t.Fatal("clone did not hydrate exact image bytes")
	}
	filter := strings.TrimSpace(runCLIGit(t, filepath.Join(parent, "checkout"), "config", "--local", "--get", "filter.pixlog.process"))
	if !strings.Contains(filter, helperPath) {
		t.Fatalf("installed filter = %q", filter)
	}
	if _, err := os.Stat(filepath.Join(parent, "checkout", ".git", "hooks", "pre-push")); err != nil {
		t.Fatalf("pre-push hook was not installed: %v", err)
	}
}

func TestCLIGitFilterProcessHelper(t *testing.T) {
	if os.Getenv("PIXLOG_CLI_FILTER_HELPER") != "1" {
		t.Skip("filter helper")
	}
	if err := repository.ServeGitFilterProcess("", os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}

func writeCLIFilterHelper(t *testing.T) string {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	helperPath := filepath.Join(t.TempDir(), "pixlog-test-helper")
	helper := "#!/bin/sh\nPIXLOG_CLI_FILTER_HELPER=1 exec '" + strings.ReplaceAll(testExecutable, "'", "'\"'\"'") + "' -test.run '^TestCLIGitFilterProcessHelper$'\n"
	if err := os.WriteFile(helperPath, []byte(helper), 0o755); err != nil {
		t.Fatalf("write filter helper: %v", err)
	}
	return helperPath
}

func writeGitCLIImage(t *testing.T, path string, changed image.Point) {
	t.Helper()
	output := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			pixel := color.NRGBA{R: 20, G: 40, B: 60, A: 255}
			if x == changed.X && y == changed.Y {
				pixel = color.NRGBA{R: 240, G: 220, B: 200, A: 255}
			}
			output.SetNRGBA(x, y, pixel)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PNG: %v", err)
	}
	if err := png.Encode(file, output); err != nil {
		file.Close()
		t.Fatalf("encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close PNG: %v", err)
	}
}
