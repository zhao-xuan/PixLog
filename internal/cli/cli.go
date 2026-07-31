package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/imaging"
	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

var Version = "0.1.0-dev"

type repositoryView interface {
	Status() (repository.Status, error)
	DiffWorking([]string, imaging.DiffOptions) (repository.DiffReport, error)
	DiffStaged([]string, imaging.DiffOptions) (repository.DiffReport, error)
	DiffCommits(string, string, []string, imaging.DiffOptions) (repository.DiffReport, error)
	CheckPolicy(string, imaging.DiffOptions) (repository.PolicyCheckResult, error)
}

type assetAdder interface {
	Add([]string, string) ([]repository.Entry, error)
}

type commandCaptureRepository interface {
	RootPath() string
	SnapshotAssets() (map[string]string, error)
	CaptureEntries() (map[string]repository.Entry, error)
	ApplyCommandCapture([]string, []string, []byte) (string, error)
}

type recipeRepository interface {
	ImportRecipe(string, []byte) (string, error)
	RecipeData(string, string) (string, []byte, error)
	RecipeDiff(string, string, string) (string, string, []recipe.Change, error)
}

type inspectionRepository interface {
	Inspect(string, string) (repository.Inspection, error)
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	if gitCommand, delegated := gitProxyCommand(args[0]); delegated {
		if err := runGitProxy(gitCommand, args[1:], stdout, stderr); err != nil {
			return commandExitCode(err, stderr)
		}
		return 0
	}
	var err error
	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintf(stdout, "pixlog %s\n", Version)
		return 0
	case "init":
		err = runInit(args[1:], stdout, stderr)
	case "install":
		err = runGitIntegration(append([]string{"install"}, args[1:]...), stdout, stderr)
	case "git":
		err = runGitIntegration(args[1:], stdout, stderr)
	case "git-diff":
		err = runGitDiff(args[1:], stdout, stderr)
	case "git-merge-driver":
		err = runGitMergeDriver(args[1:])
	case "filter-process":
		err = runFilterProcess(args[1:], stdout)
	case "hook":
		err = runHook(args[1:], stdout, stderr)
	case "add":
		err = runAdd(args[1:], stdout, stderr)
	case "track":
		err = runTrack(args[1:], stdout, stderr)
	case "rm":
		err = runRemove(args[1:], stdout, stderr)
	case "status":
		err = runStatus(args[1:], stdout, stderr)
	case "diff":
		err = runDiff(args[1:], stdout, stderr)
	case "compare":
		err = runCompare(args[1:], stdout, stderr)
	case "hydrate", "dehydrate":
		err = runHydration(args[0], args[1:], stdout, stderr)
	case "commit":
		err = runCommit(args[1:], stdout, stderr)
	case "log":
		err = runLog(args[1:], stdout, stderr)
	case "recipe":
		err = runRecipe(args[1:], stdout, stderr)
	case "remote":
		err = runRemote(args[1:], stdout, stderr)
	case "push", "fetch", "pull":
		err = runSync(args[0], args[1:], stdout, stderr)
	case "clone":
		err = runClone(args[1:], stdout, stderr)
	case "verify":
		err = runVerify(args[1:], stdout, stderr)
	case "doctor":
		err = runDoctor(args[1:], stdout, stderr)
	case "inspect":
		err = runInspect(args[1:], stdout, stderr)
	case "restore":
		err = runRestore(args[1:], stdout, stderr)
	case "lineage":
		err = runLineage(args[1:], stdout, stderr)
	case "reproduce":
		err = runReproduce(args[1:], stdout, stderr)
	case "blame":
		err = runBlame(args[1:], stdout, stderr)
	case "branch":
		err = runBranch(args[1:], stdout, stderr)
	case "switch":
		err = runSwitch(args[1:], stdout, stderr)
	case "tag":
		err = runTag(args[1:], stdout, stderr)
	case "lock":
		err = runLock(args[1:], stdout, stderr)
	case "unlock":
		err = runUnlock(args[1:], stdout, stderr)
	case "locks":
		err = runLocks(args[1:], stdout, stderr)
	case "check":
		err = runCheck(args[1:], stdout, stderr)
	case "run":
		err = runCapturedCommand(args[1:], stdout, stderr)
	case "capture":
		err = runCapture(args[1:], stdout, stderr)
	case "metadata":
		err = runMetadata(args[1:], stdout, stderr)
	case "c2pa":
		err = runC2PA(args[1:], stdout, stderr)
	case "bisect":
		err = runBisect(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "pixlog: unknown command %q\n", args[0])
		fmt.Fprintln(stderr, "Run 'pixlog help' for usage.")
		return 2
	}
	if err != nil {
		var exitError *commandExitError
		if errors.As(err, &exitError) {
			return exitError.Code
		}
		fmt.Fprintf(stderr, "pixlog: %v\n", err)
		return 1
	}
	return 0
}

func gitProxyCommand(command string) (string, bool) {
	alternatives := map[string]string{
		"rm":          "rm",
		"commit":      "commit",
		"log":         "log",
		"show":        "show",
		"restore":     "restore",
		"checkout":    "checkout",
		"branch":      "branch",
		"switch":      "switch",
		"tag":         "tag",
		"remote":      "remote",
		"push":        "push",
		"fetch":       "fetch",
		"pull":        "pull",
		"clone":       "clone",
		"merge":       "merge",
		"bisect":      "bisect",
		"rebase":      "rebase",
		"cherry-pick": "cherry-pick",
		"reset":       "reset",
		"revert":      "revert",
	}
	alternative, exists := alternatives[command]
	return alternative, exists
}

type commandExitError struct {
	Code int
}

func (err *commandExitError) Error() string {
	return fmt.Sprintf("command exited with status %d", err.Code)
}

func commandExitCode(err error, stderr io.Writer) int {
	var exitError *commandExitError
	if errors.As(err, &exitError) {
		return exitError.Code
	}
	fmt.Fprintf(stderr, "pixlog: %v\n", err)
	return 1
}

func runGitPassthrough(command string, args []string, stdout, stderr io.Writer) error {
	child := exec.Command("git", append([]string{command}, args...)...)
	child.Stdin = os.Stdin
	child.Stdout = stdout
	child.Stderr = stderr
	if err := child.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return &commandExitError{Code: exitError.ExitCode()}
		}
		return fmt.Errorf("run git %s: %w", command, err)
	}
	return nil
}

func runGitProxy(command string, args []string, stdout, stderr io.Writer) error {
	if command == "clone" {
		return runGitCloneProxy(args, stdout, stderr)
	}
	if command == "push" {
		executable, err := currentExecutable()
		if err != nil {
			return err
		}
		if _, err := repository.InstallPrePushHook("", executable); err != nil {
			return err
		}
	}
	return runGitPassthrough(command, args, stdout, stderr)
}

