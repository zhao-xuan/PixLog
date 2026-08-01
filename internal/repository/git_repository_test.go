package repository

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

func TestGitRepositoryStatusAndDiffs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	asset := filepath.Join(root, "hero.png")
	writeGitPNG(t, asset, image.Point{X: -1, Y: -1})
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "base")

	gitRepo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	writeGitPNG(t, asset, image.Point{X: 3, Y: 3})
	status, err := gitRepo.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Staged) != 0 || len(status.Unstaged) != 1 || status.Unstaged[0].Path != "hero.png" {
		t.Fatalf("worktree status = %#v", status)
	}
	workingDiff, err := gitRepo.DiffWorking(nil, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("DiffWorking: %v", err)
	}
	assertGitVisualDiff(t, workingDiff)

	runGitTest(t, root, "add", "hero.png")
	status, err = gitRepo.Status()
	if err != nil {
		t.Fatalf("Status staged: %v", err)
	}
	if len(status.Staged) != 1 || len(status.Unstaged) != 0 {
		t.Fatalf("staged status = %#v", status)
	}
	stagedDiff, err := gitRepo.DiffStaged(nil, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("DiffStaged: %v", err)
	}
	assertGitVisualDiff(t, stagedDiff)

	runGitTest(t, root, "commit", "--quiet", "-m", "change")
	commitDiff, err := gitRepo.DiffCommits("HEAD~1", "HEAD", nil, imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("DiffCommits: %v", err)
	}
	assertGitVisualDiff(t, commitDiff)
}

func TestGitRepositoryHonorsIgnoredUntrackedAssets(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.png\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	writeGitPNG(t, filepath.Join(root, "ignored.png"), image.Point{})
	writeGitPNG(t, filepath.Join(root, "visible.png"), image.Point{})

	gitRepo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	status, err := gitRepo.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Untracked) != 1 || status.Untracked[0] != "visible.png" {
		t.Fatalf("untracked = %#v", status.Untracked)
	}
}

func TestGitRepositoryChecksPolicyAcrossRevisionRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	runGitTest(t, root, "config", "user.name", "PixLog Test")
	runGitTest(t, root, "config", "user.email", "pixlog@example.test")
	asset := filepath.Join(root, "hero.png")
	writeGitPNG(t, asset, image.Point{X: -1, Y: -1})
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "base")
	writeGitPNG(t, asset, image.Point{X: 3, Y: 3})
	runGitTest(t, root, "add", "hero.png")
	runGitTest(t, root, "commit", "--quiet", "-m", "change")

	maxChange := 0.01
	policyPath := filepath.Join(root, ".pixlog-policy.json")
	writePolicyFile(t, policyPath, Policy{
		Schema: PolicySchema,
		Rules: []PolicyRule{{
			Asset:           "hero.png",
			MaxVisualChange: &maxChange,
		}},
	})
	gitRepo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	result, err := gitRepo.CheckPolicyRange(policyPath, "HEAD~1..HEAD", imaging.DiffOptions{Threshold: 0})
	if err != nil {
		t.Fatalf("CheckPolicyRange: %v", err)
	}
	if result.Passed || len(result.Violations) != 1 || result.Violations[0].Code != "max_visual_change" {
		t.Fatalf("policy result = %#v", result)
	}
}

func TestInstallGitIntegrationIsIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	attributesPath := filepath.Join(root, ".gitattributes")
	if err := os.WriteFile(attributesPath, []byte("*.txt text\n"), 0o644); err != nil {
		t.Fatalf("write existing attributes: %v", err)
	}

	result, err := InstallGitIntegration(root, "/opt/Pix Log/pixlog")
	if err != nil {
		t.Fatalf("InstallGitIntegration: %v", err)
	}
	first, err := os.ReadFile(attributesPath)
	if err != nil {
		t.Fatalf("read attributes: %v", err)
	}
	if strings.Count(string(first), gitAttributesStart) != 1 || !strings.Contains(string(first), "*.png filter=pixlog diff=pixlog merge=pixlog -text") {
		t.Fatalf("attributes:\n%s", first)
	}
	expectedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if result.Root != expectedRoot || result.AttributesPath != filepath.Join(expectedRoot, ".gitattributes") {
		t.Fatalf("install result = %#v", result)
	}
	configured := strings.TrimSpace(runGitTestOutput(t, root, "config", "--local", "--get", "diff.pixlog.command"))
	if configured != "'/opt/Pix Log/pixlog' git-diff" {
		t.Fatalf("driver command = %q", configured)
	}

	if _, err := InstallGitIntegration(root, "/opt/Pix Log/pixlog"); err != nil {
		t.Fatalf("InstallGitIntegration again: %v", err)
	}
	second, err := os.ReadFile(attributesPath)
	if err != nil {
		t.Fatalf("read attributes again: %v", err)
	}
	if string(second) != string(first) {
		t.Fatalf("second install changed attributes:\n%s", second)
	}
}

func TestInstallGitIntegrationRejectsStandaloneRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	if _, err := Init(root, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := InstallGitIntegration(root, "pixlog"); err == nil || !strings.Contains(err.Error(), "standalone PixLog repository") {
		t.Fatalf("InstallGitIntegration error = %v", err)
	}
}

func TestGitRepositoryStagesAndReadsCaptureMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	if _, err := InstallGitIntegration(root, "pixlog"); err != nil {
		t.Fatalf("InstallGitIntegration: %v", err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	runGitTest(t, root, "config", "filter.pixlog.process", shellQuote(testExecutable)+" -test.run=^TestGitFilterProcessHelper$ --")
	t.Setenv("PIXLOG_TEST_FILTER_PROCESS", "1")
	writeGitPNG(t, filepath.Join(root, "hero.png"), image.Point{X: -1, Y: -1})
	gitRepo, err := OpenGit(root)
	if err != nil {
		t.Fatalf("OpenGit: %v", err)
	}
	recipeData := []byte(`{"schema":"pixlog.recipe/v1","kind":"command-edit"}`)
	recipeOID, err := gitRepo.ApplyCommandCapture([]string{"hero.png"}, nil, recipeData)
	if err != nil {
		t.Fatalf("ApplyCommandCapture: %v", err)
	}
	if recipeOID == "" {
		t.Fatal("recipe OID is empty")
	}
	entries, err := gitRepo.CaptureEntries()
	if err != nil {
		t.Fatalf("CaptureEntries: %v", err)
	}
	if entries["hero.png"].RecipeOID != recipeOID {
		t.Fatalf("entry = %#v", entries["hero.png"])
	}
	loadedOID, loadedRecipe, err := gitRepo.RecipeData("", filepath.Join(root, "hero.png"))
	if err != nil {
		t.Fatalf("RecipeData: %v", err)
	}
	if loadedOID != recipeOID || !strings.Contains(string(loadedRecipe), `"kind":"command-edit"`) {
		t.Fatalf("loaded recipe %s: %s", loadedOID, loadedRecipe)
	}
	staged := runGitTestOutput(t, root, "diff", "--cached", "--name-only")
	for _, expected := range []string{"hero.png", ".gitattributes", ".pixlog.toml"} {
		if !strings.Contains(staged, expected) {
			t.Errorf("staged paths missing %q:\n%s", expected, staged)
		}
	}
	pointerData, err := gitBytes(root, "show", ":hero.png")
	if err != nil {
		t.Fatalf("read staged pointer: %v", err)
	}
	pointer, found, err := ParsePixLogPointer(pointerData)
	if err != nil || !found || pointer.RecipeOID != recipeOID {
		t.Fatalf("staged pointer = %#v, found = %v, err = %v", pointer, found, err)
	}

	policyPath := filepath.Join(root, ".pixlog-policy.json")
	writePolicyFile(t, policyPath, Policy{
		Schema: PolicySchema,
		Rules:  []PolicyRule{{Asset: "hero.png", RequireRecipe: true}},
	})
	result, err := gitRepo.CheckPolicy(policyPath, imaging.DiffOptions{})
	if err != nil {
		t.Fatalf("CheckPolicy: %v", err)
	}
	if !result.Passed {
		t.Fatalf("policy result = %#v", result)
	}
	store, err := OpenGitMediaStore(root)
	if err != nil {
		t.Fatalf("OpenGitMediaStore: %v", err)
	}
	manifestPath, err := store.ObjectPath(pointer.ManifestOID)
	if err != nil {
		t.Fatalf("manifest object path: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("write tampered manifest: %v", err)
	}
	if _, err := gitRepo.CheckPolicy(policyPath, imaging.DiffOptions{}); err == nil || !strings.Contains(err.Error(), "failed content verification") {
		t.Fatalf("CheckPolicy tampered manifest error = %v", err)
	}
}

func runGitTestOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func assertGitVisualDiff(t *testing.T, report DiffReport) {
	t.Helper()
	if len(report.Assets) != 1 || report.Assets[0].Path != "hero.png" || report.Assets[0].Visual == nil {
		t.Fatalf("diff report = %#v", report)
	}
	if report.Assets[0].Visual.ChangedPixels != 1 {
		t.Fatalf("visual diff = %#v", report.Assets[0].Visual)
	}
}

func writeGitPNG(t *testing.T, filePath string, changed image.Point) {
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
	file, err := os.Create(filePath)
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
