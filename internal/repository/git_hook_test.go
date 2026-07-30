package repository

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPrePushHookPreservesExistingHook(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	runGitTest(t, root, "init", "--quiet")
	hooksDirectory := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooksDirectory, 0o755); err != nil {
		t.Fatalf("create hooks directory: %v", err)
	}
	hookPath := filepath.Join(hooksDirectory, "pre-push")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho user-hook\n"), 0o755); err != nil {
		t.Fatalf("write user hook: %v", err)
	}
	result, err := InstallPrePushHook(root, "/opt/Pix Log/pixlog")
	if err != nil {
		t.Fatalf("InstallPrePushHook: %v", err)
	}
	if result.PreservedHookPath == "" {
		t.Fatalf("install result = %#v", result)
	}
	managed, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read managed hook: %v", err)
	}
	if !strings.Contains(string(managed), pixLogHookMarker) || !strings.Contains(string(managed), "dispatch-pre-push") {
		t.Fatalf("managed hook:\n%s", managed)
	}
	var stdout, stderr bytes.Buffer
	transfer, err := RunPrePushDispatcher(root, []string{"origin", "unused"}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunPrePushDispatcher: %v", err)
	}
	if !strings.Contains(stdout.String(), "user-hook") || len(transfer.Objects) != 0 {
		t.Fatalf("stdout = %q, transfer = %#v", stdout.String(), transfer)
	}
	if _, err := InstallPrePushHook(root, "/opt/Pix Log/pixlog"); err != nil {
		t.Fatalf("InstallPrePushHook again: %v", err)
	}
}
