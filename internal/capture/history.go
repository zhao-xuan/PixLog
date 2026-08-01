package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhao-xuan/PixLog/internal/recipe"
)

type ApplicationHistoryResult struct {
	Recipe  []byte
	Payload []byte
	Entries int
}

func ApplicationHistoryRecipe(platform, outputOID string, logData []byte) (ApplicationHistoryResult, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "photoshop" {
		return ApplicationHistoryResult{}, fmt.Errorf("unsupported application history platform %q", platform)
	}
	if !strings.HasPrefix(outputOID, "sha256:") {
		return ApplicationHistoryResult{}, errors.New("application history output must have a SHA-256 OID")
	}
	payload := RedactText(logData)
	entries := 0
	for _, line := range bytes.Split(payload, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) > 0 {
			entries++
		}
	}
	if entries == 0 {
		return ApplicationHistoryResult{}, errors.New("application history log is empty")
	}
	document := map[string]any{
		"schema": recipe.Schema,
		"kind":   "application-history",
		"tool":   map[string]any{"name": "Adobe Photoshop"},
		"normalized": map[string]any{
			"operation": "application.history-log",
			"entries":   entries,
		},
		"capture": map[string]any{
			"source":          "photoshop-history-log",
			"adapter":         "photoshop-history-log",
			"adapter_version": "1",
			"fidelity":        recipe.FidelityApplicationHistory,
			"captured_at":     time.Now().UTC(),
			"unknown_fields": []string{
				"exact_operation_parameters",
				"layer_state",
				"brush_paths",
				"selection_masks",
			},
		},
		"reproducibility": map[string]any{"status": recipe.ReproducibilityProvenanceOnly},
		"outputs":         []any{map[string]any{"asset": outputOID, "role": "edited-document"}},
	}
	data, err := json.Marshal(document)
	if err != nil {
		return ApplicationHistoryResult{}, err
	}
	normalized, err := recipe.Normalize(data)
	if err != nil {
		return ApplicationHistoryResult{}, err
	}
	return ApplicationHistoryResult{Recipe: normalized, Payload: payload, Entries: entries}, nil
}
