package c2pa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManifestFromRecipeOmitsSensitiveParameters(t *testing.T) {
	recipe := []byte(`{
		"schema":"pixlog.recipe/v1",
		"kind":"ai-generation",
		"tool":{"name":"ComfyUI"},
		"parameters":{"prompt":"private prompt","seed":42},
		"parents":[{"asset":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]
	}`)
	manifest, err := ManifestFromRecipe("assets/hero.png", "image/png", "test", recipe)
	if err != nil {
		t.Fatalf("ManifestFromRecipe: %v", err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "private prompt") || !strings.Contains(string(data), "c2pa.created") {
		t.Fatalf("manifest = %s", data)
	}
}

func TestToolVerifyUsesDetailedJSONOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	directory := t.TempDir()
	executable := filepath.Join(directory, "c2patool")
	script := "#!/bin/sh\nprintf '%s' '{\"validation_status\":[]}'\n"
	if err := os.WriteFile(executable, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake c2patool: %v", err)
	}
	result, err := (Tool{Executable: executable}).Verify("asset.png")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if string(result) != `{"validation_status":[]}` {
		t.Fatalf("result = %s", result)
	}
}
