package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

type AssetDelta struct {
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

func (r *Repository) SnapshotAssets() (map[string]string, error) {
	paths, err := r.discoverAssets()
	if err != nil {
		return nil, err
	}
	snapshot := make(map[string]string, len(paths))
	for _, path := range paths {
		oid, err := HashFile(filepath.Join(r.Root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("hash asset %s: %w", path, err)
		}
		snapshot[path] = oid
	}
	return snapshot, nil
}

func CompareAssetSnapshots(before, after map[string]string) AssetDelta {
	delta := AssetDelta{Modified: []string{}, Deleted: []string{}}
	for path, oid := range after {
		if previous, existed := before[path]; !existed || previous != oid {
			delta.Modified = append(delta.Modified, path)
		}
	}
	for path := range before {
		if _, exists := after[path]; !exists {
			delta.Deleted = append(delta.Deleted, path)
		}
	}
	sort.Strings(delta.Modified)
	sort.Strings(delta.Deleted)
	return delta
}

func (r *Repository) Add(paths []string, recipeOID string) ([]Entry, error) {
	if r.Config.Bare {
		return nil, errors.New("cannot add files to a bare repository")
	}
	if len(paths) == 0 {
		return nil, errors.New("no paths specified")
	}
	if recipeOID != "" {
		if _, err := r.Load(recipeOID); err != nil {
			return nil, fmt.Errorf("load recipe %s: %w", recipeOID, err)
		}

	}
	files, err := r.expandAssetPaths(paths)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no supported image assets found")
	}

	index, err := r.ReadIndex()
	if err != nil {
		return nil, err
	}
	added := make([]Entry, 0, len(files))
	for _, absolutePath := range files {
		relativePath, err := r.relativePath(absolutePath)
		if err != nil {
			return nil, err
		}
		contentOID, err := r.StoreFile(absolutePath)
		if err != nil {
			return nil, err
		}
		manifest, err := imaging.InspectFile(absolutePath, contentOID)
		if err != nil {
			return nil, err
		}
		manifestOID, err := r.StoreJSON(manifest)
		if err != nil {
			return nil, err
		}
		entryRecipeOID := recipeOID
		if entryRecipeOID == "" {
			automaticOID, found, err := r.CaptureEmbeddedRecipe(manifest.EmbeddedMetadata)
			if err != nil {
				return nil, fmt.Errorf("capture embedded recipe for %s: %w", relativePath, err)
			}
			if found {
				entryRecipeOID = automaticOID
			} else if previous, exists := index.Entries[relativePath]; exists && previous.ContentOID == contentOID {
				entryRecipeOID = previous.RecipeOID
			}
		}
		entry := Entry{
			Path:        relativePath,
			ContentOID:  contentOID,
			ManifestOID: manifestOID,
			RecipeOID:   entryRecipeOID,
			Size:        manifest.Size,
			Format:      manifest.Format,
			Width:       manifest.Width,
			Height:      manifest.Height,
			VisualHash:  manifest.VisualHash,
		}
		index.Entries[relativePath] = entry
		added = append(added, entry)
	}
	if err := r.WriteIndex(index); err != nil {
		return nil, err
	}
	return added, nil
}

func (r *Repository) Remove(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("no paths specified")
	}
	index, err := r.ReadIndex()
	if err != nil {
		return nil, err
	}
	removedSet := map[string]struct{}{}
	for _, input := range paths {
		absolute, err := filepath.Abs(input)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", input, err)
		}
		relative, err := r.relativePath(absolute)
		if err != nil {
			return nil, err
		}
		prefix := strings.TrimSuffix(relative, "/") + "/"
		for trackedPath := range index.Entries {
			if trackedPath == relative || strings.HasPrefix(trackedPath, prefix) {
				delete(index.Entries, trackedPath)
				removedSet[trackedPath] = struct{}{}
			}
		}
	}
	if len(removedSet) == 0 {
		return nil, errors.New("none of the specified paths are tracked")
	}
	if err := r.WriteIndex(index); err != nil {
		return nil, err
	}
	removed := sortedKeys(removedSet)
	return removed, nil
}

