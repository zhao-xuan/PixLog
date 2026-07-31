package capture

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

type SessionDetail struct {
	Session     repository.CaptureSession      `json:"session"`
	Events      []repository.CaptureEvent      `json:"events"`
	Jobs        []repository.CaptureJob        `json:"jobs"`
	Checkpoints []repository.CaptureCheckpoint `json:"checkpoints"`
	Artifacts   []repository.CaptureArtifact   `json:"artifacts"`
}

type FinalizeResult struct {
	SessionID  string `json:"session_id"`
	AssetPath  string `json:"asset_path"`
	ContentOID string `json:"content_oid"`
	RecipeOID  string `json:"recipe_oid"`
}

func LoadSession(start, sessionID string) (SessionDetail, error) {
	journal, err := repository.OpenProvenanceJournal(start)
	if err != nil {
		return SessionDetail{}, err
	}
	defer journal.Close()
	session, err := journal.CaptureSession(sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	events, err := journal.CaptureEvents(sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	jobs, err := journal.CaptureJobs(sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	checkpoints, err := journal.CaptureCheckpoints(sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	artifacts, err := journal.CaptureArtifacts(sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{
		Session: session, Events: events, Jobs: jobs,
		Checkpoints: checkpoints, Artifacts: artifacts,
	}, nil
}

func FinalizeSession(start, sessionID, assetPath, kind string) (FinalizeResult, error) {
	detail, err := LoadSession(start, sessionID)
	if err != nil {
		return FinalizeResult{}, err
	}
	if len(detail.Events) == 0 && len(detail.Jobs) == 0 {
		return FinalizeResult{}, fmt.Errorf("capture session %s has no events or jobs", sessionID)
	}
	repo, err := repository.OpenGit(start)
	if err != nil {
		return FinalizeResult{}, err
	}
	absolutePath, err := filepath.Abs(assetPath)
	if err != nil {
		return FinalizeResult{}, err
	}
	resolvedRoot := repo.Root
	if resolved, resolveErr := filepath.EvalSymlinks(repo.Root); resolveErr == nil {
		resolvedRoot = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolutePath); resolveErr == nil {
		absolutePath = resolved
	}
	contentOID, err := repository.HashFile(absolutePath)
	if err != nil {
		return FinalizeResult{}, err
	}
	relativePath, err := filepath.Rel(resolvedRoot, absolutePath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return FinalizeResult{}, fmt.Errorf("asset %s is outside the Git repository", assetPath)
	}
	relativePath = filepath.ToSlash(relativePath)
	if strings.TrimSpace(kind) == "" {
		kind = defaultRecipeKind(detail.Session.Adapter)
	}
	fidelity := aggregateFidelity(detail.Events)
	reproducibility := reproducibilityForCapture(detail.Session.Adapter, fidelity)
	operations := make([]map[string]any, 0, len(detail.Events))
	for _, event := range detail.Events {
		operation := map[string]any{
			"id":          event.ID,
			"sequence":    event.Sequence,
			"type":        event.EventType,
			"occurred_at": event.OccurredAt,
			"fidelity":    event.Fidelity,
			"normalized":  json.RawMessage(event.Normalized),
		}
		if event.TransactionID != "" {
			operation["transaction_id"] = event.TransactionID
		}
		if event.RawPayloadOID != "" {
			operation["raw_payload_oid"] = event.RawPayloadOID
		}
		operations = append(operations, operation)
	}
	parents := []map[string]any{}
	outputs := []map[string]any{{"asset": contentOID, "path": relativePath, "role": "output"}}
	for _, artifact := range detail.Artifacts {
		entry := map[string]any{"asset": artifact.ContentOID, "role": artifact.Role}
		if artifact.AssetPath != "" {
			entry["path"] = artifact.AssetPath
		}
		if artifact.RecipeOID != "" {
			entry["recipe"] = artifact.RecipeOID
		}
		if artifact.Role == "output" || artifact.Role == "result" {
			if artifact.ContentOID != contentOID {
				outputs = append(outputs, entry)
			}
		} else {
			parents = append(parents, entry)
		}
	}
	finishedAt := detail.Session.EndedAt
	if finishedAt == nil {
		value := time.Now().UTC()
		finishedAt = &value
	}
	document := map[string]any{
		"schema": recipe.Schema,
		"kind":   kind,
		"tool": map[string]any{
			"name":            detail.Session.Application,
			"adapter":         detail.Session.Adapter,
			"adapter_version": detail.Session.AdapterVersion,
		},
		"capture": map[string]any{
			"source":          "pixlog-capture-daemon",
			"adapter":         detail.Session.Adapter,
			"adapter_version": detail.Session.AdapterVersion,
			"session_id":      detail.Session.ID,
			"fidelity":        fidelity,
			"captured_at":     detail.Session.StartedAt,
			"finished_at":     finishedAt,
		},
		"reproducibility": map[string]any{"status": reproducibility},
		"operations":      operations,
		"jobs":            detail.Jobs,
		"checkpoints":     detail.Checkpoints,
		"parents":         parents,
		"outputs":         outputs,
	}
	if len(detail.Session.Metadata) > 0 {
		document["session"] = map[string]any{
			"document_id": detail.Session.DocumentID,
			"metadata":    json.RawMessage(detail.Session.Metadata),
		}
	}
	recipeData, err := json.Marshal(document)
	if err != nil {
		return FinalizeResult{}, fmt.Errorf("encode capture recipe: %w", err)
	}
	recipeOID, err := repo.ImportRecipe(absolutePath, recipeData)
	if err != nil {
		return FinalizeResult{}, err
	}
	journal, err := repository.OpenProvenanceJournal(repo.Root)
	if err != nil {
		return FinalizeResult{}, err
	}
	defer journal.Close()
	if detail.Session.EndedAt == nil {
		if err := journal.FinishCaptureSession(sessionID, *finishedAt); err != nil {
			return FinalizeResult{}, err
		}
	}
	if _, err := journal.RecordCaptureArtifact(repository.CaptureArtifact{
		SessionID: sessionID, Role: "output", ContentOID: contentOID,
		RecipeOID: recipeOID, AssetPath: relativePath,
	}); err != nil {
		return FinalizeResult{}, err
	}
	return FinalizeResult{
		SessionID: sessionID, AssetPath: relativePath,
		ContentOID: contentOID, RecipeOID: recipeOID,
	}, nil
}

func defaultRecipeKind(adapter string) string {
	switch {
	case strings.Contains(adapter, "photoshop"):
		return "image-edit"
	case strings.Contains(adapter, "browser"):
		return "observed-generation"
	default:
		return "ai-generation"
	}
}

func aggregateFidelity(events []repository.CaptureEvent) recipe.CaptureFidelity {
	priority := map[recipe.CaptureFidelity]int{
		recipe.FidelityExactRequest:       0,
		recipe.FidelityExactCommand:       1,
		recipe.FidelityEmbeddedMetadata:   2,
		recipe.FidelityApplicationHistory: 3,
		recipe.FidelityUIObserved:         4,
		recipe.FidelityInferred:           5,
	}
	result := recipe.FidelityExactRequest
	highest := -1
	for _, event := range events {
		if value, exists := priority[event.Fidelity]; exists && value > highest {
			result, highest = event.Fidelity, value
		}
	}
	return result
}

func reproducibilityForCapture(adapter string, fidelity recipe.CaptureFidelity) recipe.ReproducibilityStatus {
	switch fidelity {
	case recipe.FidelityExactRequest:
		if strings.Contains(adapter, "openai") || strings.Contains(adapter, "firefly") {
			return recipe.ReproducibilityRequestReproducible
		}
		return recipe.ReproducibilityBestEffort
	case recipe.FidelityExactCommand, recipe.FidelityEmbeddedMetadata:
		return recipe.ReproducibilityBestEffort
	case recipe.FidelityInferred:
		return recipe.ReproducibilityInferred
	default:
		return recipe.ReproducibilityProvenanceOnly
	}
}
