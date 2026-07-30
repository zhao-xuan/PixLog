package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pixlog/pixlog/internal/recipe"
)

func (r *Repository) ImportRecipe(assetPath string, data []byte) (string, error) {
	oid, err := r.StoreRecipe(data)
	if err != nil {
		return "", err
	}
	index, err := r.ReadIndex()
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return "", err
	}
	path, err := r.relativePath(absolute)
	if err != nil {
		return "", err
	}
	entry, exists := index.Entries[path]
	if !exists {
		return "", fmt.Errorf("asset %s is not staged; run pixlog add first", path)
	}
	entry.RecipeOID = oid
	index.Entries[path] = entry
	if err := r.WriteIndex(index); err != nil {
		return "", err
	}
	return oid, nil
}

func (r *Repository) StoreRecipe(data []byte) (string, error) {
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", err
	}
	oid, err := r.Store(normalized)
	if err != nil {
		return "", err
	}
	return oid, nil
}

func (r *Repository) RecipeData(revision, assetPath string) (string, []byte, error) {
	entry, err := r.EntryAt(revision, assetPath)
	if err != nil {
		return "", nil, err
	}
	if entry.RecipeOID == "" {
		return "", nil, fmt.Errorf("asset %s has no recipe at %s", entry.Path, displayRevision(revision))
	}
	data, err := r.Load(entry.RecipeOID)
	if err != nil {
		return "", nil, err
	}
	return entry.RecipeOID, data, nil
}

func (r *Repository) RecipeDiff(oldRevision, newRevision, assetPath string) (string, string, []recipe.Change, error) {
	oldOID, oldData, err := r.RecipeData(oldRevision, assetPath)
	if err != nil {
		return "", "", nil, err
	}
	newOID, newData, err := r.RecipeData(newRevision, assetPath)
	if err != nil {
		return "", "", nil, err
	}
	changes, err := recipe.Diff(oldData, newData)
	return oldOID, newOID, changes, err
}

func (r *Repository) EntryAt(revision, assetPath string) (Entry, error) {
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return Entry{}, err
	}
	path, err := r.relativePath(absolute)
	if err != nil {
		return Entry{}, err
	}
	var tree map[string]Entry
	if revision == "" || revision == "INDEX" {
		index, err := r.ReadIndex()
		if err != nil {
			return Entry{}, err
		}
		tree = index.Entries
	} else {
		_, commit, err := r.ResolveCommit(revision)
		if err != nil {
			return Entry{}, err
		}
		tree = commit.Tree
	}
	entry, exists := tree[path]
	if !exists {
		return Entry{}, fmt.Errorf("asset %s does not exist at %s", path, displayRevision(revision))
	}
	return entry, nil
}

func (r *Repository) CaptureEmbeddedRecipe(manifestMetadata map[string]string) (string, bool, error) {
	data, found, err := recipe.FromEmbedded(manifestMetadata)
	if err != nil || !found {
		return "", found, err
	}
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return "", false, err
	}
	oid, err := r.Store(normalized)
	return oid, true, err
}

func ReadRecipeFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("recipe file %s does not exist", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read recipe file: %w", err)
	}
	return data, nil
}

func displayRevision(revision string) string {
	if revision == "" {
		return "INDEX"
	}
	return revision
}
