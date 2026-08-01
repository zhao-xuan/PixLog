package repository

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/zhao-xuan/PixLog/internal/recipe"
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
	Request       *ReproductionRequest `json:"request,omitempty"`
	SourceControl *GitContext          `json:"source_control,omitempty"`
}

type ReproductionRequest struct {
	Provider    string                 `json:"provider"`
	Adapter     string                 `json:"adapter"`
	Method      string                 `json:"method"`
	Path        string                 `json:"path"`
	ContentType string                 `json:"content_type,omitempty"`
	RequestOID  string                 `json:"request_oid"`
	Fidelity    recipe.CaptureFidelity `json:"fidelity"`
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
		Capture       struct {
			Adapter  string                 `json:"adapter"`
			Fidelity recipe.CaptureFidelity `json:"fidelity"`
		} `json:"capture"`
		Operations []struct {
			Fidelity   recipe.CaptureFidelity `json:"fidelity"`
			Normalized json.RawMessage        `json:"normalized"`
		} `json:"operations"`
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
	request := reproductionRequestFromOperations(document.Capture.Adapter, document.Capture.Fidelity, document.Operations)
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
		Request:       request,
		SourceControl: document.SourceControl,
	}, nil
}

func (g *GitRepository) ValidateRequestReproduction(plan ReproductionPlan, baseURL string) (*url.URL, error) {
	if plan.Request == nil {
		return nil, fmt.Errorf("recipe %s does not contain an exact captured request", plan.RecipeOID)
	}
	if plan.Request.Fidelity != recipe.FidelityExactRequest {
		return nil, fmt.Errorf("recipe %s request fidelity is %s, not exact-request", plan.RecipeOID, plan.Request.Fidelity)
	}
	if _, err := parseOID(plan.Request.RequestOID); err != nil {
		return nil, fmt.Errorf("recipe %s has invalid request object: %w", plan.RecipeOID, err)
	}
	method := strings.ToUpper(plan.Request.Method)
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return nil, fmt.Errorf("recipe %s uses unsupported replay method %q", plan.RecipeOID, plan.Request.Method)
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || base.User != nil || base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("invalid explicit reproduction base URL %q", baseURL)
	}
	if base.Path != "" && base.Path != "/" || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("reproduction base URL must be an origin without path, query, or fragment")
	}
	relative, err := url.Parse(plan.Request.Path)
	if err != nil || relative.IsAbs() || relative.Host != "" || !strings.HasPrefix(relative.Path, "/") {
		return nil, fmt.Errorf("recipe %s has unsafe request path %q", plan.RecipeOID, plan.Request.Path)
	}
	if containsReproductionRedaction(plan.Request.Path, relative) {
		return nil, fmt.Errorf("recipe %s request path contains redacted fields", plan.RecipeOID)
	}
	return base.ResolveReference(relative), nil
}

func containsReproductionRedaction(rawPath string, parsed *url.URL) bool {
	if strings.Contains(rawPath, "[REDACTED]") {
		return true
	}
	decoded, err := url.QueryUnescape(rawPath)
	if err == nil && strings.Contains(decoded, "[REDACTED]") {
		return true
	}
	for _, values := range parsed.Query() {
		for _, value := range values {
			if strings.Contains(value, "[REDACTED]") {
				return true
			}
		}
	}
	return false
}

func (g *GitRepository) ReproductionRequestData(plan ReproductionPlan) ([]byte, error) {
	if plan.Request == nil {
		return nil, fmt.Errorf("recipe %s has no captured request", plan.RecipeOID)
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return nil, err
	}
	data, err := g.loadMediaObject(store, plan.Request.RequestOID)
	if err != nil {
		return nil, fmt.Errorf("load reproduction request %s: %w", plan.Request.RequestOID, err)
	}
	return data, nil
}

func reproductionRequestFromOperations(adapter string, captureFidelity recipe.CaptureFidelity, operations []struct {
	Fidelity   recipe.CaptureFidelity `json:"fidelity"`
	Normalized json.RawMessage        `json:"normalized"`
}) *ReproductionRequest {
	for _, operation := range operations {
		fidelity := operation.Fidelity
		if fidelity == "" {
			fidelity = captureFidelity
		}
		if fidelity != recipe.FidelityExactRequest {
			continue
		}
		var normalized struct {
			Platform    string `json:"platform"`
			Method      string `json:"method"`
			Path        string `json:"path"`
			ContentType string `json:"content_type"`
			RequestOID  string `json:"request_oid"`
		}
		if err := json.Unmarshal(operation.Normalized, &normalized); err != nil || normalized.RequestOID == "" {
			continue
		}
		method := strings.ToUpper(normalized.Method)
		if method != "POST" && method != "PUT" && method != "PATCH" {
			continue
		}
		return &ReproductionRequest{
			Provider: normalized.Platform, Adapter: adapter, Method: method,
			Path: normalized.Path, ContentType: normalized.ContentType,
			RequestOID: normalized.RequestOID, Fidelity: fidelity,
		}
	}
	return nil
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