func runGitCloneProxy(args []string, stdout, stderr io.Writer) error {
	executable, err := currentExecutable()
	if err != nil {
		return err
	}
	filterCommand := shellCommandQuote(executable) + " filter-process"
	childArgs := []string{"-c", "filter.pixlog.process=" + filterCommand, "-c", "filter.pixlog.required=true", "clone"}
	childArgs = append(childArgs, args...)
	child := exec.Command("git", childArgs...)
	child.Stdin = os.Stdin
	child.Stdout = stdout
	child.Stderr = stderr
	if err := child.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return &commandExitError{Code: exitError.ExitCode()}
		}
		return fmt.Errorf("run git clone: %w", err)
	}
	target, err := gitCloneTarget(args)
	if err != nil {
		return err
	}
	if _, err := repository.InstallGitIntegration(target, executable); err != nil {
		return fmt.Errorf("install PixLog in cloned repository: %w", err)
	}
	return nil
}

func gitCloneTarget(args []string) (string, error) {
	optionsWithValues := map[string]bool{
		"--template": true, "--reference": true, "--reference-if-able": true,
		"--origin": true, "-o": true, "--branch": true, "-b": true,
		"--upload-pack": true, "-u": true, "--depth": true, "--shallow-since": true,
		"--shallow-exclude": true, "--separate-git-dir": true, "--jobs": true,
		"-j": true, "--filter": true, "--server-option": true, "--config": true, "-c": true,
	}
	positionals := []string{}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		name := argument
		if before, _, found := strings.Cut(argument, "="); found {
			name = before
		}
		if optionsWithValues[name] {
			if name == argument {
				index++
			}
			continue
		}
		if strings.HasPrefix(argument, "-") {
			continue
		}
		positionals = append(positionals, argument)
	}
	if len(positionals) == 0 || len(positionals) > 2 {
		return "", errors.New("could not determine cloned repository path")
	}
	if len(positionals) == 2 {
		return positionals[1], nil
	}
	remote := strings.TrimRight(positionals[0], "/")
	if colon := strings.LastIndex(remote, ":"); colon >= 0 && !strings.Contains(remote[colon+1:], "/") {
		remote = remote[colon+1:]
	} else {
		remote = filepath.Base(remote)
	}
	target := strings.TrimSuffix(remote, ".git")
	if target == "" || target == "." {
		return "", fmt.Errorf("could not infer clone directory from %q", positionals[0])
	}
	return target, nil
}

func shellCommandQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runInit(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("init", stderr)
	bare := flags.Bool("bare", false, "unsupported: PixLog requires a Git worktree")
	_ = flags.Bool("git", false, "deprecated alias; Git is always the version-control backend")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("usage: pixlog init [--git] [path]")
	}
	path := "."
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	if *bare {
		return errors.New("PixLog requires a non-bare Git worktree; use git init --bare only for a Git remote")
	}
	if _, err := repository.OpenGit(path); err != nil {
		if !errors.Is(err, repository.ErrNotGitRepository) {
			return err
		}
		if err := runGitPassthrough("init", []string{"--", path}, stdout, stderr); err != nil {
			return err
		}
	}
	result, err := installGitIntegration(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Initialized PixLog in Git repository %s\n", result.Root)
	fmt.Fprintf(stdout, "Updated %s\n", result.AttributesPath)
	fmt.Fprintf(stdout, "Config %s\n", result.ConfigPath)
	return nil
}

func runGitIntegration(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog git <git-command> [args...] | pixlog git install [path]")
	}
	switch args[0] {
	case "install":
		flags := newFlagSet("git install", stderr)
		asJSON := flags.Bool("json", false, "emit machine-readable JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() > 1 {
			return errors.New("usage: pixlog git install [--json] [path]")
		}
		path := "."
		if flags.NArg() == 1 {
			path = flags.Arg(0)
		}
		result, err := installGitIntegration(path)
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(stdout, result)
		}
		fmt.Fprintf(stdout, "Git repository: %s\n", result.Root)
		fmt.Fprintf(stdout, "Attributes:     %s\n", result.AttributesPath)
		fmt.Fprintf(stdout, "Config:         %s\n", result.ConfigPath)
		fmt.Fprintf(stdout, "Diff driver:    %s\n", result.DriverCommand)
		fmt.Fprintf(stdout, "Media filter:   %s\n", result.FilterCommand)
		fmt.Fprintf(stdout, "Merge driver:   %s\n", result.MergeCommand)
		return nil
	default:
		return runGitPassthrough(args[0], args[1:], stdout, stderr)
	}
}

func installGitIntegration(path string) (repository.GitInstallResult, error) {
	executable, err := currentExecutable()
	if err != nil {
		return repository.GitInstallResult{}, fmt.Errorf("resolve PixLog executable: %w", err)
	}
	return repository.InstallGitIntegration(path, executable)
}

func currentExecutable() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PIXLOG_EXECUTABLE")); override != "" {
		return filepath.Abs(override)
	}
	return os.Executable()
}

func runAdd(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("add", stderr)
	recipeOID := flags.String("recipe", "", "attach an existing recipe object ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openAssetAdder()
	if err != nil {
		return err
	}
	entries, err := repo.Add(flags.Args(), *recipeOID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "add %s  %s\n", repository.ShortOID(entry.ContentOID), entry.Path)
	}
	return nil
}

func runTrack(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("track", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	patterns, err := repo.TrackPatterns(flags.Args())
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, map[string]any{"patterns": patterns})
	}
	for _, pattern := range patterns {
		fmt.Fprintf(stdout, "track %s\n", pattern)
	}
	return nil
}

func runRemove(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("rm", stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	removed, err := repo.Remove(flags.Args())
	if err != nil {
		return err
	}
	for _, path := range removed {
		fmt.Fprintf(stdout, "rm  %s\n", path)
	}
	return nil
}

func runStatus(args []string, stdout, stderr io.Writer) error {
	for _, argument := range args {
		if argument == "--porcelain" || strings.HasPrefix(argument, "--porcelain=") {
			return runGitPassthrough("status", args, stdout, stderr)
		}
	}
	flags := newFlagSet("status", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog status [--json]")
	}
	repo, err := openRepositoryView()
	if err != nil {
		return err
	}
	status, err := repo.Status()
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "On branch %s\n", status.Branch)
	if status.Head == "" {
		fmt.Fprintln(stdout, "No commits yet")
	} else {
		fmt.Fprintf(stdout, "HEAD %s\n", repository.ShortOID(status.Head))
	}
	printChanges(stdout, "Changes to be committed", status.Staged)
	printChanges(stdout, "Changes not staged for commit", status.Unstaged)
	if len(status.Untracked) > 0 {
		fmt.Fprintln(stdout, "\nUntracked image assets:")
		for _, path := range status.Untracked {
			fmt.Fprintf(stdout, "  ? %s\n", path)
		}
	}
	if len(status.Staged) == 0 && len(status.Unstaged) == 0 && len(status.Untracked) == 0 {
		fmt.Fprintln(stdout, "nothing to commit, working tree clean")
	}
	return nil
}

