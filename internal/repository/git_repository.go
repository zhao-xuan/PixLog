package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhao-xuan/PixLog/internal/imaging"
	"github.com/zhao-xuan/PixLog/internal/recipe"
)

var ErrNotGitRepository = errors.New("not a Git repository (or any parent directory)")

type GitRepository struct {
	Root string
}

func OpenGit(start string) (*GitRepository, error) {
	root, err := gitOutput(start, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || gitExitCode(err) == 128 {
			return nil, ErrNotGitRepository
		}
		return nil, fmt.Errorf("discover Git repository: %w", err)
	}
	return &GitRepository{Root: filepath.Clean(root)}, nil
}

func (g *GitRepository) relativePath(filePath string) (string, error) {
	absolute, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", filePath, err)
	}
	root := g.Root
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return "", fmt.Errorf("make %s relative to Git repository: %w", filePath, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s is outside the repository", filePath)
	}
	return filepath.ToSlash(filepath.Clean(relative)), nil
}

func (g *GitRepository) Status() (Status, error) {
	headOID, err := gitOptionalOutput(g.Root, []int{128}, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Status{}, err
	}
	branch, err := gitOptionalOutput(g.Root, []int{1}, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return Status{}, err
	}
	if branch == "" {
		branch = "(detached)"
	}

	head := emptySnapshot("HEAD")
	if headOID != "" {
		head, err = g.revisionSnapshot("HEAD", "HEAD")
		if err != nil {
			return Status{}, err
		}
	}
	index, err := g.indexSnapshot("index")
	if err != nil {
		return Status{}, err
	}
	worktree, err := g.worktreeSnapshot(index)
	if err != nil {
		return Status{}, err
	}
	untracked, err := g.untrackedAssets()
	if err != nil {
		return Status{}, err
	}
	return Status{
		Branch:    branch,
		Head:      headOID,
		Staged:    CompareTrees(head.entries, index.entries),
		Unstaged:  CompareTrees(index.entries, worktree.entries),
		Untracked: untracked,
	}, nil
}

func (g *GitRepository) DiffWorking(paths []string, options imaging.DiffOptions) (DiffReport, error) {
	index, err := g.indexSnapshot("index")
	if err != nil {
		return DiffReport{}, err
	}
	worktree, err := g.worktreeSnapshot(index)
	if err != nil {
		return DiffReport{}, err
	}
	return g.diff(index, worktree, paths, options)
}

func (g *GitRepository) DiffStaged(paths []string, options imaging.DiffOptions) (DiffReport, error) {
	headOID, err := gitOptionalOutput(g.Root, []int{128}, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return DiffReport{}, err
	}
	head := emptySnapshot("HEAD")
	if headOID != "" {
		head, err = g.revisionSnapshot("HEAD", "HEAD")
		if err != nil {
			return DiffReport{}, err
		}
	}
	index, err := g.indexSnapshot("index")
	if err != nil {
		return DiffReport{}, err
	}
	return g.diff(head, index, paths, options)
}

func (g *GitRepository) DiffCommits(oldRevision, newRevision string, paths []string, options imaging.DiffOptions) (DiffReport, error) {
	oldSnapshot, err := g.revisionSnapshot(oldRevision, oldRevision)
	if err != nil {
		return DiffReport{}, err
	}
	newSnapshot, err := g.revisionSnapshot(newRevision, newRevision)
	if err != nil {
		return DiffReport{}, err
	}
	return g.diff(oldSnapshot, newSnapshot, paths, options)
}

func (g *GitRepository) CheckPolicy(filePath string, options imaging.DiffOptions) (PolicyCheckResult, error) {
	headOID, err := gitOptionalOutput(g.Root, []int{128}, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return PolicyCheckResult{}, err
	}
	head := emptySnapshot("HEAD")
	if headOID != "" {
		head, err = g.revisionSnapshot("HEAD", "HEAD")
		if err != nil {
			return PolicyCheckResult{}, err
		}
	}
	index, err := g.indexSnapshot("index")
	if err != nil {
		return PolicyCheckResult{}, err
	}
	diff, err := g.diff(head, index, nil, options)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	return g.checkPolicySnapshots(filePath, head, index, diff, options)
}

func (g *GitRepository) CheckPolicyRange(filePath, revisionRange string, options imaging.DiffOptions) (PolicyCheckResult, error) {
	oldRevision, newRevision, err := g.resolveRange(revisionRange)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	oldSnapshot, err := g.revisionSnapshot(oldRevision, oldRevision)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	newSnapshot, err := g.revisionSnapshot(newRevision, newRevision)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	diff, err := g.diff(oldSnapshot, newSnapshot, nil, options)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	return g.checkPolicySnapshots(filePath, oldSnapshot, newSnapshot, diff, options)
}

