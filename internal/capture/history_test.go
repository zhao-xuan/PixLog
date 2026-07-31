package capture

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplicationHistoryRecipeRedactsSecretsAndMarksFidelity(t *testing.T) {
	result, err := ApplicationHistoryRecipe("photoshop", "sha256:"+strings.Repeat("a", 64), []byte("Open\nAPI key: private-value\nAuthorization: Bearer bearer-secret\nx-api-key=vendor-secret\ngithub_token=github-secret\nCurves\n"))
	if err != nil {
		t.Fatalf("ApplicationHistoryRecipe: %v", err)
	}
	if strings.Contains(string(result.Payload), "private-value") || strings.Contains(string(result.Payload), "bearer-secret") || strings.Contains(string(result.Payload), "vendor-secret") || strings.Contains(string(result.Payload), "github-secret") || !strings.Contains(string(result.Payload), "[REDACTED]") {
		t.Fatalf("payload = %q", result.Payload)
	}
	var document map[string]any
	if err := json.Unmarshal(result.Recipe, &document); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	captureData := document["capture"].(map[string]any)
	reproducibility := document["reproducibility"].(map[string]any)
	if captureData["fidelity"] != "application-history" || reproducibility["status"] != "provenance-only" || result.Entries != 6 {
		t.Fatalf("result = %#v, recipe = %#v", result, document)
	}
}

func TestApplicationHistoryRecipeAcceptsLargeDetailedEntry(t *testing.T) {
	logData := []byte("Descriptor: " + strings.Repeat("x", 128<<10))
	result, err := ApplicationHistoryRecipe("photoshop", "sha256:"+strings.Repeat("b", 64), logData)
	if err != nil {
		t.Fatalf("ApplicationHistoryRecipe: %v", err)
	}
	if result.Entries != 1 {
		t.Fatalf("Entries = %d, want 1", result.Entries)
	}
}