func runDiff(args []string, stdout, stderr io.Writer) error {
	preSeparator, pathsAfterSeparator := splitSeparator(args)
	flags := newFlagSet("diff", stderr)
	staged := flags.Bool("staged", false, "compare HEAD with the index")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	format := flags.String("format", "text", "output format: text, json, or ndjson")
	heatmapPath := flags.String("heatmap", "", "write a PNG heatmap for a single changed asset")
	threshold := flags.Int("threshold", 8, "per-channel change threshold from 0 to 255")
	preview := flags.Bool("preview", false, "force inline before, after, and heatmap previews with Chafa")
	noPreview := flags.Bool("no-preview", false, "disable automatic terminal image previews")
	if err := flags.Parse(preSeparator); err != nil {
		return err
	}
	if *threshold < 0 || *threshold > 255 {
		return errors.New("threshold must be between 0 and 255")
	}
	if *asJSON {
		*format = "json"
	}
	if *format != "text" && *format != "json" && *format != "ndjson" {
		return errors.New("format must be text, json, or ndjson")
	}
	previewer, showPreview, err := terminalPreviewer(stdout, *preview, *noPreview, *format != "text")
	if err != nil {
		return err
	}

	revisions := flags.Args()
	paths := pathsAfterSeparator
	if pathsAfterSeparator == nil {
		switch len(revisions) {
		case 0:
		case 1:
			paths = revisions
			revisions = nil
		case 2:
		default:
			return errors.New("use -- before path filters when comparing two revisions")
		}
	}
	if *staged && len(revisions) > 0 {
		return errors.New("--staged cannot be combined with revision arguments")
	}
	if len(revisions) != 0 && len(revisions) != 2 {
		return errors.New("diff requires zero or two revisions")
	}

	repo, err := openRepositoryView()
	if err != nil {
		return err
	}
	options := imaging.DiffOptions{Threshold: uint8(*threshold), IncludePreview: showPreview}
	var report repository.DiffReport
	switch {
	case len(revisions) == 2:
		report, err = repo.DiffCommits(revisions[0], revisions[1], paths, options)
	case *staged:
		report, err = repo.DiffStaged(paths, options)
	default:
		report, err = repo.DiffWorking(paths, options)
	}
	if err != nil {
		return err
	}

	if *heatmapPath != "" {
		visuals := make([]imaging.VisualDiff, 0, 1)
		for _, asset := range report.Assets {
			if asset.Visual != nil {
				visuals = append(visuals, *asset.Visual)
			}
		}
		if len(visuals) != 1 {
			return fmt.Errorf("--heatmap requires exactly one visually comparable changed asset, found %d", len(visuals))
		}
		file, createErr := os.Create(*heatmapPath)
		if createErr != nil {
			return fmt.Errorf("create heatmap: %w", createErr)
		}
		writeErr := imaging.WriteHeatmap(file, visuals[0])
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return fmt.Errorf("close heatmap: %w", closeErr)
		}
	}

	switch *format {
	case "json":
		return writeJSON(stdout, report)
	case "ndjson":
		encoder := json.NewEncoder(stdout)
		for _, asset := range report.Assets {
			if err := encoder.Encode(asset); err != nil {
				return err
			}
		}
		return nil
	default:
		printDiff(stdout, report)
		if showPreview {
			return renderDiffPreviews(previewer, stdout, stderr, report)
		}
		return nil
	}
}

func runCompare(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("compare", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	threshold := flags.Int("threshold", 8, "per-channel change threshold from 0 to 255")
	preview := flags.Bool("preview", false, "force inline before, after, and heatmap previews with Chafa")
	noPreview := flags.Bool("no-preview", false, "disable automatic terminal image previews")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: pixlog compare [--json] [--threshold <0-255>] <old-image> <new-image>")
	}
	if *threshold < 0 || *threshold > 255 {
		return errors.New("threshold must be between 0 and 255")
	}
	previewer, showPreview, err := terminalPreviewer(stdout, *preview, *noPreview, *asJSON)
	if err != nil {
		return err
	}
	visual, err := compareImageFiles(flags.Arg(0), flags.Arg(1), imaging.DiffOptions{Threshold: uint8(*threshold)})
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, visual)
	}
	report := repository.DiffReport{
		From: flags.Arg(0),
		To:   flags.Arg(1),
		Assets: []repository.AssetDiff{{
			Path:   flags.Arg(1),
			Kind:   repository.ChangeModified,
			Visual: &visual,
		}},
	}
	if showPreview {
		before, err := readImageFile(flags.Arg(0))
		if err != nil {
			return err
		}
		after, err := readImageFile(flags.Arg(1))
		if err != nil {
			return err
		}
		report.Assets[0].Preview = &repository.AssetPreview{Before: before, After: after}
	}
	printDiff(stdout, report)
	if showPreview {
		return renderDiffPreviews(previewer, stdout, stderr, report)
	}
	return nil
}

