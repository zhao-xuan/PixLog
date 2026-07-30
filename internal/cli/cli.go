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

const Version = "0.1.0-dev"

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
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
	case "add":
		err = runAdd(args[1:], stdout, stderr)
	case "rm":
		err = runRemove(args[1:], stdout, stderr)
	case "status":
		err = runStatus(args[1:], stdout, stderr)
	case "diff":
		err = runDiff(args[1:], stdout, stderr)
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
	case "inspect":
		err = runInspect(args[1:], stdout, stderr)
	case "restore":
		err = runRestore(args[1:], stdout, stderr)
	case "lineage":
		err = runLineage(args[1:], stdout, stderr)
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
	case "bisect":
		err = runBisect(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "pixlog: unknown command %q\n", args[0])
		fmt.Fprintln(stderr, "Run 'pixlog help' for usage.")
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "pixlog: %v\n", err)
		return 1
	}
	return 0
}

func runInit(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("init", stderr)
	bare := flags.Bool("bare", false, "create a bare repository")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("usage: pixlog init [--bare] [path]")
	}
	path := "."
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	repo, err := repository.Init(path, *bare)
	if err != nil {
		return err
	}
	kind := "repository"
	if *bare {
		kind = "bare repository"
	}
	fmt.Fprintf(stdout, "Initialized empty PixLog %s in %s\n", kind, repo.Control)
	return nil
}

func runAdd(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("add", stderr)
	recipeOID := flags.String("recipe", "", "attach an existing recipe object ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := repository.Open("")
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
	flags := newFlagSet("status", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog status [--json]")
	}
	repo, err := repository.Open("")
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

	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	options := imaging.DiffOptions{Threshold: uint8(*threshold)}
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
		return nil
	}
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
		return errors.New("usage: pixlog recipe <import|show|diff> ...")
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
		repo, err := repository.Open("")
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
		repo, err := repository.Open("")
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
		repo, err := repository.Open("")
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
	repo, err := repository.Open("")
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

func runInspect(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("inspect", stderr)
	revision := flags.String("revision", "", "inspect a commit instead of the index")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog inspect [--revision <rev>] [--json] <asset>")
	}
	repo, err := repository.Open("")
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog lineage [--json] <asset>")
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
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
	repo, err := repository.Open("")
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
	repo, err := repository.Open("")
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
	repo, err := repository.Open("")
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
	repo, err := repository.Open("")
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
	threshold := flags.Int("threshold", 8, "per-channel visual change threshold from 0 to 255")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog check [--policy <path>] [--threshold <0-255>] [--json]")
	}
	if *threshold < 0 || *threshold > 255 {
		return errors.New("threshold must be between 0 and 255")
	}
	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	result, err := repo.CheckPolicy(*policyPath, imaging.DiffOptions{Threshold: uint8(*threshold)})
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

	repo, err := repository.Open("")
	if err != nil {
		return err
	}
	before, err := repo.SnapshotAssets()
	if err != nil {
		return err
	}
	indexBefore, err := repo.ReadIndex()
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
		if _, tracked := indexBefore.Entries[path]; tracked {
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
	relativeWorkingDirectory, err := filepath.Rel(repo.Root, workingDirectory)
	if err != nil {
		return err
	}

	parents := []map[string]any{}
	for _, path := range append(append([]string{}, delta.Modified...), trackedDeleted...) {
		if entry, exists := indexBefore.Entries[path]; exists {
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
	recipeData, err := json.Marshal(recipeDocument)
	if err != nil {
		return fmt.Errorf("encode command recipe: %w", err)
	}
	if len(delta.Modified) > 0 {
		result.RecipeOID, err = repo.StoreRecipe(recipeData)
		if err != nil {
			return err
		}
		absolutePaths := make([]string, 0, len(delta.Modified))
		for _, path := range delta.Modified {
			absolutePaths = append(absolutePaths, filepath.Join(repo.Root, filepath.FromSlash(path)))
		}
		if _, err := repo.Add(absolutePaths, result.RecipeOID); err != nil {
			return err
		}
	}
	if len(trackedDeleted) > 0 {
		absolutePaths := make([]string, 0, len(trackedDeleted))
		for _, path := range trackedDeleted {
			absolutePaths = append(absolutePaths, filepath.Join(repo.Root, filepath.FromSlash(path)))
		}
		if _, err := repo.Remove(absolutePaths); err != nil {
			return err
		}
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

func printHelp(writer io.Writer) {
	help := `PixLog - Git-like version control for images and generation provenance

Usage:
  pixlog <command> [options]

Repository commands:
  init       Create a PixLog repository
  add        Stage image assets
  rm         Stage tracked asset removal
  status     Show index and working tree state
	diff       Inspect byte, metadata, and visual changes
  commit     Record the staged image tree
  log        Show commit history
	recipe     Import, inspect, and compare generation recipes
	remote     Configure a local or file:// remote
	push       Upload missing objects, then update the remote ref
	fetch      Download and verify remote objects
	pull       Fast-forward and restore the remote image tree
	clone      Clone a PixLog repository
	verify     Verify every content-addressed object
	inspect    Show an asset manifest and provenance IDs
	restore    Restore image bytes from the index or a commit
	lineage    Show an asset's version and recipe lineage
	blame      Find the commit that last changed an image point
	branch     List, create, or delete branches
	switch     Switch branches and restore their image tree
	tag        List or create immutable release refs
	lock       Lock a tracked binary asset locally or on a remote
	unlock     Release a binary asset lock
	locks      List active binary asset locks
	check      Enforce staged image policy rules for CI
	run        Capture a command and stage its changed image outputs
	bisect     Find the first image revision crossing a visual threshold

Other commands:
  version    Print the PixLog version
  help       Show this help
`
	fmt.Fprint(writer, strings.TrimLeft(help, "\n"))
}
