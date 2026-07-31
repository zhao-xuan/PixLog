package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pixlog/pixlog/internal/imaging"
	"github.com/pixlog/pixlog/internal/recipe"
)

const (
	GitMetadataDir         = ".pixlog-meta"
	GitMetadataSchema      = "pixlog.git/v1"
	GitAssetMetadataSchema = "pixlog.asset/v1"
)

type GitAssetMetadata struct {
	Schema   string           `json:"schema"`
	Path     string           `json:"path"`
	Entry    Entry            `json:"entry"`
	Manifest imaging.Manifest `json:"manifest"`
}

func (g *GitRepository) RootPath() string {
	return g.Root
}

func (g *GitRepository) SnapshotAssets() (map[string]string, error) {
	output, err := gitBytes(g.Root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list Git worktree files: %w", err)
	}
	snapshot := map[string]string{}
	for _, value := range bytes.Split(output, []byte{0}) {
		assetPath := filepath.ToSlash(string(value))
		if assetPath == "" || !imaging.IsAsset(assetPath) {
			continue
		}
		oid, err := HashFile(filepath.Join(g.Root, filepath.FromSlash(assetPath)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("hash asset %s: %w", assetPath, err)
		}
		snapshot[assetPath] = oid
	}
	return snapshot, nil
}

func (g *GitRepository) CaptureEntries() (map[string]Entry, error) {
	index, err := g.indexSnapshot("index")
	if err != nil {
		return nil, err
	}
	return index.entries, nil
}

func (g *GitRepository) Add(paths []string, recipeOID string) ([]Entry, error) {
	if len(paths) == 0 {
		return nil, errors.New("no paths specified")
	}
	if recipeOID != "" {
		if _, err := parseOID(recipeOID); err != nil {
			return nil, err
		}
		store, err := OpenGitMediaStore(g.Root)
		if err != nil {
			return nil, err
		}
		if _, err := store.Get(recipeOID); err != nil {
			return nil, fmt.Errorf("load recipe %s: %w", recipeOID, err)
		}
	}
	adapter := &Repository{Root: g.Root}
	files, err := adapter.expandAssetPaths(paths)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no supported image assets found")
	}

	entries := make([]Entry, 0, len(files))
	stagePaths := g.configStagePaths()
	journal, err := OpenProvenanceJournal(g.Root)
	if err != nil {
		return nil, err
	}
	defer journal.Close()
	for _, absolutePath := range files {
		assetPath, err := g.relativePath(absolutePath)
		if err != nil {
			return nil, err
		}
		contentOID, err := HashFile(absolutePath)
		if err != nil {
			return nil, err
		}
		if recipeOID != "" {
			if err := journal.Record(contentOID, recipeOID, assetPath); err != nil {
				return nil, err
			}
		}
		manifest, err := imaging.InspectFile(absolutePath, contentOID)
		if err != nil {
			return nil, err
		}
		entry := Entry{Path: assetPath, ContentOID: contentOID, RecipeOID: recipeOID, Size: manifest.Size, Format: manifest.Format, Width: manifest.Width, Height: manifest.Height, VisualHash: manifest.VisualHash}
		entries = append(entries, entry)
		stagePaths = append(stagePaths, assetPath)
	}
	if err := g.stage(stagePaths); err != nil {
		return nil, err
	}
	return entries, nil
}

func (g *GitRepository) ImportRecipe(assetPath string, data []byte) (string, error) {
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", err
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return "", err
	}
	oid, err := store.Put(normalized)
	if err != nil {
		return "", err
	}
	absolutePath, err := filepath.Abs(assetPath)
	if err != nil {
		return "", err
	}
	relativePath, err := g.relativePath(absolutePath)
	if err != nil {
		return "", err
	}
	if !imaging.IsAsset(relativePath) {
		return "", fmt.Errorf("%s is not a supported image asset", relativePath)
	}
	contentOID, err := HashFile(absolutePath)
	if err != nil {
		return "", err
	}
	journal, err := OpenProvenanceJournal(g.Root)
	if err != nil {
		return "", err
	}
	defer journal.Close()
	if err := journal.Record(contentOID, oid, relativePath); err != nil {
		return "", err
	}
	stagePaths := append(g.configStagePaths(), relativePath)
	if err := g.stage(stagePaths); err != nil {
		return "", err
	}
	if _, err := gitOutput(g.Root, "add", "--renormalize", "--", relativePath); err != nil {
		return "", fmt.Errorf("refresh recipe in Git pointer: %w", err)
	}
	return oid, nil
}