func runHydration(action string, args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet(action, stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	var result repository.HydrateResult
	if action == "hydrate" {
		result, err = repo.Hydrate(flags.Args())
	} else {
		result, err = repo.Dehydrate(flags.Args())
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	paths := result.Hydrated
	if action == "dehydrate" {
		paths = result.Dehydrated
	}
	for _, path := range paths {
		fmt.Fprintf(stdout, "%s %s\n", action, path)
	}
	return nil
}

func runGitDiff(args []string, stdout, stderr io.Writer) error {
	if len(args) != 7 {
		return errors.New("git-diff must be invoked by Git's external diff protocol")
	}
	asset := repository.AssetDiff{Path: filepath.ToSlash(args[0]), Kind: repository.ChangeModified}
	switch {
	case args[3] == "000000" || args[1] == "/dev/null":
		asset.Kind = repository.ChangeAdded
	case args[6] == "000000" || args[4] == "/dev/null":
		asset.Kind = repository.ChangeDeleted
	default:
		visual, err := compareImageFiles(args[1], args[4], imaging.DiffOptions{Threshold: 8})
		if err != nil {
			if errors.Is(err, imaging.ErrUnsupportedVisualFormat) {
				asset.Note = "visual diff unavailable for this format"
			} else {
				return err
			}
		} else {
			asset.Visual = &visual
		}
	}
	from, to := repository.ShortOID(args[2]), repository.ShortOID(args[5])
	if isNullGitOID(args[2]) {
		from = "(none)"
	}
	if isNullGitOID(args[5]) {
		to = "worktree"
	}
	if asset.Kind == repository.ChangeDeleted {
		to = "(none)"
	}
	report := repository.DiffReport{
		From:   from,
		To:     to,
		Assets: []repository.AssetDiff{asset},
	}
	previewer, showPreview, err := terminalPreviewer(stdout, false, false, false)
	if err != nil {
		return err
	}
	if showPreview {
		preview := &repository.AssetPreview{}
		if args[1] != "/dev/null" {
			preview.Before, err = readImageFile(args[1])
			if err != nil {
				return err
			}
		}
		if args[4] != "/dev/null" {
			preview.After, err = readImageFile(args[4])
			if err != nil {
				return err
			}
		}
		report.Assets[0].Preview = preview
	}
	printDiff(stdout, report)
	if showPreview {
		return renderDiffPreviews(previewer, stdout, stderr, report)
	}
	return nil
}

func runGitMergeDriver(args []string) error {
	if len(args) != 5 {
		return errors.New("git-merge-driver must be invoked by Git")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	return repo.MergeDriver(args[0], args[1], args[2], args[4])
}

func runFilterProcess(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: pixlog filter-process")
	}
	return repository.ServeGitFilterProcess("", os.Stdin, stdout)
}

func runHook(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "dispatch-pre-push" {
		return errors.New("usage: pixlog hook dispatch-pre-push <remote-name> [remote-location]")
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	result, err := repository.RunPrePushDispatcher("", args[1:], input, stdout, stderr)
	if err != nil {
		return err
	}
	if len(result.Objects) > 0 {
		fmt.Fprintf(stderr, "PixLog uploaded %d object(s), %d byte(s) to %s\n", len(result.Objects), result.Bytes, result.Endpoint)
	}
	return nil
}

func compareImageFiles(oldPath, newPath string, options imaging.DiffOptions) (imaging.VisualDiff, error) {
	oldData, err := readImageFile(oldPath)
	if err != nil {
		return imaging.VisualDiff{}, err
	}
	newData, err := readImageFile(newPath)
	if err != nil {
		return imaging.VisualDiff{}, err
	}
	return imaging.CompareReaders(bytes.NewReader(oldData), bytes.NewReader(newData), options)
}

func readImageFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open image %s: %w", path, err)
	}
	if _, found, _ := repository.ParsePixLogPointer(data); !found {
		return data, nil
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return nil, err
	}
	data, _, err = repo.SmudgeFilter(data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func isNullGitOID(oid string) bool {
	return oid != "" && strings.Trim(oid, "0") == ""
}

func runCommit(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("commit", stderr)
	message := flags.String("m", "", "commit message")
	author := flags.String("author", "", "commit author")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog commit -m <message> [--author <author>]")
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	oid, commit, err := repo.CreateCommit(*message, *author, time.Now())
	if err != nil {
		return err
	}
	branch, _ := repo.CurrentBranch()
	fmt.Fprintf(stdout, "[%s %s] %s\n", branch, repository.ShortOID(oid), commit.Message)
	fmt.Fprintf(stdout, " %d image asset(s)\n", len(commit.Tree))
	return nil
}

func runLog(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("log", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	limit := flags.Int("n", 0, "limit number of commits")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("usage: pixlog log [--json] [-n count] [path]")
	}
	path := ""
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	records, err := repo.Log(path, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, records)
	}
	for index, record := range records {
		if index > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "commit %s\n", record.OID)
		fmt.Fprintf(stdout, "Author: %s\n", record.Commit.Author)
		fmt.Fprintf(stdout, "Date:   %s\n\n", record.Commit.CreatedAt.Local().Format(time.RFC1123Z))
		fmt.Fprintf(stdout, "    %s\n", record.Commit.Message)
	}
	return nil
}

func runRecipe(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog recipe <import|show|diff|infer> ...")
	}
	switch args[0] {
	case "import":
		flags := newFlagSet("recipe import", stderr)
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 2 {
			return errors.New("usage: pixlog recipe import <asset> <recipe.json>")
		}
		repo, err := openRecipeRepository()
		if err != nil {
			return err
		}
		data, err := repository.ReadRecipeFile(flags.Arg(1))
		if err != nil {
			return err
		}
		oid, err := repo.ImportRecipe(flags.Arg(0), data)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "recipe %s  %s\n", repository.ShortOID(oid), flags.Arg(0))
		return nil
	case "show":
		flags := newFlagSet("recipe show", stderr)
		revision := flags.String("revision", "", "read the recipe from a commit instead of the index")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("usage: pixlog recipe show [--revision <rev>] <asset>")
		}
		repo, err := openRecipeRepository()
		if err != nil {
			return err
		}
		oid, data, err := repo.RecipeData(*revision, flags.Arg(0))
		if err != nil {
			return err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "recipe %s\n%s\n", oid, pretty.String())
		return nil
	case "diff":
		preSeparator, paths := splitSeparator(args[1:])
		flags := newFlagSet("recipe diff", stderr)
		asJSON := flags.Bool("json", false, "emit machine-readable JSON")
		if err := flags.Parse(preSeparator); err != nil {
			return err
		}
		if flags.NArg() != 2 || len(paths) != 1 {
			return errors.New("usage: pixlog recipe diff [--json] <old-rev> <new-rev> -- <asset>")
		}
		repo, err := openRecipeRepository()
		if err != nil {
			return err
		}
		oldOID, newOID, changes, err := repo.RecipeDiff(flags.Arg(0), flags.Arg(1), paths[0])
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(stdout, map[string]any{"old_recipe_oid": oldOID, "new_recipe_oid": newOID, "changes": changes})
		}
		fmt.Fprintf(stdout, "recipe diff %s -> %s\n", repository.ShortOID(oldOID), repository.ShortOID(newOID))
		if len(changes) == 0 {
			fmt.Fprintln(stdout, "no recipe changes")
		}
		for _, change := range changes {
			fmt.Fprintf(stdout, "  %-32s %v -> %v\n", change.Field, displayEmpty(change.Old), displayEmpty(change.New))
		}
		return nil
	case "infer":
		flags := newFlagSet("recipe infer", stderr)
		assetPath := flags.String("asset", "", "asset that receives the inferred recipe; defaults to AFTER")
		threshold := flags.Int("threshold", 8, "per-channel visual change threshold from 0 to 255")
		asJSON := flags.Bool("json", false, "emit machine-readable result")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 2 || *threshold < 0 || *threshold > 255 {
			return errors.New("usage: pixlog recipe infer [--asset <path>] [--threshold <0-255>] [--json] <before> <after>")
		}
		beforePath, afterPath := flags.Arg(0), flags.Arg(1)
		if *assetPath == "" {
			*assetPath = afterPath
		}
		result, err := recipe.InferFiles(beforePath, afterPath, imaging.DiffOptions{Threshold: uint8(*threshold)})
		if err != nil {
			return err
		}
		assetOID, err := repository.HashFile(*assetPath)
		if err != nil {
			return err
		}
		if assetOID != result.OutputOID {
			return errors.New("the recipe target does not contain the exact AFTER bytes")
		}
		repo, err := repository.OpenGit("")
		if err != nil {
			return err
		}
		store, err := repository.OpenGitMediaStore(repo.Root)
		if err != nil {
			return err
		}
		for _, path := range []string{beforePath, afterPath} {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err := store.Put(data); err != nil {
				return err
			}
		}
		recipeOID, err := repo.ImportRecipe(*assetPath, result.Recipe)
		if err != nil {
			return err
		}
		output := map[string]any{
			"recipe_oid": recipeOID, "input_oid": result.InputOID,
			"output_oid": result.OutputOID, "operation": result.Operation,
			"confidence": result.Confidence,
		}
		if *asJSON {
			return writeJSON(stdout, output)
		}
		fmt.Fprintf(stdout, "Inferred %s (confidence %.2f), recipe %s\n", result.Operation, result.Confidence, repository.ShortOID(recipeOID))
		return nil
	default:
		return fmt.Errorf("unknown recipe command %q", args[0])
	}
}

func runRemote(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "list" {
		if len(args) > 1 {
			return errors.New("usage: pixlog remote [list]")
		}
		repo, err := repository.Open("")
		if err != nil {
			return err
		}
		names := make([]string, 0, len(repo.Config.Remotes))
		for name := range repo.Config.Remotes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(stdout, "%s\t%s\n", name, repo.Config.Remotes[name].URL)
		}
		return nil
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) != 3 {
			return errors.New("usage: pixlog remote add <name> <path-or-file-url>")
		}
		if err := repo.AddRemote(args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "remote %s -> %s\n", args[1], args[2])
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return errors.New("usage: pixlog remote remove <name>")
		}
		if err := repo.RemoveRemote(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "removed remote %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown remote command %q", args[0])
	}
}