func (g *GitRepository) checkPolicySnapshots(filePath string, oldSnapshot, newSnapshot snapshot, diff DiffReport, options imaging.DiffOptions) (PolicyCheckResult, error) {
	absolutePolicyPath, err := filepath.Abs(filePath)
	if err != nil {
		return PolicyCheckResult{}, fmt.Errorf("resolve policy path: %w", err)
	}
	policy, err := LoadPolicy(absolutePolicyPath)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	objects := make(map[string][]byte, len(oldSnapshot.data)+len(newSnapshot.data))
	for path, entry := range oldSnapshot.entries {
		objects[entry.ContentOID] = oldSnapshot.data[path]
	}
	for path, entry := range newSnapshot.entries {
		objects[entry.ContentOID] = newSnapshot.data[path]
	}
	loadObject := func(oid string) ([]byte, error) {
		data, exists := objects[oid]
		if !exists {
			return nil, fmt.Errorf("Git snapshot object %s is unavailable", oid)
		}
		return data, nil
	}
	return evaluatePolicy(absolutePolicyPath, policy, newSnapshot.entries, diff, g.Root, loadObject, options)
}

func (g *GitRepository) resolveRange(revisionRange string) (string, string, error) {
	if strings.Count(revisionRange, "...") == 1 {
		parts := strings.SplitN(revisionRange, "...", 2)
		if parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid Git revision range %q", revisionRange)
		}
		mergeBase, err := gitOutput(g.Root, "merge-base", parts[0], parts[1])
		if err != nil {
			return "", "", fmt.Errorf("resolve merge base for %q: %w", revisionRange, err)
		}
		return mergeBase, parts[1], nil
	}
	if strings.Count(revisionRange, "..") == 1 {
		parts := strings.SplitN(revisionRange, "..", 2)
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], nil
		}
	}
	return "", "", fmt.Errorf("invalid Git revision range %q; expected <base>..<head> or <base>...<head>", revisionRange)
}

func (g *GitRepository) diff(oldSnapshot, newSnapshot snapshot, paths []string, options imaging.DiffOptions) (DiffReport, error) {
	adapter := &Repository{Root: g.Root}
	return adapter.diffSnapshots(oldSnapshot, newSnapshot, paths, options)
}

func (g *GitRepository) indexSnapshot(label string) (snapshot, error) {
	output, err := gitBytes(g.Root, "ls-files", "--stage", "-z")
	if err != nil {
		return snapshot{}, fmt.Errorf("read Git index: %w", err)
	}
	result := emptySnapshot(label)
	metadataDocuments := map[string][]byte{}
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		metadata, pathBytes, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		if !found || len(fields) != 3 || fields[2] != "0" {
			continue
		}
		path := filepath.ToSlash(string(pathBytes))
		if _, recipeObject := recipeOIDFromGitMetadataPath(path); recipeObject {
			data, err := gitBytes(g.Root, "cat-file", "blob", fields[1])
			if err != nil {
				return snapshot{}, fmt.Errorf("read staged recipe %s: %w", path, err)
			}
			if err := addGitSnapshotRecipe(&result, path, data); err != nil {
				return snapshot{}, err
			}
			continue
		}
		if assetPath, metadata := assetPathFromGitMetadata(path); metadata {
			data, err := gitBytes(g.Root, "cat-file", "blob", fields[1])
			if err != nil {
				return snapshot{}, fmt.Errorf("read staged metadata %s: %w", path, err)
			}
			metadataDocuments[assetPath] = data
			continue
		}
		if !imaging.IsAsset(path) {
			continue
		}
		data, err := gitBytes(g.Root, "cat-file", "blob", fields[1])
		if err != nil {
			return snapshot{}, fmt.Errorf("read staged asset %s: %w", path, err)
		}
		if err := g.addGitSnapshotData(&result, path, data); err != nil {
			return snapshot{}, err
		}
	}
	if err := applyGitMetadata(&result, metadataDocuments); err != nil {
		return snapshot{}, err
	}
	return result, nil
}