func (r *Repository) Status() (Status, error) {
	index, err := r.ReadIndex()
	if err != nil {
		return Status{}, err
	}
	headOID, headTree, err := r.HeadTree()
	if err != nil {
		return Status{}, err
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return Status{}, err
	}

	status := Status{
		Branch:   branch,
		Head:     headOID,
		Staged:   CompareTrees(headTree, index.Entries),
		Unstaged: []Change{},
	}
	for path, indexed := range index.Entries {
		absolute := filepath.Join(r.Root, filepath.FromSlash(path))
		workingOID, hashErr := HashFile(absolute)
		if errors.Is(hashErr, os.ErrNotExist) {
			oldEntry := indexed
			status.Unstaged = append(status.Unstaged, Change{Path: path, Kind: ChangeDeleted, Old: &oldEntry})
			continue
		}
		if hashErr != nil {
			return Status{}, fmt.Errorf("inspect working file %s: %w", path, hashErr)
		}
		if workingOID != indexed.ContentOID {
			oldEntry := indexed
			workingEntry := indexed
			workingEntry.ContentOID = workingOID
			status.Unstaged = append(status.Unstaged, Change{Path: path, Kind: ChangeModified, Old: &oldEntry, New: &workingEntry})
		}
	}
	sort.Slice(status.Unstaged, func(left, right int) bool {
		return status.Unstaged[left].Path < status.Unstaged[right].Path
	})

	discovered, err := r.discoverAssets()
	if err != nil {
		return Status{}, err
	}
	for _, path := range discovered {
		if _, tracked := index.Entries[path]; !tracked {
			status.Untracked = append(status.Untracked, path)
		}
	}
	return status, nil
}

func (r *Repository) CreateCommit(message, author string, createdAt time.Time) (string, Commit, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", Commit{}, errors.New("commit message cannot be empty")
	}
	index, err := r.ReadIndex()
	if err != nil {
		return "", Commit{}, err
	}
	parentOID, parentTree, err := r.HeadTree()
	if err != nil {
		return "", Commit{}, err
	}
	if len(CompareTrees(parentTree, index.Entries)) == 0 {
		return "", Commit{}, errors.New("nothing staged to commit")
	}
	if strings.TrimSpace(author) == "" {
		author = DefaultAuthor()
	}
	commit := Commit{
		Schema:    SchemaVersion,
		Parent:    parentOID,
		Author:    author,
		Message:   message,
		CreatedAt: createdAt.UTC(),
		Tree:      cloneTree(index.Entries),
	}
	oid, err := r.StoreJSON(commit)
	if err != nil {
		return "", Commit{}, err
	}
	if err := r.SetHeadOID(oid); err != nil {
		return "", Commit{}, err
	}
	return oid, commit, nil
}

func (r *Repository) Log(path string, limit int) ([]CommitRecord, error) {
	oid, err := r.HeadOID()
	if err != nil {
		return nil, err
	}
	if oid == "" {
		return []CommitRecord{}, nil
	}
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path, err = r.relativePath(absolute)
		if err != nil {
			return nil, err
		}
	}

	records := []CommitRecord{}
	for oid != "" && (limit <= 0 || len(records) < limit) {
		commit, err := r.ReadCommit(oid)
		if err != nil {
			return nil, err
		}
		include := path == ""
		if path != "" {
			var parentTree map[string]Entry
			if commit.Parent == "" {
				parentTree = map[string]Entry{}
			} else {
				parent, err := r.ReadCommit(commit.Parent)
				if err != nil {
					return nil, err
				}
				parentTree = parent.Tree
			}
			include = !entriesEqual(parentTree[path], commit.Tree[path])
		}
		if include {
			records = append(records, CommitRecord{OID: oid, Commit: commit})
		}
		oid = commit.Parent
	}
	return records, nil
}

func (r *Repository) HeadTree() (string, map[string]Entry, error) {
	oid, err := r.HeadOID()
	if err != nil {
		return "", nil, err
	}
	if oid == "" {
		return "", map[string]Entry{}, nil
	}
	commit, err := r.ReadCommit(oid)
	if err != nil {
		return "", nil, err
	}
	return oid, commit.Tree, nil
}

