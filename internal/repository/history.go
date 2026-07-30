package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/pixlog/pixlog/internal/imaging"
)

type Inspection struct {
	Revision string           `json:"revision"`
	Entry    Entry            `json:"entry"`
	Manifest imaging.Manifest `json:"manifest"`
}

type LineageNode struct {
	CommitOID  string    `json:"commit_oid"`
	Parent     string    `json:"parent,omitempty"`
	Author     string    `json:"author"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
	ContentOID string    `json:"content_oid,omitempty"`
	RecipeOID  string    `json:"recipe_oid,omitempty"`
	VisualHash string    `json:"visual_hash,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
}

type BlameResult struct {
	Path       string              `json:"path"`
	Point      [2]int              `json:"point"`
	CommitOID  string              `json:"commit_oid"`
	Author     string              `json:"author"`
	Message    string              `json:"message"`
	CreatedAt  time.Time           `json:"created_at"`
	ContentOID string              `json:"content_oid"`
	RecipeOID  string              `json:"recipe_oid,omitempty"`
	Region     imaging.BoundingBox `json:"region"`
	Confidence string              `json:"confidence"`
}

func (r *Repository) Restore(revision string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("no paths specified")
	}
	var tree map[string]Entry
	if revision == "" || revision == "INDEX" {
		index, err := r.ReadIndex()
		if err != nil {
			return nil, err
		}
		tree = index.Entries
	} else {
		_, commit, err := r.ResolveCommit(revision)
		if err != nil {
			return nil, err
		}
		tree = commit.Tree
	}
	filters, err := r.normalizeFilters(paths)
	if err != nil {
		return nil, err
	}
	restored := []string{}
	for path, entry := range tree {
		if !matchesFilters(path, filters) {
			continue
		}
		data, err := r.Load(entry.ContentOID)
		if err != nil {
			return nil, err
		}
		relative, err := safeTreePath(path)
		if err != nil {
			return nil, err
		}
		if err := writeFileAtomic(filepath.Join(r.Root, relative), data, 0o644); err != nil {
			return nil, err
		}
		restored = append(restored, path)
	}
	if len(restored) == 0 {
		return nil, fmt.Errorf("pathspec did not match any assets at %s", displayRevision(revision))
	}
	return restored, nil
}

func (r *Repository) Inspect(revision, assetPath string) (Inspection, error) {
	entry, err := r.EntryAt(revision, assetPath)
	if err != nil {
		return Inspection{}, err
	}
	data, err := r.Load(entry.ManifestOID)
	if err != nil {
		return Inspection{}, err
	}
	var manifest imaging.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Inspection{}, fmt.Errorf("decode manifest: %w", err)
	}
	return Inspection{Revision: displayRevision(revision), Entry: entry, Manifest: manifest}, nil
}

func (r *Repository) Lineage(assetPath string) ([]LineageNode, error) {
	records, err := r.Log(assetPath, 0)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return nil, err
	}
	path, err := r.relativePath(absolute)
	if err != nil {
		return nil, err
	}
	nodes := make([]LineageNode, 0, len(records))
	for _, record := range records {
		entry := record.Commit.Tree[path]
		nodes = append(nodes, LineageNode{
			CommitOID:  record.OID,
			Parent:     record.Commit.Parent,
			Author:     record.Commit.Author,
			Message:    record.Commit.Message,
			CreatedAt:  record.Commit.CreatedAt,
			ContentOID: entry.ContentOID,
			RecipeOID:  entry.RecipeOID,
			VisualHash: entry.VisualHash,
			Width:      entry.Width,
			Height:     entry.Height,
		})
	}
	return nodes, nil
}

func (r *Repository) BlamePoint(assetPath string, x, y int, options imaging.DiffOptions) (BlameResult, error) {
	if x < 0 || y < 0 {
		return BlameResult{}, errors.New("point coordinates must be non-negative")
	}
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return BlameResult{}, err
	}
	path, err := r.relativePath(absolute)
	if err != nil {
		return BlameResult{}, err
	}
	oid, head, err := r.ResolveCommit("HEAD")
	if err != nil {
		return BlameResult{}, err
	}
	headEntry, exists := head.Tree[path]
	if !exists {
		return BlameResult{}, fmt.Errorf("asset %s does not exist at HEAD", path)
	}
	if headEntry.Width > 0 && headEntry.Height > 0 && (x >= headEntry.Width || y >= headEntry.Height) {
		return BlameResult{}, fmt.Errorf("point %d,%d is outside %dx%d image", x, y, headEntry.Width, headEntry.Height)
	}

	for oid != "" {
		commit, err := r.ReadCommit(oid)
		if err != nil {
			return BlameResult{}, err
		}
		currentEntry, currentExists := commit.Tree[path]
		var parentTree map[string]Entry
		if commit.Parent == "" {
			parentTree = map[string]Entry{}
		} else {
			parent, err := r.ReadCommit(commit.Parent)
			if err != nil {
				return BlameResult{}, err
			}
			parentTree = parent.Tree
		}
		parentEntry, parentExists := parentTree[path]
		if currentExists && (!parentExists || currentEntry.ContentOID != parentEntry.ContentOID) {
			queryX, queryY := x, y
			if headEntry.Width > 0 && currentEntry.Width > 0 {
				queryX = x * currentEntry.Width / headEntry.Width
			}
			if headEntry.Height > 0 && currentEntry.Height > 0 {
				queryY = y * currentEntry.Height / headEntry.Height
			}
			if !parentExists {
				region := imaging.BoundingBox{Width: currentEntry.Width, Height: currentEntry.Height}
				return blameFromCommit(path, x, y, oid, commit, currentEntry, region, "asset-introduction"), nil
			}
			oldData, err := r.Load(parentEntry.ContentOID)
			if err != nil {
				return BlameResult{}, err
			}
			newData, err := r.Load(currentEntry.ContentOID)
			if err != nil {
				return BlameResult{}, err
			}
			visual, compareErr := imaging.CompareReaders(bytes.NewReader(oldData), bytes.NewReader(newData), options)
			if compareErr == nil {
				for _, changed := range visual.Regions {
					if boxContains(changed.BoundingBox, queryX, queryY) {
						confidence := "exact-pixel-region"
						if visual.Geometry.Type != "identity" {
							confidence = "normalized-coordinate-after-geometry-change"
						}
						return blameFromCommit(path, x, y, oid, commit, currentEntry, changed.BoundingBox, confidence), nil
					}
				}
			} else if !errors.Is(compareErr, imaging.ErrUnsupportedVisualFormat) {
				return BlameResult{}, compareErr
			}
		}
		oid = commit.Parent
	}
	return BlameResult{}, fmt.Errorf("no visual change found for point %d,%d in %s", x, y, path)
}

func blameFromCommit(path string, x, y int, oid string, commit Commit, entry Entry, region imaging.BoundingBox, confidence string) BlameResult {
	return BlameResult{
		Path:       path,
		Point:      [2]int{x, y},
		CommitOID:  oid,
		Author:     commit.Author,
		Message:    commit.Message,
		CreatedAt:  commit.CreatedAt,
		ContentOID: entry.ContentOID,
		RecipeOID:  entry.RecipeOID,
		Region:     region,
		Confidence: confidence,
	}
}

func boxContains(box imaging.BoundingBox, x, y int) bool {
	return x >= box.X && y >= box.Y && x < box.X+box.Width && y < box.Y+box.Height
}