func (g *GitRepository) revisionSnapshot(revision, label string) (snapshot, error) {
	oid, err := gitOutput(g.Root, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return snapshot{}, fmt.Errorf("resolve Git revision %q: %w", revision, err)
	}
	output, err := gitBytes(g.Root, "ls-tree", "-r", "-z", "--full-tree", oid)
	if err != nil {
		return snapshot{}, fmt.Errorf("read Git tree %q: %w", revision, err)
	}
	result := emptySnapshot(label + " (" + ShortOID(oid) + ")")
	metadataDocuments := map[string][]byte{}
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		metadata, pathBytes, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		if !found || len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		path := filepath.ToSlash(string(pathBytes))
		if _, recipeObject := recipeOIDFromGitMetadataPath(path); recipeObject {
			data, err := gitBytes(g.Root, "cat-file", "blob", fields[2])
			if err != nil {
				return snapshot{}, fmt.Errorf("read recipe %s at %s: %w", path, revision, err)
			}
			if err := addGitSnapshotRecipe(&result, path, data); err != nil {
				return snapshot{}, err
			}
			continue
		}
		if assetPath, metadata := assetPathFromGitMetadata(path); metadata {
			data, err := gitBytes(g.Root, "cat-file", "blob", fields[2])
			if err != nil {
				return snapshot{}, fmt.Errorf("read metadata %s at %s: %w", path, revision, err)
			}
			metadataDocuments[assetPath] = data
			continue
		}
		if !imaging.IsAsset(path) {
			continue
		}
		data, err := gitBytes(g.Root, "cat-file", "blob", fields[2])
		if err != nil {
			return snapshot{}, fmt.Errorf("read asset %s at %s: %w", path, revision, err)
		}
		if err := g.addGitSnapshotData(&result, path, data); err != nil {
			return snapshot{}, err
		}
	}
	if err := applyGitMetadata(&result, metadataDocuments); err != nil {
		return snapshot{}, err
	}
	return result, nil
}

func (g *GitRepository) worktreeSnapshot(index snapshot) (snapshot, error) {
	result := emptySnapshot("worktree")
	result.working = true
	metadataDocuments := map[string][]byte{}
	for oid := range index.recipes {
		recipePath := gitRecipePath(oid)
		data, err := os.ReadFile(filepath.Join(g.Root, filepath.FromSlash(recipePath)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot{}, fmt.Errorf("read working recipe %s: %w", oid, err)
		}
		if err := addGitSnapshotRecipe(&result, recipePath, data); err != nil {
			return snapshot{}, err
		}
	}
	for path := range index.entries {
		data, err := os.ReadFile(filepath.Join(g.Root, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot{}, fmt.Errorf("read working asset %s: %w", path, err)
		}
		if err := g.addGitSnapshotData(&result, path, data); err != nil {
			return snapshot{}, err
		}
		metadataPath, err := gitAssetMetadataPath(path)
		if err != nil {
			return snapshot{}, err
		}
		metadataData, err := os.ReadFile(filepath.Join(g.Root, filepath.FromSlash(metadataPath)))
		if err == nil {
			metadataDocuments[path] = metadataData
		} else if !errors.Is(err, os.ErrNotExist) {
			return snapshot{}, fmt.Errorf("read working metadata for %s: %w", path, err)
		}
	}
	if err := applyGitMetadata(&result, metadataDocuments); err != nil {
		return snapshot{}, err
	}
	return result, nil
}

func (g *GitRepository) untrackedAssets() ([]string, error) {
	output, err := gitBytes(g.Root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked Git files: %w", err)
	}
	paths := []string{}
	for _, value := range bytes.Split(output, []byte{0}) {
		path := filepath.ToSlash(string(value))
		if path != "" && imaging.IsAsset(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func emptySnapshot(label string) snapshot {
	return snapshot{
		label:     label,
		entries:   map[string]Entry{},
		manifests: map[string]imaging.Manifest{},
		data:      map[string][]byte{},
		recipes:   map[string][]byte{},
	}
}

func addGitSnapshotAsset(target *snapshot, path string, data []byte) error {
	contentOID := hashBytes(data)
	manifest, err := inspectGitBlob(path, data, contentOID)
	if err != nil {
		return fmt.Errorf("inspect Git asset %s: %w", path, err)
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest for %s: %w", path, err)
	}
	recipeOID, err := embeddedRecipeOID(manifest.EmbeddedMetadata)
	if err != nil {
		return fmt.Errorf("inspect embedded recipe for %s: %w", path, err)
	}
	target.entries[path] = Entry{
		Path:        path,
		ContentOID:  contentOID,
		ManifestOID: hashBytes(manifestData),
		RecipeOID:   recipeOID,
		Size:        manifest.Size,
		Format:      manifest.Format,
		Width:       manifest.Width,
		Height:      manifest.Height,
		VisualHash:  manifest.VisualHash,
	}
	target.manifests[path] = manifest
	target.data[path] = data
	return nil
}

func inspectGitBlob(path string, data []byte, contentOID string) (imaging.Manifest, error) {
	temporary, err := os.CreateTemp("", "pixlog-git-*"+filepath.Ext(path))
	if err != nil {
		return imaging.Manifest{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return imaging.Manifest{}, err
	}
	if err := temporary.Close(); err != nil {
		return imaging.Manifest{}, err
	}
	return imaging.InspectFile(temporaryPath, contentOID)
}

func embeddedRecipeOID(metadata map[string]string) (string, error) {
	data, found, err := recipe.FromEmbedded(metadata)
	if err != nil || !found {
		return "", err
	}
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", err
	}
	return hashBytes(normalized), nil
}

func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}