func (g *GitRepository) RecipeData(revision, assetPath string) (string, []byte, error) {
	entry, err := g.EntryAt(revision, assetPath)
	if err != nil {
		return "", nil, err
	}
	if entry.RecipeOID == "" {
		return "", nil, fmt.Errorf("asset %s has no recipe at %s", entry.Path, displayRevision(revision))
	}
	if _, err := parseOID(entry.RecipeOID); err != nil {
		return "", nil, fmt.Errorf("invalid recipe reference for %s: %w", entry.Path, err)
	}
	store, storeErr := OpenGitMediaStore(g.Root)
	if storeErr == nil {
		if data, loadErr := store.Get(entry.RecipeOID); loadErr == nil {
			normalized, err := recipe.Normalize(data)
			if err != nil {
				return "", nil, err
			}
			if hashBytes(normalized) != entry.RecipeOID {
				return "", nil, fmt.Errorf("recipe %s failed content verification", entry.RecipeOID)
			}
			return entry.RecipeOID, normalized, nil
		}
	}
	recipePath := gitRecipePath(entry.RecipeOID)
	var objectOID string
	if revision == "" || revision == "INDEX" {
		output, err := gitBytes(g.Root, "ls-files", "--stage", "-z", "--", recipePath)
		if err != nil {
			return "", nil, err
		}
		record := bytes.TrimSuffix(output, []byte{0})
		metadata, _, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		if !found || len(fields) != 3 || fields[2] != "0" {
			return "", nil, fmt.Errorf("recipe %s is not staged", entry.RecipeOID)
		}
		objectOID = fields[1]
	} else {
		objectOID, err = gitOutput(g.Root, "rev-parse", "--verify", revision+":"+recipePath)
		if err != nil {
			return "", nil, fmt.Errorf("resolve recipe %s at %s: %w", entry.RecipeOID, revision, err)
		}
	}
	data, err := gitBytes(g.Root, "cat-file", "blob", objectOID)
	if err != nil {
		return "", nil, err
	}
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", nil, err
	}
	if hashBytes(normalized) != entry.RecipeOID {
		return "", nil, fmt.Errorf("recipe %s failed content verification", entry.RecipeOID)
	}
	return entry.RecipeOID, normalized, nil
}

func (g *GitRepository) RecipeDiff(oldRevision, newRevision, assetPath string) (string, string, []recipe.Change, error) {
	oldOID, oldData, err := g.RecipeData(oldRevision, assetPath)
	if err != nil {
		return "", "", nil, err
	}
	newOID, newData, err := g.RecipeData(newRevision, assetPath)
	if err != nil {
		return "", "", nil, err
	}
	changes, err := recipe.Diff(oldData, newData)
	return oldOID, newOID, changes, err
}

func (g *GitRepository) EntryAt(revision, assetPath string) (Entry, error) {
	absolutePath, err := filepath.Abs(assetPath)
	if err != nil {
		return Entry{}, err
	}
	relativePath, err := g.relativePath(absolutePath)
	if err != nil {
		return Entry{}, err
	}
	var source snapshot
	if revision == "" || revision == "INDEX" {
		source, err = g.indexSnapshot("index")
	} else {
		source, err = g.revisionSnapshot(revision, revision)
	}
	if err != nil {
		return Entry{}, err
	}
	entry, exists := source.entries[relativePath]
	if !exists {
		return Entry{}, fmt.Errorf("asset %s does not exist at %s", relativePath, displayRevision(revision))
	}
	return entry, nil
}

func (g *GitRepository) Inspect(revision, assetPath string) (Inspection, error) {
	absolutePath, err := filepath.Abs(assetPath)
	if err != nil {
		return Inspection{}, err
	}
	relativePath, err := g.relativePath(absolutePath)
	if err != nil {
		return Inspection{}, err
	}
	var source snapshot
	if revision == "" || revision == "INDEX" {
		source, err = g.indexSnapshot("index")
	} else {
		source, err = g.revisionSnapshot(revision, revision)
	}
	if err != nil {
		return Inspection{}, err
	}
	entry, exists := source.entries[relativePath]
	if !exists {
		return Inspection{}, fmt.Errorf("asset %s does not exist at %s", relativePath, displayRevision(revision))
	}
	preview, exists := source.data[relativePath]
	if !exists {
		return Inspection{}, fmt.Errorf("asset data for %s is missing at %s", relativePath, displayRevision(revision))
	}
	return Inspection{Revision: displayRevision(revision), Entry: entry, Manifest: source.manifests[relativePath], Preview: preview}, nil
}

