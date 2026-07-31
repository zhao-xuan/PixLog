package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pixlog/pixlog/internal/imaging"
)

type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

type AssetDiff struct {
	Path     string              `json:"path"`
	Kind     ChangeKind          `json:"kind"`
	Old      *Entry              `json:"old,omitempty"`
	New      *Entry              `json:"new,omitempty"`
	Metadata []FieldChange       `json:"metadata"`
	Visual   *imaging.VisualDiff `json:"visual,omitempty"`
	Note     string              `json:"note,omitempty"`
	Preview  *AssetPreview       `json:"-"`
}

type AssetPreview struct {
	Before []byte
	After  []byte
}

type DiffReport struct {
	From   string      `json:"from"`
	To     string      `json:"to"`
	Assets []AssetDiff `json:"assets"`
}

type snapshot struct {
	label     string
	entries   map[string]Entry
	manifests map[string]imaging.Manifest
	data      map[string][]byte
	recipes   map[string][]byte
	working   bool
}

func (r *Repository) DiffWorking(paths []string, options imaging.DiffOptions) (DiffReport, error) {
	index, err := r.ReadIndex()
	if err != nil {
		return DiffReport{}, err
	}
	oldSnapshot, err := r.objectSnapshot("index", index.Entries)
	if err != nil {
		return DiffReport{}, err
	}
	workingEntries := map[string]Entry{}
	workingManifests := map[string]imaging.Manifest{}
	for path, indexed := range index.Entries {
		absolute := filepath.Join(r.Root, filepath.FromSlash(path))
		contentOID, hashErr := HashFile(absolute)
		if errors.Is(hashErr, os.ErrNotExist) {
			continue
		}
		if hashErr != nil {
			return DiffReport{}, fmt.Errorf("inspect working file %s: %w", path, hashErr)
		}
		manifest, err := imaging.InspectFile(absolute, contentOID)
		if err != nil {
			return DiffReport{}, err
		}
		manifestOID := ""
		if contentOID == indexed.ContentOID {
			manifestOID = indexed.ManifestOID
		}
		workingEntries[path] = Entry{
			Path:        path,
			ContentOID:  contentOID,
			ManifestOID: manifestOID,
			RecipeOID:   indexed.RecipeOID,
			Size:        manifest.Size,
			Format:      manifest.Format,
			Width:       manifest.Width,
			Height:      manifest.Height,
			VisualHash:  manifest.VisualHash,
		}
		workingManifests[path] = manifest
	}
	newSnapshot := snapshot{label: "worktree", entries: workingEntries, manifests: workingManifests, working: true}
	return r.diffSnapshots(oldSnapshot, newSnapshot, paths, options)
}

func (r *Repository) DiffStaged(paths []string, options imaging.DiffOptions) (DiffReport, error) {
	_, headTree, err := r.HeadTree()
	if err != nil {
		return DiffReport{}, err
	}
	index, err := r.ReadIndex()
	if err != nil {
		return DiffReport{}, err
	}
	oldSnapshot, err := r.objectSnapshot("HEAD", headTree)
	if err != nil {
		return DiffReport{}, err
	}
	newSnapshot, err := r.objectSnapshot("index", index.Entries)
	if err != nil {
		return DiffReport{}, err
	}
	return r.diffSnapshots(oldSnapshot, newSnapshot, paths, options)
}

func (r *Repository) DiffCommits(oldRevision, newRevision string, paths []string, options imaging.DiffOptions) (DiffReport, error) {
	oldOID, oldCommit, err := r.ResolveCommit(oldRevision)
	if err != nil {
		return DiffReport{}, err
	}
	newOID, newCommit, err := r.ResolveCommit(newRevision)
	if err != nil {
		return DiffReport{}, err
	}
	oldSnapshot, err := r.objectSnapshot(oldRevision+" ("+ShortOID(oldOID)+")", oldCommit.Tree)
	if err != nil {
		return DiffReport{}, err
	}
	newSnapshot, err := r.objectSnapshot(newRevision+" ("+ShortOID(newOID)+")", newCommit.Tree)
	if err != nil {
		return DiffReport{}, err
	}
	return r.diffSnapshots(oldSnapshot, newSnapshot, paths, options)
}

func (r *Repository) objectSnapshot(label string, entries map[string]Entry) (snapshot, error) {
	manifests := make(map[string]imaging.Manifest, len(entries))
	for path, entry := range entries {
		if entry.ManifestOID == "" {
			continue
		}
		data, err := r.Load(entry.ManifestOID)
		if err != nil {
			return snapshot{}, fmt.Errorf("load manifest for %s: %w", path, err)
		}
		var manifest imaging.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return snapshot{}, fmt.Errorf("decode manifest for %s: %w", path, err)
		}
		manifests[path] = manifest
	}
	return snapshot{label: label, entries: entries, manifests: manifests}, nil
}

