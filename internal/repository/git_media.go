package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zhao-xuan/PixLog/internal/imaging"
	"github.com/zhao-xuan/PixLog/internal/recipe"
)

type GitMediaStore struct {
	Root string
}

func OpenGitMediaStore(start string) (*GitMediaStore, error) {
	repo, err := OpenGit(start)
	if err != nil {
		return nil, err
	}
	commonDirectory, err := gitOutput(repo.Root, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve Git common directory: %w", err)
	}
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(repo.Root, commonDirectory)
	}
	return &GitMediaStore{Root: filepath.Clean(filepath.Join(commonDirectory, "pixlog"))}, nil
}

func (store *GitMediaStore) Put(data []byte) (string, error) {
	oid := hashBytes(data)
	objectPath, err := store.ObjectPath(oid)
	if err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(objectPath); err == nil {
		if hashBytes(existing) != oid {
			return "", fmt.Errorf("local PixLog object %s failed content verification", oid)
		}
		return oid, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := writeFileAtomic(objectPath, data, 0o644); err != nil {
		return "", fmt.Errorf("store PixLog object %s: %w", oid, err)
	}
	return oid, nil
}

func (store *GitMediaStore) Get(oid string) ([]byte, error) {
	objectPath, err := store.ObjectPath(oid)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("read PixLog object %s: %w", oid, err)
	}
	if hashBytes(data) != oid {
		return nil, fmt.Errorf("PixLog object %s failed content verification", oid)
	}
	return data, nil
}

func (store *GitMediaStore) Has(oid string) bool {
	objectPath, err := store.ObjectPath(oid)
	if err != nil {
		return false
	}
	_, err = os.Stat(objectPath)
	return err == nil
}

func (store *GitMediaStore) ObjectPath(oid string) (string, error) {
	digest, err := parseOID(oid)
	if err != nil {
		return "", err
	}
	return filepath.Join(store.Root, "objects", "sha256", digest[:2], digest[2:]), nil
}

func (g *GitRepository) CleanFilter(assetPath string, data []byte) ([]byte, PixLogPointer, error) {
	if pointer, found, err := ParsePixLogPointer(data); found || err != nil {
		return data, pointer, err
	}
	if !imaging.IsAsset(assetPath) {
		return data, PixLogPointer{}, nil
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	contentOID, err := store.Put(data)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	manifest, err := inspectGitBlob(assetPath, data, contentOID)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	manifestOID, err := store.Put(manifestData)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	recipeOID := ""
	journal, err := OpenProvenanceJournal(g.Root)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	defer journal.Close()
	if associatedOID, found, err := journal.RecipeForContent(contentOID); err != nil {
		return nil, PixLogPointer{}, err
	} else if found {
		if _, err := store.Get(associatedOID); err != nil {
			return nil, PixLogPointer{}, fmt.Errorf("load journal recipe for %s: %w", assetPath, err)
		}
		recipeOID = associatedOID
	} else if embedded, found, err := recipe.FromEmbedded(manifest.EmbeddedMetadata); err != nil {
		return nil, PixLogPointer{}, err
	} else if found {
		normalized, err := recipe.Normalize(embedded)
		if err != nil {
			return nil, PixLogPointer{}, err
		}
		recipeOID, err = store.Put(normalized)
		if err != nil {
			return nil, PixLogPointer{}, err
		}
		if err := journal.Record(contentOID, recipeOID, assetPath); err != nil {
			return nil, PixLogPointer{}, err
		}
	}
	pointer := PixLogPointer{
		OID:         contentOID,
		Size:        int64(len(data)),
		ManifestOID: manifestOID,
		RecipeOID:   recipeOID,
		MediaType:   manifest.MediaType,
		VisualHash:  manifest.VisualHash,
	}
	pointerData, err := pointer.Encode()
	return pointerData, pointer, err
}

func (g *GitRepository) SmudgeFilter(data []byte) ([]byte, PixLogPointer, error) {
	pointer, found, err := ParsePixLogPointer(data)
	if err != nil || !found {
		return data, PixLogPointer{}, err
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	content, err := g.loadMediaObject(store, pointer.OID)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	if int64(len(content)) != pointer.Size {
		return nil, PixLogPointer{}, fmt.Errorf("PixLog object %s has size %d, pointer requires %d", pointer.OID, len(content), pointer.Size)
	}
	return content, pointer, nil
}

func (g *GitRepository) addGitSnapshotData(target *snapshot, assetPath string, data []byte) error {
	pointer, found, err := ParsePixLogPointer(data)
	if err != nil {
		return fmt.Errorf("parse PixLog pointer for %s: %w", assetPath, err)
	}
	if !found {
		return addGitSnapshotAsset(target, assetPath, data)
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return err
	}
	content, err := g.loadMediaObject(store, pointer.OID)
	if err != nil {
		return fmt.Errorf("hydrate %s: %w", assetPath, err)
	}
	manifestData, err := g.loadMediaObject(store, pointer.ManifestOID)
	if err != nil {
		return fmt.Errorf("load manifest for %s: %w", assetPath, err)
	}
	var manifest imaging.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("decode manifest for %s: %w", assetPath, err)
	}
	if manifest.ContentOID != pointer.OID || manifest.Size != pointer.Size {
		return fmt.Errorf("manifest for %s does not match its pointer", assetPath)
	}
	entry := Entry{
		Path:        assetPath,
		ContentOID:  pointer.OID,
		ManifestOID: pointer.ManifestOID,
		RecipeOID:   pointer.RecipeOID,
		Size:        manifest.Size,
		Format:      manifest.Format,
		Width:       manifest.Width,
		Height:      manifest.Height,
		VisualHash:  manifest.VisualHash,
	}
	if pointer.RecipeOID != "" {
		recipeData, err := g.loadMediaObject(store, pointer.RecipeOID)
		if err != nil {
			return fmt.Errorf("load recipe for %s: %w", assetPath, err)
		}
		if _, err := recipe.Normalize(recipeData); err != nil {
			return fmt.Errorf("decode recipe for %s: %w", assetPath, err)
		}
		target.recipes[pointer.RecipeOID] = recipeData
	}
	target.entries[assetPath] = entry
	target.manifests[assetPath] = manifest
	target.data[assetPath] = content
	return nil
}

func (g *GitRepository) loadMediaObject(store *GitMediaStore, oid string) ([]byte, error) {
	data, err := store.Get(oid)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if fetchErr := g.FetchMediaObject("origin", oid); fetchErr != nil {
		return nil, errors.Join(err, fetchErr)
	}
	return store.Get(oid)
}