func (g *GitRepository) ApplyCommandCapture(modified, deleted []string, recipeData []byte) (string, error) {
	recipeOID := ""
	stagePaths := g.configStagePaths()
	if len(modified) > 0 {
		normalized, err := recipe.Normalize(recipeData)
		if err != nil {
			return "", err
		}
		store, err := OpenGitMediaStore(g.Root)
		if err != nil {
			return "", err
		}
		recipeOID, err = store.Put(normalized)
		if err != nil {
			return "", err
		}
		journal, err := OpenProvenanceJournal(g.Root)
		if err != nil {
			return "", err
		}
		defer journal.Close()
		for _, assetPath := range modified {
			contentOID, err := HashFile(filepath.Join(g.Root, filepath.FromSlash(assetPath)))
			if err != nil {
				return "", err
			}
			if err := journal.Record(contentOID, recipeOID, assetPath); err != nil {
				return "", err
			}
			stagePaths = append(stagePaths, assetPath)
		}
	}
	for _, assetPath := range deleted {
		stagePaths = append(stagePaths, assetPath)
	}
	if err := g.stage(stagePaths); err != nil {
		return "", err
	}
	return recipeOID, nil
}

func (g *GitRepository) writeAssetMetadata(assetPath, recipeOID string) (Entry, string, error) {
	metadataPath, err := gitAssetMetadataPath(assetPath)
	if err != nil {
		return Entry{}, "", err
	}
	absolutePath := filepath.Join(g.Root, filepath.FromSlash(assetPath))
	contentOID, err := HashFile(absolutePath)
	if err != nil {
		return Entry{}, "", fmt.Errorf("hash asset %s: %w", assetPath, err)
	}
	manifest, err := imaging.InspectFile(absolutePath, contentOID)
	if err != nil {
		return Entry{}, "", err
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return Entry{}, "", err
	}
	entry := Entry{
		Path:        assetPath,
		ContentOID:  contentOID,
		ManifestOID: hashBytes(manifestData),
		RecipeOID:   recipeOID,
		Size:        manifest.Size,
		Format:      manifest.Format,
		Width:       manifest.Width,
		Height:      manifest.Height,
		VisualHash:  manifest.VisualHash,
	}
	document := GitAssetMetadata{Schema: GitAssetMetadataSchema, Path: assetPath, Entry: entry, Manifest: manifest}
	if err := writeJSON(filepath.Join(g.Root, filepath.FromSlash(metadataPath)), document); err != nil {
		return Entry{}, "", err
	}
	return entry, metadataPath, nil
}

func (g *GitRepository) recipeForAsset(absolutePath, assetPath string) (string, error) {
	contentOID, err := HashFile(absolutePath)
	if err != nil {
		return "", err
	}
	manifest, err := imaging.InspectFile(absolutePath, contentOID)
	if err != nil {
		return "", err
	}
	data, found, err := recipe.FromEmbedded(manifest.EmbeddedMetadata)
	if err != nil {
		return "", err
	}
	if found {
		oid, _, err := g.storeRecipe(data)
		return oid, err
	}
	metadataPath, err := gitAssetMetadataPath(assetPath)
	if err != nil {
		return "", err
	}
	var existing GitAssetMetadata
	if err := readJSON(filepath.Join(g.Root, filepath.FromSlash(metadataPath)), &existing); err == nil && existing.Entry.ContentOID == contentOID {
		return existing.Entry.RecipeOID, nil
	}
	return "", nil
}

func (g *GitRepository) storeRecipe(data []byte) (string, string, error) {
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", "", err
	}
	oid := hashBytes(normalized)
	recipePath := gitRecipePath(oid)
	absolutePath := filepath.Join(g.Root, filepath.FromSlash(recipePath))
	if _, err := os.Stat(absolutePath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomic(absolutePath, append(normalized, '\n'), 0o644); err != nil {
			return "", "", err
		}
	} else if err != nil {
		return "", "", err
	}
	return oid, recipePath, nil
}

func (g *GitRepository) stage(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, value := range paths {
		set[filepath.ToSlash(value)] = struct{}{}
	}
	ordered := make([]string, 0, len(set))
	for value := range set {
		ordered = append(ordered, value)
	}
	sort.Strings(ordered)
	args := append([]string{"add", "-A", "--"}, ordered...)
	if _, err := gitOutput(g.Root, args...); err != nil {
		return fmt.Errorf("stage Git assets: %w", err)
	}
	return nil
}

