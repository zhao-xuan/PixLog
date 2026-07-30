package repository

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/imaging"
)

type gitAssetRevision struct {
	CommitOID string
	Parent    string
	Author    string
	Message   string
	CreatedAt time.Time
	Path      string
	Entry     Entry
	Data      []byte
}

func (g *GitRepository) Lineage(assetPath string) ([]LineageNode, error) {
	history, err := g.assetHistory(assetPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]LineageNode, 0, len(history))
	for _, revision := range history {
		nodes = append(nodes, LineageNode{
			CommitOID:  revision.CommitOID,
			Parent:     revision.Parent,
			Author:     revision.Author,
			Message:    revision.Message,
			CreatedAt:  revision.CreatedAt,
			ContentOID: revision.Entry.ContentOID,
			RecipeOID:  revision.Entry.RecipeOID,
			VisualHash: revision.Entry.VisualHash,
			Width:      revision.Entry.Width,
			Height:     revision.Entry.Height,
		})
	}
	return nodes, nil
}

func (g *GitRepository) BlamePoint(assetPath string, x, y int, options imaging.DiffOptions) (BlameResult, error) {
	if x < 0 || y < 0 {
		return BlameResult{}, errors.New("point coordinates must be non-negative")
	}
	history, err := g.assetHistory(assetPath)
	if err != nil {
		return BlameResult{}, err
	}
	head := history[0]
	if head.Entry.Width > 0 && head.Entry.Height > 0 && (x >= head.Entry.Width || y >= head.Entry.Height) {
		return BlameResult{}, fmt.Errorf("point %d,%d is outside %dx%d image", x, y, head.Entry.Width, head.Entry.Height)
	}
	for index, current := range history {
		queryX, queryY := x, y
		if head.Entry.Width > 0 && current.Entry.Width > 0 {
			queryX = x * current.Entry.Width / head.Entry.Width
		}
		if head.Entry.Height > 0 && current.Entry.Height > 0 {
			queryY = y * current.Entry.Height / head.Entry.Height
		}
		if index == len(history)-1 {
			region := imaging.BoundingBox{Width: current.Entry.Width, Height: current.Entry.Height}
			return gitBlameResult(assetPath, x, y, current, region, "asset-introduction"), nil
		}
		previous := history[index+1]
		if current.Entry.ContentOID == previous.Entry.ContentOID {
			continue
		}
		visual, compareErr := imaging.CompareReaders(bytes.NewReader(previous.Data), bytes.NewReader(current.Data), options)
		if errors.Is(compareErr, imaging.ErrUnsupportedVisualFormat) {
			continue
		}
		if compareErr != nil {
			return BlameResult{}, compareErr
		}
		for _, changed := range visual.Regions {
			if boxContains(changed.BoundingBox, queryX, queryY) {
				confidence := "exact-pixel-region"
				if visual.Geometry.Type != "identity" {
					confidence = "normalized-coordinate-after-geometry-change"
				}
				return gitBlameResult(assetPath, x, y, current, changed.BoundingBox, confidence), nil
			}
		}
	}
	return BlameResult{}, fmt.Errorf("no visual change found for point %d,%d in %s", x, y, assetPath)
}

func (g *GitRepository) assetHistory(assetPath string) ([]gitAssetRevision, error) {
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return nil, err
	}
	currentPath, err := g.relativePath(absolute)
	if err != nil {
		return nil, err
	}
	logData, err := gitBytes(g.Root, "log", "--follow", "--format=%H%x00", "--", currentPath)
	if err != nil {
		return nil, fmt.Errorf("read Git history for %s: %w", currentPath, err)
	}
	commitOIDs := []string{}
	for _, value := range bytes.Split(logData, []byte{0}) {
		if oid := strings.TrimSpace(string(value)); oid != "" {
			commitOIDs = append(commitOIDs, oid)
		}
	}
	if len(commitOIDs) == 0 {
		return nil, fmt.Errorf("asset %s has no Git history", currentPath)
	}
	history := make([]gitAssetRevision, 0, len(commitOIDs))
	for _, commitOID := range commitOIDs {
		parent, author, createdAt, message, err := g.gitCommitMetadata(commitOID)
		if err != nil {
			return nil, err
		}
		snapshot, err := g.revisionSnapshot(commitOID, commitOID)
		if err != nil {
			return nil, err
		}
		if entry, exists := snapshot.entries[currentPath]; exists {
			history = append(history, gitAssetRevision{
				CommitOID: commitOID,
				Parent:    parent,
				Author:    author,
				Message:   message,
				CreatedAt: createdAt,
				Path:      currentPath,
				Entry:     entry,
				Data:      snapshot.data[currentPath],
			})
		}
		if parent != "" {
			currentPath, err = g.parentPathAcrossRename(parent, commitOID, currentPath)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("asset %s does not exist in its Git history", assetPath)
	}
	return history, nil
}

func (g *GitRepository) gitCommitMetadata(commitOID string) (string, string, time.Time, string, error) {
	data, err := gitBytes(g.Root, "show", "-s", "--format=%P%x00%an <%ae>%x00%aI%x00%B", commitOID)
	if err != nil {
		return "", "", time.Time{}, "", err
	}
	fields := bytes.SplitN(data, []byte{0}, 4)
	if len(fields) != 4 {
		return "", "", time.Time{}, "", fmt.Errorf("read metadata for Git commit %s", commitOID)
	}
	parents := strings.Fields(strings.TrimSpace(string(fields[0])))
	parent := ""
	if len(parents) > 0 {
		parent = parents[0]
	}
	createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(string(fields[2])))
	if err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("parse date for Git commit %s: %w", commitOID, err)
	}
	return parent, strings.TrimSpace(string(fields[1])), createdAt, strings.TrimSpace(string(fields[3])), nil
}

func (g *GitRepository) parentPathAcrossRename(parent, commitOID, currentPath string) (string, error) {
	data, err := gitBytes(g.Root, "diff-tree", "--no-commit-id", "--name-status", "-r", "-M", "-z", parent, commitOID)
	if err != nil {
		return "", err
	}
	fields := bytes.Split(data, []byte{0})
	for index := 0; index < len(fields); {
		status := string(fields[index])
		index++
		if status == "" {
			continue
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if index+1 >= len(fields) {
				break
			}
			oldPath := filepath.ToSlash(string(fields[index]))
			newPath := filepath.ToSlash(string(fields[index+1]))
			index += 2
			if newPath == currentPath {
				return oldPath, nil
			}
			continue
		}
		index++
	}
	return currentPath, nil
}

func gitBlameResult(requestedPath string, x, y int, revision gitAssetRevision, region imaging.BoundingBox, confidence string) BlameResult {
	return BlameResult{
		Path:       filepath.ToSlash(requestedPath),
		Point:      [2]int{x, y},
		CommitOID:  revision.CommitOID,
		Author:     revision.Author,
		Message:    revision.Message,
		CreatedAt:  revision.CreatedAt,
		ContentOID: revision.Entry.ContentOID,
		RecipeOID:  revision.Entry.RecipeOID,
		Region:     region,
		Confidence: confidence,
	}
}