func CompareTrees(oldTree, newTree map[string]Entry) []Change {
	paths := map[string]struct{}{}
	for path := range oldTree {
		paths[path] = struct{}{}
	}
	for path := range newTree {
		paths[path] = struct{}{}
	}
	orderedPaths := sortedKeys(paths)
	changes := make([]Change, 0)
	for _, path := range orderedPaths {
		oldEntry, oldExists := oldTree[path]
		newEntry, newExists := newTree[path]
		switch {
		case !oldExists && newExists:
			entry := newEntry
			changes = append(changes, Change{Path: path, Kind: ChangeAdded, New: &entry})
		case oldExists && !newExists:
			entry := oldEntry
			changes = append(changes, Change{Path: path, Kind: ChangeDeleted, Old: &entry})
		case !entriesEqual(oldEntry, newEntry):
			oldCopy, newCopy := oldEntry, newEntry
			changes = append(changes, Change{Path: path, Kind: ChangeModified, Old: &oldCopy, New: &newCopy})
		}
	}
	return changes
}

func DefaultAuthor() string {
	if author := strings.TrimSpace(os.Getenv("PIXLOG_AUTHOR")); author != "" {
		return author
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user
	}
	return "unknown"
}

func ShortOID(oid string) string {
	digest := strings.TrimPrefix(oid, "sha256:")
	if len(digest) > 12 {
		digest = digest[:12]
	}
	return digest
}

func (r *Repository) relativePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	relative, err := filepath.Rel(r.Root, absolute)
	if err != nil {
		return "", fmt.Errorf("make %s relative to repository: %w", path, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s is outside the repository", path)
	}
	if relative == ControlDirName || strings.HasPrefix(relative, ControlDirName+string(filepath.Separator)) {
		return "", fmt.Errorf("cannot track PixLog control files")
	}
	return filepath.ToSlash(filepath.Clean(relative)), nil
}

func (r *Repository) expandAssetPaths(inputs []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, input := range inputs {
		matches := []string{input}
		if strings.ContainsAny(input, "*?[") {
			globMatches, err := filepath.Glob(input)
			if err != nil {
				return nil, fmt.Errorf("expand %s: %w", input, err)
			}
			matches = globMatches
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("pathspec %q did not match any files", input)
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, fmt.Errorf("inspect %s: %w", match, err)
			}
			if !info.IsDir() {
				if !imaging.IsAsset(match) {
					return nil, fmt.Errorf("%s is not a supported image asset", match)
				}
				absolute, err := filepath.Abs(match)
				if err != nil {
					return nil, err
				}
				if _, err := r.relativePath(absolute); err != nil {
					return nil, err
				}
				set[absolute] = struct{}{}
				continue
			}
			err = filepath.WalkDir(match, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() && (entry.Name() == ControlDirName || entry.Name() == ".git") {
					return filepath.SkipDir
				}
				if entry.Type().IsRegular() && imaging.IsAsset(path) {
					absolute, err := filepath.Abs(path)
					if err != nil {
						return err
					}
					if _, err := r.relativePath(absolute); err != nil {
						return err
					}
					set[absolute] = struct{}{}
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", match, err)
			}
		}
	}
	return sortedKeys(set), nil
}

func (r *Repository) discoverAssets() ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(r.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ControlDirName || entry.Name() == ".git") {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() && imaging.IsAsset(path) {
			relative, err := r.relativePath(path)
			if err != nil {
				return err
			}
			paths = append(paths, relative)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func cloneTree(tree map[string]Entry) map[string]Entry {
	copy := make(map[string]Entry, len(tree))
	for path, entry := range tree {
		copy[path] = entry
	}
	return copy
}

func entriesEqual(left, right Entry) bool {
	return left.ContentOID == right.ContentOID &&
		left.ManifestOID == right.ManifestOID &&
		left.RecipeOID == right.RecipeOID
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
