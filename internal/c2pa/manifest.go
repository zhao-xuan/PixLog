package c2pa

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type ManifestDefinition struct {
	ClaimGenerator string      `json:"claim_generator"`
	Title          string      `json:"title"`
	Format         string      `json:"format"`
	Assertions     []Assertion `json:"assertions"`
}

type Assertion struct {
	Label string `json:"label"`
	Data  any    `json:"data"`
}

type Action struct {
	Action        string `json:"action"`
	SoftwareAgent string `json:"softwareAgent,omitempty"`
}

func ManifestFromRecipe(assetPath, mediaType, pixlogVersion string, recipeData []byte) (ManifestDefinition, error) {
	var document map[string]any
	if err := json.Unmarshal(recipeData, &document); err != nil {
		return ManifestDefinition{}, fmt.Errorf("decode PixLog recipe: %w", err)
	}
	kind, _ := document["kind"].(string)
	if kind == "" {
		return ManifestDefinition{}, fmt.Errorf("PixLog recipe has no kind")
	}
	softwareAgent := recipeSoftwareAgent(document)
	actions := []Action{{Action: c2paAction(kind, normalizedOperation(document)), SoftwareAgent: softwareAgent}}
	if parents, ok := document["parents"].([]any); ok && len(parents) > 0 {
		actions = append([]Action{{Action: "c2pa.opened", SoftwareAgent: softwareAgent}}, actions...)
	}
	return ManifestDefinition{
		ClaimGenerator: "PixLog/" + pixlogVersion,
		Title:          filepath.Base(assetPath),
		Format:         mediaType,
		Assertions: []Assertion{{
			Label: "c2pa.actions",
			Data:  map[string]any{"actions": actions},
		}},
	}, nil
}

func recipeSoftwareAgent(document map[string]any) string {
	if tool, ok := document["tool"].(map[string]any); ok {
		if name, ok := tool["name"].(string); ok && name != "" {
			return name
		}
	}
	if capture, ok := document["capture"].(map[string]any); ok {
		if adapter, ok := capture["adapter"].(string); ok && adapter != "" {
			return adapter
		}
	}
	return "PixLog"
}

func normalizedOperation(document map[string]any) string {
	if normalized, ok := document["normalized"].(map[string]any); ok {
		if operation, ok := normalized["operation"].(string); ok {
			return operation
		}
	}
	if operation, ok := document["operation"].(string); ok {
		return operation
	}
	return ""
}

func c2paAction(kind, operation string) string {
	value := strings.ToLower(kind + " " + operation)
	switch {
	case strings.Contains(value, "crop"):
		return "c2pa.cropped"
	case strings.Contains(value, "resize"):
		return "c2pa.resized"
	case strings.Contains(value, "color"), strings.Contains(value, "curve"):
		return "c2pa.color_adjustments"
	case strings.Contains(value, "place"), strings.Contains(value, "reference"):
		return "c2pa.placed"
	case strings.Contains(value, "ai-generation"), strings.Contains(value, "generation"), strings.Contains(value, "created"):
		return "c2pa.created"
	default:
		return "c2pa.edited"
	}
}