func runSync(action string, args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet(action, stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: pixlog %s [--json] [remote]", action)
	}
	remote := "origin"
	if flags.NArg() == 1 {
		remote = flags.Arg(0)
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	var result repository.SyncResult
	switch action {
	case "push":
		result, err = repo.Push(remote)
	case "fetch":
		result, err = repo.Fetch(remote)
	case "pull":
		result, err = repo.Pull(remote)
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "%s %s/%s\n", action, result.Remote, result.Branch)
	fmt.Fprintf(stdout, "  objects: %d copied\n", result.CopiedObjects)
	if result.RemoteHead != "" {
		fmt.Fprintf(stdout, "  remote:  %s\n", repository.ShortOID(result.RemoteHead))
	}
	if result.LocalHead != "" {
		fmt.Fprintf(stdout, "  local:   %s\n", repository.ShortOID(result.LocalHead))
	}
	return nil
}

func runClone(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("clone", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return errors.New("usage: pixlog clone [--json] <remote> [directory]")
	}
	target := ""
	if flags.NArg() == 2 {
		target = flags.Arg(1)
	}
	repo, result, err := repository.Clone(flags.Arg(0), target)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, map[string]any{"repository": repo.Root, "sync": result})
	}
	fmt.Fprintf(stdout, "Cloned %s into %s\n", flags.Arg(0), repo.Root)
	fmt.Fprintf(stdout, " %d objects, HEAD %s\n", result.CopiedObjects, repository.ShortOID(result.LocalHead))
	return nil
}

func runVerify(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("verify", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog verify [--json]")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	result, err := repo.VerifyObjects()
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	if len(result.Corrupt) > 0 {
		fmt.Fprintf(stdout, "verified %d objects: %d corrupt\n", result.Objects, len(result.Corrupt))
		for _, path := range result.Corrupt {
			fmt.Fprintf(stdout, "  corrupt %s\n", path)
		}
		return errors.New("object verification failed")
	}
	fmt.Fprintf(stdout, "verified %d objects: all hashes valid\n", result.Objects)
	return nil
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("doctor", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog doctor [--json]")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	result, err := repo.Doctor()
	if err != nil {
		return err
	}
	if *asJSON {
		if err := writeJSON(stdout, result); err != nil {
			return err
		}
	} else {
		for _, check := range result.Checks {
			status := "PASS"
			if !check.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(stdout, "%-4s %-12s %s\n", status, check.Name, check.Detail)
		}
	}
	if !result.Passed {
		return errors.New("PixLog doctor found configuration or object errors")
	}
	return nil
}

func runInspect(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("inspect", stderr)
	revision := flags.String("revision", "", "inspect a commit instead of the index")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	preview := flags.Bool("preview", false, "force an inline image preview with Chafa")
	noPreview := flags.Bool("no-preview", false, "disable automatic terminal image preview")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog inspect [--revision <rev>] [--json] [--preview | --no-preview] <asset>")
	}
	previewer, showPreview, err := terminalPreviewer(stdout, *preview, *noPreview, *asJSON)
	if err != nil {
		return err
	}
	repo, err := openInspectionRepository()
	if err != nil {
		return err
	}
	inspection, err := repo.Inspect(*revision, flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, inspection)
	}
	fmt.Fprintf(stdout, "%s @ %s\n", inspection.Entry.Path, inspection.Revision)
	fmt.Fprintf(stdout, "  content     %s\n", inspection.Entry.ContentOID)
	fmt.Fprintf(stdout, "  manifest    %s\n", inspection.Entry.ManifestOID)
	fmt.Fprintf(stdout, "  recipe      %s\n", displayEmpty(inspection.Entry.RecipeOID))
	fmt.Fprintf(stdout, "  format      %s (%s)\n", inspection.Manifest.Format, inspection.Manifest.MediaType)
	fmt.Fprintf(stdout, "  dimensions  %dx%d\n", inspection.Manifest.Width, inspection.Manifest.Height)
	fmt.Fprintf(stdout, "  size        %d bytes\n", inspection.Manifest.Size)
	fmt.Fprintf(stdout, "  visual hash %s\n", displayEmpty(inspection.Manifest.VisualHash))
	if len(inspection.Manifest.EmbeddedMetadata) > 0 {
		fmt.Fprintln(stdout, "  embedded metadata:")
		keys := make([]string, 0, len(inspection.Manifest.EmbeddedMetadata))
		for key := range inspection.Manifest.EmbeddedMetadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(stdout, "    %s: %s\n", key, inspection.Manifest.EmbeddedMetadata[key])
		}
	}
	if showPreview {
		return renderDiffPreviews(previewer, stdout, stderr, repository.DiffReport{Assets: []repository.AssetDiff{{
			Path:    inspection.Entry.Path,
			Preview: &repository.AssetPreview{After: inspection.Preview},
		}}})
	}
	return nil
}

func runRestore(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("restore", stderr)
	source := flags.String("source", "", "restore from a commit instead of the index")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	restored, err := repo.Restore(*source, flags.Args())
	if err != nil {
		return err
	}
	for _, path := range restored {
		fmt.Fprintf(stdout, "restore %s\n", path)
	}
	return nil
}

