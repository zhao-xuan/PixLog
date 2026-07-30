package repository

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

type HydrateResult struct {
	Hydrated   []string `json:"hydrated,omitempty"`
	Dehydrated []string `json:"dehydrated,omitempty"`
}

func (g *GitRepository) Hydrate(paths []string) (HydrateResult, error) {
	pointers, err := g.trackedPointers(paths)
	if err != nil {
		return HydrateResult{}, err
	}
	result := HydrateResult{Hydrated: []string{}}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return result, err
	}
	for assetPath, pointerData := range pointers {
		pointer, _, _ := ParsePixLogPointer(pointerData)
		content, err := g.loadMediaObject(store, pointer.OID)
		if err != nil {
			return result, fmt.Errorf("hydrate %s: %w", assetPath, err)
		}
		if int64(len(content)) != pointer.Size {
			return result, fmt.Errorf("hydrate %s: object size does not match pointer", assetPath)
		}
		if err := writeFileAtomic(filepath.Join(g.Root, filepath.FromSlash(assetPath)), content, 0o644); err != nil {
			return result, err
		}
		result.Hydrated = append(result.Hydrated, assetPath)
	}
	return result, nil
}

func (g *GitRepository) Dehydrate(paths []string) (HydrateResult, error) {
	pointers, err := g.trackedPointers(paths)
	if err != nil {
		return HydrateResult{}, err
	}
	result := HydrateResult{Dehydrated: []string{}}
	for assetPath, pointerData := range pointers {
		if err := writeFileAtomic(filepath.Join(g.Root, filepath.FromSlash(assetPath)), pointerData, 0o644); err != nil {
			return result, err
		}
		result.Dehydrated = append(result.Dehydrated, assetPath)
	}
	return result, nil
}

func (g *GitRepository) trackedPointers(paths []string) (map[string][]byte, error) {
	args := []string{"ls-files", "-z"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	listing, err := gitBytes(g.Root, args...)
	if err != nil {
		return nil, err
	}
	result := map[string][]byte{}
	for _, pathBytes := range bytes.Split(listing, []byte{0}) {
		assetPath := filepath.ToSlash(string(pathBytes))
		if assetPath == "" {
			continue
		}
		pointerData, err := gitBytes(g.Root, "show", ":"+assetPath)
		if err != nil {
			return nil, err
		}
		if _, found, err := ParsePixLogPointer(pointerData); err != nil {
			return nil, err
		} else if found {
			result[assetPath] = pointerData
		}
	}
	return result, nil
}

func removeMediaObject(store *GitMediaStore, oid string) error {
	objectPath, err := store.ObjectPath(oid)
	if err != nil {
		return err
	}
	if err := os.Remove(objectPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
