package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCaptureGuideProvidesPlatformSpecificSteps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"capture", "guide", "--json", "forge"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("capture guide exit %d: %s", exitCode, stderr.String())
	}
	var guide map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &guide); err != nil {
		t.Fatalf("decode guide: %v", err)
	}
	if guide["platform"] != "automatic1111" || guide["capture_fidelity"] != "exact-request" {
		t.Fatalf("guide = %#v", guide)
	}
}

func TestCaptureGuideRejectsUnknownPlatform(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"capture", "guide", "unknown-platform"}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "unknown capture platform") {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
}
