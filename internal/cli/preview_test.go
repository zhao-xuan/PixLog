package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalPreviewerDisablesAutoModeForNonTerminal(t *testing.T) {
	t.Setenv("PIXLOG_CHAFA", filepath.Join(t.TempDir(), "missing-chafa"))
	previewer, enabled, err := terminalPreviewer(&bytes.Buffer{}, false, false, false)
	if err != nil {
		t.Fatalf("terminalPreviewer returned error: %v", err)
	}
	if enabled || previewer != "" {
		t.Fatalf("non-terminal preview = %q, enabled %v", previewer, enabled)
	}
}

func TestTerminalPreviewerRejectsForcedStructuredOutput(t *testing.T) {
	_, _, err := terminalPreviewer(&bytes.Buffer{}, true, false, true)
	if err == nil || !strings.Contains(err.Error(), "JSON or NDJSON") {
		t.Fatalf("forced structured preview error = %v", err)
	}
}

func TestTerminalPreviewerExplainsMissingChafaInForcedMode(t *testing.T) {
	t.Setenv("PIXLOG_CHAFA", filepath.Join(t.TempDir(), "missing-chafa"))
	_, _, err := terminalPreviewer(&bytes.Buffer{}, true, false, false)
	if err == nil || !strings.Contains(err.Error(), "brew install chafa") {
		t.Fatalf("missing Chafa error = %v", err)
	}
}
