package repository

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const pixLogHookMarker = "# PixLog managed pre-push dispatcher"

type HookInstallResult struct {
	HookPath          string `json:"hook_path"`
	PreservedHookPath string `json:"preserved_hook_path,omitempty"`
}

func InstallPrePushHook(start, executable string) (HookInstallResult, error) {
	repo, err := OpenGit(start)
	if err != nil {
		return HookInstallResult{}, err
	}
	hooksDirectory, err := gitOutput(repo.Root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return HookInstallResult{}, fmt.Errorf("resolve Git hooks directory: %w", err)
	}
	if !filepath.IsAbs(hooksDirectory) {
		hooksDirectory = filepath.Join(repo.Root, hooksDirectory)
	}
	if err := os.MkdirAll(hooksDirectory, 0o755); err != nil {
		return HookInstallResult{}, err
	}
	hookPath := filepath.Join(hooksDirectory, "pre-push")
	preservedPath := hookPath + ".pixlog-user"
	if existing, err := os.ReadFile(hookPath); err == nil {
		if strings.Contains(string(existing), pixLogHookMarker) {
			return HookInstallResult{HookPath: hookPath, PreservedHookPath: existingFile(preservedPath)}, nil
		}
		if _, err := os.Stat(preservedPath); err == nil {
			return HookInstallResult{}, fmt.Errorf("cannot preserve existing pre-push hook: %s already exists", preservedPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return HookInstallResult{}, err
		}
		if err := os.Rename(hookPath, preservedPath); err != nil {
			return HookInstallResult{}, fmt.Errorf("preserve existing pre-push hook: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return HookInstallResult{}, err
	}
	if strings.TrimSpace(executable) == "" {
		executable = "pixlog"
	}
	script := "#!/bin/sh\n" + pixLogHookMarker + "\nexec " + shellQuote(executable) + " hook dispatch-pre-push \"$@\"\n"
	if err := writeFileAtomic(hookPath, []byte(script), 0o755); err != nil {
		return HookInstallResult{}, err
	}
	return HookInstallResult{HookPath: hookPath, PreservedHookPath: existingFile(preservedPath)}, nil
}

func RunPrePushDispatcher(start string, args []string, input []byte, stdout, stderr io.Writer) (MediaTransferResult, error) {
	if len(args) < 1 || len(args) > 2 {
		return MediaTransferResult{}, errors.New("pre-push hook requires <remote-name> [remote-location]")
	}
	repo, err := OpenGit(start)
	if err != nil {
		return MediaTransferResult{}, err
	}
	hooksDirectory, err := gitOutput(repo.Root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return MediaTransferResult{}, err
	}
	if !filepath.IsAbs(hooksDirectory) {
		hooksDirectory = filepath.Join(repo.Root, hooksDirectory)
	}
	preservedPath := filepath.Join(hooksDirectory, "pre-push.pixlog-user")
	if info, err := os.Stat(preservedPath); err == nil && info.Mode().IsRegular() {
		child := exec.Command(preservedPath, args...)
		child.Dir = repo.Root
		child.Stdin = bytes.NewReader(input)
		child.Stdout = stdout
		child.Stderr = stderr
		if err := child.Run(); err != nil {
			return MediaTransferResult{}, fmt.Errorf("preserved pre-push hook failed: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return MediaTransferResult{}, err
	}
	updates, err := ParseGitRefUpdates(input)
	if err != nil {
		return MediaTransferResult{}, err
	}
	return repo.PushMediaObjects(args[0], updates)
}

func existingFile(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