func runLineage(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("lineage", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	asGraph := flags.Bool("graph", false, "traverse recipe inputs, outputs, models, and vendor payloads")
	verify := flags.Bool("verify", false, "fail when a referenced object is missing or corrupt")
	hydrate := flags.Bool("hydrate", false, "fetch missing referenced objects from the origin endpoint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog lineage [--graph] [--verify] [--hydrate] [--json] <asset>")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	if *asGraph || *verify || *hydrate {
		graph, err := repo.ProvenanceGraph(flags.Arg(0), *hydrate)
		if err != nil {
			return err
		}
		if *asJSON {
			if err := writeJSON(stdout, graph); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(stdout, "%s: %d nodes, %d edges\n", graph.Asset, len(graph.Nodes), len(graph.Edges))
			for _, edge := range graph.Edges {
				fmt.Fprintf(stdout, "  %s -> %s  %s\n", repository.ShortOID(edge.From), repository.ShortOID(edge.To), edge.Role)
			}
			for _, oid := range graph.Missing {
				fmt.Fprintf(stdout, "  missing %s\n", oid)
			}
		}
		if *verify && len(graph.Missing) > 0 {
			return fmt.Errorf("lineage verification found %d missing object(s)", len(graph.Missing))
		}
		return nil
	}
	nodes, err := repo.Lineage(flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, nodes)
	}
	for index, node := range nodes {
		prefix := "*"
		if index > 0 {
			prefix = "|"
		}
		fmt.Fprintf(stdout, "%s %s  %s\n", prefix, repository.ShortOID(node.CommitOID), node.Message)
		fmt.Fprintf(stdout, "  content %s", repository.ShortOID(node.ContentOID))
		if node.RecipeOID != "" {
			fmt.Fprintf(stdout, "  recipe %s", repository.ShortOID(node.RecipeOID))
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func runReproduce(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("reproduce", stderr)
	revision := flags.String("revision", "HEAD", "recipe revision")
	execute := flags.Bool("execute", false, "execute a validated captured command")
	baseURL := flags.String("base-url", "", "explicit provider base URL for exact-request replay")
	authEnv := flags.String("auth-env", "", "environment variable containing a provider bearer token")
	responseOutput := flags.String("response-output", "", "write a replay response body to this path")
	timeout := flags.Duration("timeout", 2*time.Minute, "provider request timeout")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog reproduce [--revision <rev>] [--execute] [--base-url <url>] [--auth-env <name>] [--response-output <path>] [--json] <asset>")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	plan, err := repo.PlanReproduction(*revision, flags.Arg(0))
	if err != nil {
		return err
	}
	if !*execute {
		if *asJSON {
			return writeJSON(stdout, plan)
		}
		fmt.Fprintf(stdout, "Recipe:    %s\n", plan.RecipeOID)
		fmt.Fprintf(stdout, "Asset:     %s @ %s\n", plan.Asset, plan.Revision)
		fmt.Fprintf(stdout, "Kind:      %s\n", plan.Kind)
		if plan.Request != nil {
			fmt.Fprintf(stdout, "Request:   %s %s (%s)\n", plan.Request.Method, plan.Request.Path, plan.Request.Provider)
			fmt.Fprintf(stdout, "Payload:   %s\n", plan.Request.RequestOID)
			return nil
		}
		if plan.Command == nil {
			fmt.Fprintln(stdout, "Command:   (adapter required)")
			return nil
		}
		fmt.Fprintf(stdout, "Command:   %s %s\n", plan.Command.Executable, strings.Join(plan.Command.Arguments, " "))
		fmt.Fprintf(stdout, "Directory: %s\n", plan.Command.WorkingDirectory)
		return nil
	}
	if *asJSON {
		return errors.New("--json cannot be combined with --execute")
	}
	if plan.Request != nil {
		if *baseURL == "" {
			return errors.New("--base-url is required to replay a captured provider request")
		}
		return executeCapturedRequest(repo, plan, *baseURL, *authEnv, *responseOutput, *timeout, stdout)
	}
	if err := repo.ValidateReproduction(plan); err != nil {
		return err
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	workingDirectory := filepath.Join(repo.Root, filepath.FromSlash(plan.Command.WorkingDirectory))
	if err := os.Chdir(workingDirectory); err != nil {
		return fmt.Errorf("enter reproduction working directory: %w", err)
	}
	defer os.Chdir(previousDirectory)
	command := append([]string{"--kind", "reproduction", "--", plan.Command.Executable}, plan.Command.Arguments...)
	return runCapturedCommand(command, stdout, stderr)
}

func runBlame(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("blame", stderr)
	point := flags.String("point", "", "image coordinate in x,y form")
	threshold := flags.Int("threshold", 8, "per-channel change threshold from 0 to 255")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *point == "" {
		return errors.New("usage: pixlog blame --point <x,y> [--json] <asset>")
	}
	if *threshold < 0 || *threshold > 255 {
		return errors.New("threshold must be between 0 and 255")
	}
	var x, y int
	if _, err := fmt.Sscanf(*point, "%d,%d", &x, &y); err != nil {
		return fmt.Errorf("invalid point %q; expected x,y", *point)
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	result, err := repo.BlamePoint(flags.Arg(0), x, y, imaging.DiffOptions{Threshold: uint8(*threshold)})
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Commit:      %s\n", result.CommitOID)
	fmt.Fprintf(stdout, "Author:      %s\n", result.Author)
	fmt.Fprintf(stdout, "Date:        %s\n", result.CreatedAt.Local().Format(time.RFC1123Z))
	fmt.Fprintf(stdout, "Change:      %s\n", result.Message)
	fmt.Fprintf(stdout, "Recipe:      %s\n", displayEmpty(result.RecipeOID))
	fmt.Fprintf(stdout, "Region:      %d,%d %dx%d\n", result.Region.X, result.Region.Y, result.Region.Width, result.Region.Height)
	fmt.Fprintf(stdout, "Confidence:  %s\n", result.Confidence)
	return nil
}

func runBranch(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("branch", stderr)
	deleteName := flags.String("delete", "", "delete a branch")
	flags.StringVar(deleteName, "d", "", "delete a branch")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	if *deleteName != "" {
		if flags.NArg() != 0 {
			return errors.New("usage: pixlog branch --delete <name>")
		}
		if err := repo.DeleteBranch(*deleteName); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Deleted branch %s\n", *deleteName)
		return nil
	}
	if flags.NArg() == 0 {
		branches, err := repo.ListBranches()
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(stdout, branches)
		}
		for _, branch := range branches {
			marker := " "
			if branch.Current {
				marker = "*"
			}
			fmt.Fprintf(stdout, "%s %-20s %s\n", marker, branch.Name, repository.ShortOID(branch.OID))
		}
		return nil
	}
	if flags.NArg() > 2 {
		return errors.New("usage: pixlog branch <name> [start-revision]")
	}
	revision := "HEAD"
	if flags.NArg() == 2 {
		revision = flags.Arg(1)
	}
	branch, err := repo.CreateBranch(flags.Arg(0), revision)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created branch %s at %s\n", branch.Name, repository.ShortOID(branch.OID))
	return nil
}

func runSwitch(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("switch", stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog switch <branch>")
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	if err := repo.SwitchBranch(flags.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Switched to branch %s\n", flags.Arg(0))
	return nil
}

func runTag(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("tag", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	if flags.NArg() == 0 {
		tags, err := repo.ListTags()
		if err != nil {
			return err
		}
		if *asJSON {
			return writeJSON(stdout, tags)
		}
		for _, tag := range tags {
			fmt.Fprintf(stdout, "%-20s %s\n", tag.Name, repository.ShortOID(tag.OID))
		}
		return nil
	}
	if flags.NArg() > 2 {
		return errors.New("usage: pixlog tag <name> [revision]")
	}
	revision := "HEAD"
	if flags.NArg() == 2 {
		revision = flags.Arg(1)
	}
	tag, err := repo.CreateTag(flags.Arg(0), revision)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created tag %s at %s\n", tag.Name, repository.ShortOID(tag.OID))
	return nil
}

func runLock(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("lock", stderr)
	owner := flags.String("owner", "", "lock owner; defaults to the current PixLog author")
	remote := flags.String("remote", "", "acquire the lock on a configured remote")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog lock [--owner <name>] [--remote <name>] [--json] <asset>")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	lock, err := repo.AcquireLock(flags.Arg(0), *owner, *remote)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, lock)
	}
	location := "local repository"
	if lock.Remote != "" {
		location = "remote " + lock.Remote
	}
	fmt.Fprintf(stdout, "Locked %s for %s on %s\n", lock.Path, lock.Owner, location)
	return nil
}

func runUnlock(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("unlock", stderr)
	owner := flags.String("owner", "", "lock owner; defaults to the current PixLog author")
	remote := flags.String("remote", "", "release the lock on a configured remote")
	force := flags.Bool("force", false, "release a lock owned by another author")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog unlock [--owner <name>] [--remote <name>] [--force] [--json] <asset>")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	lock, err := repo.ReleaseLock(flags.Arg(0), *owner, *remote, *force)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, lock)
	}
	fmt.Fprintf(stdout, "Unlocked %s (owner %s)\n", lock.Path, lock.Owner)
	return nil
}

func runLocks(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("locks", stderr)
	remote := flags.String("remote", "", "list locks on a configured remote")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog locks [--remote <name>] [--json]")
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	locks, err := repo.ListLocks(*remote)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, locks)
	}
	for _, lock := range locks {
		fmt.Fprintf(stdout, "%-32s %-20s %s\n", lock.Path, lock.Owner, lock.CreatedAt.Local().Format(time.RFC3339))
	}
	return nil
}

func runCheck(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("check", stderr)
	policyPath := flags.String("policy", ".pixlog-policy.json", "path to the JSON policy file")
	revisionRange := flags.String("range", "", "check a Git revision range: <base>..<head> or <base>...<head>")
	threshold := flags.Int("threshold", 8, "per-channel visual change threshold from 0 to 255")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog check [--policy <path>] [--range <base>..<head>] [--threshold <0-255>] [--json]")
	}
	if *threshold < 0 || *threshold > 255 {
		return errors.New("threshold must be between 0 and 255")
	}
	options := imaging.DiffOptions{Threshold: uint8(*threshold)}
	var result repository.PolicyCheckResult
	var err error
	if *revisionRange != "" {
		gitRepo, openErr := repository.OpenGit("")
		if openErr != nil {
			return openErr
		}
		result, err = gitRepo.CheckPolicyRange(*policyPath, *revisionRange, options)
	} else {
		repo, openErr := openRepositoryView()
		if openErr != nil {
			return openErr
		}
		result, err = repo.CheckPolicy(*policyPath, options)
	}
	if err != nil {
		return err
	}
	if *asJSON {
		if err := writeJSON(stdout, result); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "policy %s\n", result.Policy)
		fmt.Fprintf(stdout, "checked %d asset(s)\n", len(result.CheckedAssets))
		for _, violation := range result.Violations {
			fmt.Fprintf(stdout, "  FAIL %s [rule %d, %s] %s\n", violation.Asset, violation.Rule, violation.Code, violation.Message)
		}
		if result.Passed {
			fmt.Fprintln(stdout, "policy check passed")
		}
	}
	if !result.Passed {
		return fmt.Errorf("policy check failed with %d violation(s)", len(result.Violations))
	}
	return nil
}

type commandCaptureResult struct {
	RecipeOID  string   `json:"recipe_oid,omitempty"`
	Modified   []string `json:"modified"`
	Deleted    []string `json:"deleted"`
	DurationMS int64    `json:"duration_ms"`
}

func runCapturedCommand(args []string, stdout, stderr io.Writer) error {
	preSeparator, command := splitSeparator(args)
	flags := newFlagSet("run", stderr)
	kind := flags.String("kind", "command-edit", "recipe kind")
	redactArgs := flags.Bool("redact-args", false, "replace command arguments in the stored recipe")
	asJSON := flags.Bool("json", false, "emit a machine-readable capture result")
	if err := flags.Parse(preSeparator); err != nil {
		return err
	}
	if flags.NArg() != 0 || len(command) == 0 {
		return errors.New("usage: pixlog run [--kind <kind>] [--redact-args] [--json] -- <command> [args...]")
	}
	if strings.TrimSpace(*kind) == "" {
		return errors.New("recipe kind cannot be empty")
	}

	repo, err := openCommandCaptureRepository()
	if err != nil {
		return err
	}
	sourceControl, err := repository.DiscoverGitContext(repo.RootPath())
	if err != nil {
		return err
	}
	before, err := repo.SnapshotAssets()
	if err != nil {
		return err
	}
	entriesBefore, err := repo.CaptureEntries()
	if err != nil {
		return err
	}

	startedAt := time.Now().UTC()
	child := exec.Command(command[0], command[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = stdout
	child.Stderr = stderr
	if err := child.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with status %d; image changes were not staged", exitError.ExitCode())
		}
		return fmt.Errorf("run command: %w", err)
	}
	finishedAt := time.Now().UTC()

	after, err := repo.SnapshotAssets()
	if err != nil {
		return err
	}
	delta := repository.CompareAssetSnapshots(before, after)
	trackedDeleted := make([]string, 0, len(delta.Deleted))
	for _, path := range delta.Deleted {
		if _, tracked := entriesBefore[path]; tracked {
			trackedDeleted = append(trackedDeleted, path)
		}
	}

	result := commandCaptureResult{
		Modified:   delta.Modified,
		Deleted:    trackedDeleted,
		DurationMS: finishedAt.Sub(startedAt).Milliseconds(),
	}
	if len(delta.Modified) == 0 && len(trackedDeleted) == 0 {
		if *asJSON {
			return writeJSON(stdout, result)
		}
		fmt.Fprintln(stdout, "command completed; no image changes detected")
		return nil
	}

	recordedArguments := append([]string(nil), command[1:]...)
	if *redactArgs {
		for index := range recordedArguments {
			recordedArguments[index] = "<redacted>"
		}
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	relativeWorkingDirectory, err := filepath.Rel(repo.RootPath(), workingDirectory)
	if err != nil {
		return err
	}

	parents := []map[string]any{}
	for _, path := range append(append([]string{}, delta.Modified...), trackedDeleted...) {
		if entry, exists := entriesBefore[path]; exists {
			parent := map[string]any{"asset": entry.ContentOID, "path": path, "role": "previous-version"}
			if entry.RecipeOID != "" {
				parent["recipe"] = entry.RecipeOID
			}
			parents = append(parents, parent)
		}
	}
	outputs := make([]map[string]any, 0, len(delta.Modified))
	for _, path := range delta.Modified {
		outputs = append(outputs, map[string]any{"asset": after[path], "path": path})
	}
	recipeDocument := map[string]any{
		"schema": recipe.Schema,
		"kind":   strings.TrimSpace(*kind),
		"tool": map[string]any{
			"name": filepath.Base(command[0]),
		},
		"command": map[string]any{
			"executable":        command[0],
			"arguments":         recordedArguments,
			"working_directory": filepath.ToSlash(relativeWorkingDirectory),
			"started_at":        startedAt,
			"finished_at":       finishedAt,
			"exit_code":         0,
		},
		"environment": map[string]any{
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		},
		"capture": map[string]any{
			"source": "pixlog-run",
		},
		"parents":         parents,
		"outputs":         outputs,
		"deleted_outputs": trackedDeleted,
	}
	if sourceControl != nil {
		recipeDocument["source_control"] = sourceControl
	}
	recipeData, err := json.Marshal(recipeDocument)
	if err != nil {
		return fmt.Errorf("encode command recipe: %w", err)
	}
	result.RecipeOID, err = repo.ApplyCommandCapture(delta.Modified, trackedDeleted, recipeData)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(stdout, result)
	}
	if result.RecipeOID != "" {
		fmt.Fprintf(stdout, "recipe %s\n", repository.ShortOID(result.RecipeOID))
	}
	for _, path := range result.Modified {
		fmt.Fprintf(stdout, "add %s\n", path)
	}
	for _, path := range result.Deleted {
		fmt.Fprintf(stdout, "rm  %s\n", path)
	}
	return nil
}

func runBisect(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("bisect", stderr)
	asset := flags.String("asset", "", "tracked asset path")
	baseline := flags.String("against", "", "baseline image path")
	metric := flags.String("metric", "ssim", "metric: ssim, rmse, or change")
	threshold := flags.Float64("threshold", 0.98, "metric threshold from 0 to 1")
	pixelThreshold := flags.Int("pixel-threshold", 8, "per-channel pixel threshold from 0 to 255")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *asset == "" || *baseline == "" {
		return errors.New("usage: pixlog bisect --asset <path> --against <baseline> [--metric <ssim|rmse|change>] [--threshold <0-1>] [--json]")
	}
	if *pixelThreshold < 0 || *pixelThreshold > 255 {
		return errors.New("pixel threshold must be between 0 and 255")
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	result, err := repo.VisualBisect(*asset, *baseline, *metric, *threshold, imaging.DiffOptions{Threshold: uint8(*pixelThreshold)})
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	if !result.Found {
		fmt.Fprintf(stdout, "no %s threshold crossing found for %s\n", result.Metric, result.Path)
		return nil
	}
	fmt.Fprintf(stdout, "Commit:      %s\n", result.CommitOID)
	fmt.Fprintf(stdout, "Author:      %s\n", result.Author)
	fmt.Fprintf(stdout, "Date:        %s\n", result.CreatedAt.Local().Format(time.RFC1123Z))
	fmt.Fprintf(stdout, "Change:      %s\n", result.Message)
	fmt.Fprintf(stdout, "Metric:      %s %.6f (threshold %.6f)\n", result.Metric, result.Value, result.Threshold)
	fmt.Fprintf(stdout, "Content:     %s\n", displayEmpty(result.ContentOID))
	return nil
}

func printChanges(writer io.Writer, heading string, changes []repository.Change) {
	if len(changes) == 0 {
		return
	}
	fmt.Fprintf(writer, "\n%s:\n", heading)
	for _, change := range changes {
		fmt.Fprintf(writer, "  %-8s %s\n", change.Kind, change.Path)
	}
}

func printDiff(writer io.Writer, report repository.DiffReport) {
	fmt.Fprintf(writer, "diff %s -> %s\n", report.From, report.To)
	if len(report.Assets) == 0 {
		fmt.Fprintln(writer, "no image changes")
		return
	}
	for _, asset := range report.Assets {
		marker := map[repository.ChangeKind]string{
			repository.ChangeAdded:    "+",
			repository.ChangeModified: "M",
			repository.ChangeDeleted:  "-",
		}[asset.Kind]
		fmt.Fprintf(writer, "\n%s %s\n", marker, asset.Path)
		for _, change := range asset.Metadata {
			fmt.Fprintf(writer, "  %-24s %v -> %v\n", change.Field, displayEmpty(change.Old), displayEmpty(change.New))
		}
		if asset.Visual != nil {
			fmt.Fprintf(writer, "  geometry                 %s (%.0f%% confidence)\n", asset.Visual.Geometry.Type, asset.Visual.Geometry.Confidence*100)
			fmt.Fprintf(writer, "  visual change            %.2f%% (%d/%d sampled pixels)\n", asset.Visual.VisualChangeRatio*100, asset.Visual.ChangedPixels, asset.Visual.ComparedPixels)
			fmt.Fprintf(writer, "  RMSE / SSIM              %.5f / %.5f\n", asset.Visual.RMSE, asset.Visual.SSIM)
			for _, region := range asset.Visual.Regions {
				box := region.BoundingBox
				fmt.Fprintf(writer, "  changed region           %d,%d %dx%d (%d pixels)\n", box.X, box.Y, box.Width, box.Height, region.Pixels)
			}
		}
		if asset.Note != "" {
			fmt.Fprintf(writer, "  note                     %s\n", asset.Note)
		}
	}
}

func splitSeparator(args []string) ([]string, []string) {
	for index, argument := range args {
		if argument == "--" {
			return args[:index], args[index+1:]
		}
	}
	return args, nil
}

func displayEmpty(value any) any {
	if value == nil || value == "" {
		return "(none)"
	}
	return value
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return flags
}

func openRepositoryView() (repositoryView, error) {
	gitRepo, err := repository.OpenGit("")
	if err == nil {
		return gitRepo, nil
	}
	if errors.Is(err, repository.ErrNotGitRepository) {
		return nil, errors.New("not a Git repository (or any parent directory)")
	}
	return nil, err
}

func openAssetAdder() (assetAdder, error) {
	return repository.OpenGit("")
}

func openCommandCaptureRepository() (commandCaptureRepository, error) {
	return repository.OpenGit("")
}

func openRecipeRepository() (recipeRepository, error) {
	return repository.OpenGit("")
}

func openInspectionRepository() (inspectionRepository, error) {
	return repository.OpenGit("")
}

func printHelp(writer io.Writer) {
	help := `PixLog - Git media, visual history, and generation provenance

Usage:
  pixlog <command> [options]
	git pixlog <command> [options]

Setup:
	init       Initialize Git if needed and install PixLog
	install    Refresh attributes, drivers, config, and pre-push hook
	track      Add PixLog patterns to .gitattributes
	git        Run any Git command without PixLog-specific routing

Image and provenance:
	add        Inspect and stage image assets through Git
	status     Show Git-backed image state; porcelain options proxy Git
	diff       Inspect byte, metadata, recipe, and visual changes
	compare    Compare two image files directly
	inspect    Show an asset manifest and provenance IDs
	recipe     Import, inspect, diff, or infer provenance recipes
	run        Capture a command and stage its changed image outputs
	capture    Guide adapters; run daemon/proxy sessions; import history; finalize recipes
	metadata   Inspect or import EXIF, XMP, ICC, IPTC, PNG, and C2PA metadata
	c2pa       Verify, import, export, and sign Content Credentials with c2patool
	reproduce  Plan or execute guarded command and exact-request reproduction
	lineage    Show Git history or recursively verify/hydrate the provenance graph
	blame      Find the Git commit that last changed an image point

Media and collaboration:
	hydrate    Restore tracked pointer assets to exact image bytes
	dehydrate  Replace worktree image bytes with their tracked pointers
	verify     Verify local content-addressed objects and pointer references
	doctor     Check repository integration and object integrity
	lock       Lock a tracked binary asset locally or on a file endpoint
	unlock     Release a binary asset lock
	locks      List active binary asset locks
	check      Enforce staged or Git-range image policy rules

Git proxies:
	rm commit log show restore checkout branch switch tag remote
	push fetch pull clone merge bisect rebase cherry-pick reset revert

These commands preserve Git arguments and exit codes. pixlog push ensures the
PixLog pre-push hook is installed before running Git.

Other commands:
  version    Print the PixLog version
  help       Show this help
`
	fmt.Fprint(writer, strings.TrimLeft(help, "\n"))
}
