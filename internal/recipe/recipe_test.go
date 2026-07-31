package recipe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeValidatesCaptureContract(t *testing.T) {
	document := `{
		"kind":"ai-generation",
		"capture":{"fidelity":"exact-request"},
		"reproducibility":{"status":"request-reproducible"},
		"vendor":{"raw_payload_oid":"sha256:` + strings.Repeat("a", 64) + `"}
	}`
	normalized, err := Normalize([]byte(document))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(string(normalized), `"schema":"pixlog.recipe/v1"`) {
		t.Fatalf("normalized recipe = %s", normalized)
	}
}

func TestNormalizeRejectsUnknownFidelity(t *testing.T) {
	_, err := Normalize([]byte(`{"kind":"ai-generation","capture":{"fidelity":"probably-exact"}}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported recipe capture fidelity") {
		t.Fatalf("Normalize error = %v", err)
	}
}

func TestEmbeddedRecipeDeclaresAdapterAndFidelity(t *testing.T) {
	data, found, err := FromEmbedded(map[string]string{"workflow": `{"1":{"class_type":"KSampler"}}`})
	if err != nil || !found {
		t.Fatalf("FromEmbedded found = %v, err = %v", found, err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	capture := document["capture"].(map[string]any)
	if capture["adapter"] != "comfyui-png" || capture["fidelity"] != string(FidelityEmbeddedMetadata) {
		t.Fatalf("capture = %#v", capture)
	}
}
