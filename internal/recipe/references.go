package recipe

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Reference struct {
	OID  string `json:"oid"`
	Kind string `json:"kind"`
	Role string `json:"role"`
	Path string `json:"path"`
}

func References(data []byte) ([]Reference, error) {
	normalized, err := Normalize(data)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(normalized, &document); err != nil {
		return nil, fmt.Errorf("decode recipe references: %w", err)
	}
	references := []Reference{}
	walkReferences(document, "", "", &references)
	sort.Slice(references, func(left, right int) bool {
		if references[left].Path == references[right].Path {
			return references[left].OID < references[right].OID
		}
		return references[left].Path < references[right].Path
	})
	return references, nil
}

func walkReferences(value any, path, inheritedRole string, references *[]Reference) {
	switch typed := value.(type) {
	case map[string]any:
		role := inheritedRole
		if value, ok := typed["role"].(string); ok && value != "" {
			role = value
		}
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			walkReferences(child, childPath, role, references)
		}
	case []any:
		for index, child := range typed {
			walkReferences(child, fmt.Sprintf("%s[%d]", path, index), inheritedRole, references)
		}
	case string:
		if !sha256OIDPattern.MatchString(typed) {
			return
		}
		kind, role := referenceKindAndRole(path, inheritedRole)
		*references = append(*references, Reference{OID: typed, Kind: kind, Role: role, Path: path})
	}
}

func referenceKindAndRole(path, role string) (string, string) {
	lower := strings.ToLower(path)
	kind := "artifact"
	switch {
	case strings.Contains(lower, "recipe"):
		kind = "recipe"
	case strings.Contains(lower, "model"), strings.Contains(lower, "lora"), strings.Contains(lower, "vae"), strings.Contains(lower, "controlnet"), strings.Contains(lower, "checkpoint"):
		kind = "model"
	case strings.Contains(lower, "mask"):
		kind = "mask"
	case strings.Contains(lower, "workflow"):
		kind = "workflow"
	case strings.Contains(lower, "raw_payload"), strings.Contains(lower, "credential"):
		kind = "vendor-payload"
	case strings.Contains(lower, "output"):
		kind = "output"
	case strings.Contains(lower, "parent"), strings.Contains(lower, "input"), strings.Contains(lower, "reference"):
		kind = "input"
	}
	if role == "" {
		role = kind
	}
	return kind, role
}
