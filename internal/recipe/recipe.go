package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const Schema = "pixlog.recipe/v1"

type CaptureFidelity string

const (
	FidelityExactRequest       CaptureFidelity = "exact-request"
	FidelityExactCommand       CaptureFidelity = "exact-command"
	FidelityEmbeddedMetadata   CaptureFidelity = "embedded-metadata"
	FidelityApplicationHistory CaptureFidelity = "application-history"
	FidelityUIObserved         CaptureFidelity = "ui-observed"
	FidelityInferred           CaptureFidelity = "inferred"
)

type ReproducibilityStatus string

const (
	ReproducibilityExact               ReproducibilityStatus = "exact"
	ReproducibilityBestEffort          ReproducibilityStatus = "best-effort"
	ReproducibilityRequestReproducible ReproducibilityStatus = "request-reproducible"
	ReproducibilityProvenanceOnly      ReproducibilityStatus = "provenance-only"
	ReproducibilityInferred            ReproducibilityStatus = "inferred"
	ReproducibilityUnverified          ReproducibilityStatus = "unverified"
)

var sha256OIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (fidelity CaptureFidelity) Valid() bool {
	switch fidelity {
	case FidelityExactRequest, FidelityExactCommand, FidelityEmbeddedMetadata,
		FidelityApplicationHistory, FidelityUIObserved, FidelityInferred:
		return true
	default:
		return false
	}
}

func (status ReproducibilityStatus) Valid() bool {
	switch status {
	case ReproducibilityExact, ReproducibilityBestEffort,
		ReproducibilityRequestReproducible, ReproducibilityProvenanceOnly,
		ReproducibilityInferred, ReproducibilityUnverified:
		return true
	default:
		return false
	}
}

type Change struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

func Normalize(data []byte) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode recipe JSON: %w", err)
	}
	if document == nil {
		return nil, errors.New("recipe must be a JSON object")
	}
	schema, _ := document["schema"].(string)
	if schema == "" {
		document["schema"] = Schema
	} else if schema != Schema {
		return nil, fmt.Errorf("unsupported recipe schema %q", schema)
	}
	kind, _ := document["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		return nil, errors.New("recipe field \"kind\" is required")
	}
	if err := validateCapture(document["capture"]); err != nil {
		return nil, err
	}
	if err := validateReproducibility(document["reproducibility"]); err != nil {
		return nil, err
	}
	if err := validateVendor(document["vendor"]); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("normalize recipe: %w", err)
	}
	return normalized, nil
}

func FromEmbedded(metadata map[string]string) ([]byte, bool, error) {
	if len(metadata) == 0 {
		return nil, false, nil
	}
	values := map[string]string{}
	for key, value := range metadata {
		values[strings.ToLower(key)] = value
	}
	if workflow := values["workflow"]; workflow != "" {
		var workflowValue any
		if err := json.Unmarshal([]byte(workflow), &workflowValue); err != nil {
			workflowValue = workflow
		}
		document := map[string]any{
			"schema": Schema,
			"kind":   "ai-generation",
			"tool": map[string]any{
				"name": "ComfyUI",
			},
			"workflow": workflowValue,
			"capture": map[string]any{
				"source":          "png-embedded-metadata",
				"adapter":         "comfyui-png",
				"adapter_version": "1",
				"fidelity":        FidelityEmbeddedMetadata,
			},
			"reproducibility": map[string]any{
				"status": ReproducibilityBestEffort,
			},
		}
		if prompt := values["prompt"]; prompt != "" {
			var promptValue any
			if err := json.Unmarshal([]byte(prompt), &promptValue); err != nil {
				promptValue = prompt
			}
			document["parameters"] = map[string]any{"prompt_graph": promptValue}
		}
		data, err := json.Marshal(document)
		return data, true, err
	}
	if parameters := values["parameters"]; parameters != "" {
		document := map[string]any{
			"schema": Schema,
			"kind":   "ai-generation",
			"tool": map[string]any{
				"name": "AUTOMATIC1111",
			},
			"parameters": map[string]any{
				"raw": parameters,
			},
			"capture": map[string]any{
				"source":          "png-embedded-metadata",
				"adapter":         "automatic1111-png",
				"adapter_version": "1",
				"fidelity":        FidelityEmbeddedMetadata,
			},
			"reproducibility": map[string]any{
				"status": ReproducibilityBestEffort,
			},
		}
		data, err := json.Marshal(document)
		return data, true, err
	}
	return nil, false, nil
}

func validateCapture(value any) error {
	if value == nil {
		return nil
	}
	capture, ok := value.(map[string]any)
	if !ok {
		return errors.New("recipe field \"capture\" must be an object")
	}
	value, exists := capture["fidelity"]
	if !exists {
		return nil
	}
	fidelity, ok := value.(string)
	if !ok {
		return errors.New("recipe field \"capture.fidelity\" must be a string")
	}
	if !CaptureFidelity(fidelity).Valid() {
		return fmt.Errorf("unsupported recipe capture fidelity %q", fidelity)
	}
	return nil
}

func validateReproducibility(value any) error {
	if value == nil {
		return nil
	}
	reproducibility, ok := value.(map[string]any)
	if !ok {
		return errors.New("recipe field \"reproducibility\" must be an object")
	}
	value, exists := reproducibility["status"]
	if !exists {
		return nil
	}
	status, ok := value.(string)
	if !ok {
		return errors.New("recipe field \"reproducibility.status\" must be a string")
	}
	if !ReproducibilityStatus(status).Valid() {
		return fmt.Errorf("unsupported recipe reproducibility status %q", status)
	}
	return nil
}

func validateVendor(value any) error {
	if value == nil {
		return nil
	}
	vendor, ok := value.(map[string]any)
	if !ok {
		return errors.New("recipe field \"vendor\" must be an object")
	}
	value, exists := vendor["raw_payload_oid"]
	if !exists {
		return nil
	}
	oid, ok := value.(string)
	if !ok || !sha256OIDPattern.MatchString(oid) {
		return errors.New("recipe field \"vendor.raw_payload_oid\" must be a sha256 OID")
	}
	return nil
}

func Diff(oldData, newData []byte) ([]Change, error) {
	var oldValue, newValue any
	if err := json.Unmarshal(oldData, &oldValue); err != nil {
		return nil, fmt.Errorf("decode old recipe: %w", err)
	}
	if err := json.Unmarshal(newData, &newValue); err != nil {
		return nil, fmt.Errorf("decode new recipe: %w", err)
	}
	oldFields, newFields := map[string]any{}, map[string]any{}
	flatten("", oldValue, oldFields)
	flatten("", newValue, newFields)
	keys := map[string]struct{}{}
	for key := range oldFields {
		keys[key] = struct{}{}
	}
	for key := range newFields {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changes := []Change{}
	for _, key := range ordered {
		oldField, oldExists := oldFields[key]
		newField, newExists := newFields[key]
		oldJSON, _ := json.Marshal(oldField)
		newJSON, _ := json.Marshal(newField)
		if oldExists != newExists || string(oldJSON) != string(newJSON) {
			change := Change{Field: key}
			if oldExists {
				change.Old = oldField
			}
			if newExists {
				change.New = newField
			}
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func flatten(prefix string, value any, target map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flatten(path, child, target)
		}
	case []any:
		for index, child := range typed {
			flatten(fmt.Sprintf("%s[%d]", prefix, index), child, target)
		}
	default:
		target[prefix] = value
	}
}
