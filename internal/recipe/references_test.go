package recipe

import (
	"strings"
	"testing"
)

func TestReferencesFindsTypedRecipeDependencies(t *testing.T) {
	parentOID := "sha256:" + strings.Repeat("a", 64)
	modelOID := "sha256:" + strings.Repeat("b", 64)
	rawOID := "sha256:" + strings.Repeat("c", 64)
	data := []byte(`{
		"kind":"ai-generation",
		"parents":[{"asset":"` + parentOID + `","role":"reference-image"}],
		"model":{"digest":"` + modelOID + `"},
		"vendor":{"raw_payload_oid":"` + rawOID + `"}
	}`)
	references, err := References(data)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(references) != 3 {
		t.Fatalf("references = %#v", references)
	}
	byOID := map[string]Reference{}
	for _, reference := range references {
		byOID[reference.OID] = reference
	}
	if byOID[parentOID].Role != "reference-image" || byOID[modelOID].Kind != "model" || byOID[rawOID].Kind != "vendor-payload" {
		t.Fatalf("references by OID = %#v", byOID)
	}
}
