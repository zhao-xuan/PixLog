package repository

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type ReproductionCommand struct {
	Executable       string   `json:"executable"`
	Arguments        []string `json:"arguments,omitempty"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
}

type ReproductionPlan struct {
	Revision      string               `json:"revision"`
	Asset         string               `json:"asset"`
	RecipeOID     string               `json:"recipe_oid"`
	Kind          string               `json:"kind"`
	Command       *ReproductionCommand `json:"command,omitempty"`
	SourceControl *GitContext          `json:"source_control,omitempty"`
}

func (g *GitRepository) PlanReproduction(revision, assetPath string) (ReproductionPlan, error) {
	if revision == "" {
		revision = "HEAD"
	}
	recipeOID, data, err := g.RecipeData(revision, assetPath)
	if err != nil {
		return ReproductionPlan{}, err
	}
	var document struct {
		Kind          string               `json:"kind"`
		Command       *ReproductionCommand `json:"command"`
		SourceControl *GitContext          `json:"source_control"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return ReproductionPlan{}, fmt.Errorf("decode recipe %s: %w", recipeOID, err)
	}
	if document.Command != nil {
		workingDirectory := filepath.Clean(filepath.FromSlash(document.Command.WorkingDirectory))
		if document.Command.WorkingDirectory == "" {
			workingDirectory = "."
		}
		if filepath.IsAbs(workingDirectory) || workingDirectory == ".." || strings.HasPrefix(workingDirectory, ".."+string(filepath.Separator)) {
			return ReproductionPlan{}, fmt.Errorf("recipe %s has unsafe working directory %q", recipeOID, document.Command.WorkingDirectory)
		}
		document.Command.WorkingDirectory = filepath.ToSlash(workingDirectory)
	}
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return ReproductionPlan{}, err
	}
	relative, err := g.relativePath(absolute)
	if err != nil {
		return ReproductionPlan{}, err
	}
	return ReproductionPlan{
		Revision:      revision,
		Asset:         relative,
		RecipeOID:     recipeOID,
		Kind:          document.Kind,
		Command:       document.Command,
		SourceControl: document.SourceControl,
	}, nil
}

func (g *GitRepository) ValidateReproduction(plan ReproductionPlan) error {
	if plan.Command == nil || strings.TrimSpace(plan.Command.Executable) == "" {
		return fmt.Errorf("recipe %s does not contain an executable command; use its generation adapter", plan.RecipeOID)
	}
	for _, argument := range plan.Command.Arguments {
		if argument == "<redacted>" {
			return fmt.Errorf("recipe %s contains redacted command arguments", plan.RecipeOID)
		}
	}
	if plan.SourceControl == nil {
		return fmt.Errorf("recipe %s does not record a Git source state", plan.RecipeOID)
	}
	if plan.SourceControl.WorktreeDirty {
		return fmt.Errorf("recipe %s was captured from a dirty worktree and cannot be reproduced exactly", plan.RecipeOID)
	}
	current, err := DiscoverGitContext(g.Root)
	if err != nil {
		return err
	}
	if current == nil || current.HeadOID != plan.SourceControl.HeadOID || current.IndexTreeOID != plan.SourceControl.IndexTreeOID {
		return fmt.Errorf("current Git HEAD/index do not match recipe source state; checkout %s first", plan.SourceControl.HeadOID)
	}
	if current.WorktreeDirty {
		return fmt.Errorf("current Git worktree is dirty")
	}
	return nil
}