func (r *Repository) diffSnapshots(oldSnapshot, newSnapshot snapshot, paths []string, options imaging.DiffOptions) (DiffReport, error) {
	filters, err := r.normalizeFilters(paths)
	if err != nil {
		return DiffReport{}, err
	}
	report := DiffReport{From: oldSnapshot.label, To: newSnapshot.label, Assets: []AssetDiff{}}
	for _, change := range CompareTrees(oldSnapshot.entries, newSnapshot.entries) {
		if !matchesFilters(change.Path, filters) {
			continue
		}
		assetDiff := AssetDiff{
			Path:     change.Path,
			Kind:     change.Kind,
			Old:      change.Old,
			New:      change.New,
			Metadata: metadataChanges(change.Old, change.New, oldSnapshot.manifests[change.Path], newSnapshot.manifests[change.Path]),
		}
		var oldData, newData []byte
		if options.IncludePreview && change.Old != nil {
			oldData, err = r.snapshotData(oldSnapshot, change.Path, *change.Old)
			if err != nil {
				return DiffReport{}, err
			}
		}
		if options.IncludePreview && change.New != nil {
			newData, err = r.snapshotData(newSnapshot, change.Path, *change.New)
			if err != nil {
				return DiffReport{}, err
			}
		}
		if change.Old != nil && change.New != nil && change.Old.ContentOID != change.New.ContentOID {
			if oldData == nil {
				oldData, err = r.snapshotData(oldSnapshot, change.Path, *change.Old)
				if err != nil {
					return DiffReport{}, err
				}
			}
			if newData == nil {
				newData, err = r.snapshotData(newSnapshot, change.Path, *change.New)
				if err != nil {
					return DiffReport{}, err
				}
			}
			visual, compareErr := imaging.CompareReaders(bytes.NewReader(oldData), bytes.NewReader(newData), options)
			if compareErr != nil {
				if errors.Is(compareErr, imaging.ErrUnsupportedVisualFormat) {
					assetDiff.Note = "visual diff unavailable for this format; byte and manifest changes are still tracked"
				} else {
					return DiffReport{}, fmt.Errorf("compare %s: %w", change.Path, compareErr)
				}
			} else {
				assetDiff.Visual = &visual
			}
		}
		if options.IncludePreview {
			assetDiff.Preview = &AssetPreview{Before: oldData, After: newData}
		}
		report.Assets = append(report.Assets, assetDiff)
	}
	return report, nil
}

func (r *Repository) snapshotData(source snapshot, path string, entry Entry) ([]byte, error) {
	if data, exists := source.data[path]; exists {
		return data, nil
	}
	if source.working {
		data, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read working file %s: %w", path, err)
		}
		return data, nil
	}
	return r.Load(entry.ContentOID)
}

func (r *Repository) normalizeFilters(paths []string) ([]string, error) {
	filters := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		relative, err := r.relativePath(absolute)
		if err != nil {
			return nil, err
		}
		filters = append(filters, strings.TrimSuffix(relative, "/"))
	}
	return filters, nil
}

func matchesFilters(path string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, filter := range filters {
		if path == filter || strings.HasPrefix(path, filter+"/") {
			return true
		}
	}
	return false
}

func metadataChanges(oldEntry, newEntry *Entry, oldManifest, newManifest imaging.Manifest) []FieldChange {
	changes := []FieldChange{}
	appendChange := func(field string, oldValue, newValue any) {
		if fmt.Sprint(oldValue) != fmt.Sprint(newValue) {
			changes = append(changes, FieldChange{Field: field, Old: oldValue, New: newValue})
		}
	}
	if oldEntry == nil || newEntry == nil {
		return changes
	}
	appendChange("content_oid", oldEntry.ContentOID, newEntry.ContentOID)
	appendChange("format", oldEntry.Format, newEntry.Format)
	appendChange("size", oldEntry.Size, newEntry.Size)
	appendChange("width", oldEntry.Width, newEntry.Width)
	appendChange("height", oldEntry.Height, newEntry.Height)
	appendChange("visual_hash", oldEntry.VisualHash, newEntry.VisualHash)
	appendChange("recipe_oid", oldEntry.RecipeOID, newEntry.RecipeOID)
	metadataKeys := map[string]struct{}{}
	for key := range oldManifest.EmbeddedMetadata {
		metadataKeys[key] = struct{}{}
	}
	for key := range newManifest.EmbeddedMetadata {
		metadataKeys[key] = struct{}{}
	}
	keys := make([]string, 0, len(metadataKeys))
	for key := range metadataKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		appendChange("embedded_metadata."+key, oldManifest.EmbeddedMetadata[key], newManifest.EmbeddedMetadata[key])
	}
	return changes
}