func (g *GitRepository) metadataConfigStagePath() []string {
	relative := path.Join(GitMetadataDir, "config.json")
	if _, err := os.Stat(filepath.Join(g.Root, filepath.FromSlash(relative))); err == nil {
		return []string{relative}
	}
	return nil
}

func (g *GitRepository) configStagePaths() []string {
	paths := []string{}
	for _, relative := range []string{".gitattributes", ".pixlog.toml"} {
		if _, err := os.Stat(filepath.Join(g.Root, relative)); err == nil {
			paths = append(paths, relative)
		}
	}
	return paths
}

func gitAssetMetadataPath(assetPath string) (string, error) {
	clean := path.Clean(filepath.ToSlash(assetPath))
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid asset path %q", assetPath)
	}
	return path.Join(GitMetadataDir, "assets", clean) + ".json", nil
}

func assetPathFromGitMetadata(metadataPath string) (string, bool) {
	prefix := GitMetadataDir + "/assets/"
	if !strings.HasPrefix(metadataPath, prefix) || !strings.HasSuffix(metadataPath, ".json") {
		return "", false
	}
	assetPath := strings.TrimSuffix(strings.TrimPrefix(metadataPath, prefix), ".json")
	if assetPath == "" || !imaging.IsAsset(assetPath) {
		return "", false
	}
	return assetPath, true
}

func recipeOIDFromGitMetadataPath(metadataPath string) (string, bool) {
	prefix := GitMetadataDir + "/recipes/sha256/"
	if !strings.HasPrefix(metadataPath, prefix) || !strings.HasSuffix(metadataPath, ".json") {
		return "", false
	}
	relative := strings.TrimSuffix(strings.TrimPrefix(metadataPath, prefix), ".json")
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || len(parts[0]) != 2 {
		return "", false
	}
	oid := "sha256:" + parts[0] + parts[1]
	if _, err := parseOID(oid); err != nil {
		return "", false
	}
	return oid, true
}

func gitRecipePath(oid string) string {
	digest := strings.TrimPrefix(oid, "sha256:")
	return path.Join(GitMetadataDir, "recipes", "sha256", digest[:2], digest[2:]+".json")
}

func addGitSnapshotRecipe(target *snapshot, metadataPath string, data []byte) error {
	oid, validPath := recipeOIDFromGitMetadataPath(metadataPath)
	if !validPath {
		return fmt.Errorf("invalid Git recipe path %s", metadataPath)
	}
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return fmt.Errorf("decode Git recipe %s: %w", oid, err)
	}
	if hashBytes(normalized) != oid {
		return fmt.Errorf("Git recipe %s failed content verification", oid)
	}
	target.recipes[oid] = normalized
	return nil
}

func applyGitMetadata(target *snapshot, metadata map[string][]byte) error {
	for assetPath, data := range metadata {
		entry, exists := target.entries[assetPath]
		if !exists {
			continue
		}
		var document GitAssetMetadata
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode metadata for %s: %w", assetPath, err)
		}
		if document.Schema != GitAssetMetadataSchema || document.Path != assetPath {
			return fmt.Errorf("invalid metadata for %s", assetPath)
		}
		if document.Entry.ContentOID != entry.ContentOID {
			continue
		}
		if document.Entry.RecipeOID != "" {
			if _, err := parseOID(document.Entry.RecipeOID); err != nil {
				return fmt.Errorf("invalid recipe reference for %s: %w", assetPath, err)
			}
			if _, exists := target.recipes[document.Entry.RecipeOID]; !exists {
				return fmt.Errorf("recipe %s referenced by %s is missing from the Git snapshot", document.Entry.RecipeOID, assetPath)
			}
		}
		expectedEntry := entry
		expectedEntry.RecipeOID = document.Entry.RecipeOID
		if document.Entry != expectedEntry {
			return fmt.Errorf("metadata entry for %s does not match the inspected image", assetPath)
		}
		manifestData, err := json.Marshal(document.Manifest)
		if err != nil {
			return fmt.Errorf("encode metadata manifest for %s: %w", assetPath, err)
		}
		if hashBytes(manifestData) != entry.ManifestOID {
			return fmt.Errorf("metadata manifest for %s does not match the inspected image", assetPath)
		}
		target.entries[assetPath] = expectedEntry
	}
	return nil
}
