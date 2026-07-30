package repository

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type GitContext struct {
	Provider      string `json:"provider"`
	HeadOID       string `json:"head_oid,omitempty"`
	Branch        string `json:"branch,omitempty"`
	IndexTreeOID  string `json:"index_tree_oid,omitempty"`
	WorktreeDirty bool   `json:"worktree_dirty"`
}

func DiscoverGitContext(start string) (*GitContext, error) {
	root, err := gitOutput(start, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || gitExitCode(err) == 128 {
			return nil, nil
		}
		return nil, fmt.Errorf("discover Git repository: %w", err)
	}

	context := &GitContext{Provider: "git"}
	context.HeadOID, err = gitOptionalOutput(root, []int{128}, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve Git HEAD: %w", err)
	}
	context.Branch, err = gitOptionalOutput(root, []int{1}, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve Git branch: %w", err)
	}
	context.IndexTreeOID, err = gitOutput(root, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("resolve Git index tree: %w", err)
	}
	status, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, fmt.Errorf("inspect Git worktree: %w", err)
	}
	context.WorktreeDirty = status != ""
	return context, nil
}

func gitOptionalOutput(directory string, allowedExitCodes []int, args ...string) (string, error) {
	output, err := gitOutput(directory, args...)
	if err == nil {
		return output, nil
	}
	code := gitExitCode(err)
	for _, allowed := range allowedExitCodes {
		if code == allowed {
			return "", nil
		}
	}
	return "", err
}

func gitOutput(directory string, args ...string) (string, error) {
	if directory == "" {
		directory = "."
	}
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			message := strings.TrimSpace(string(exitError.Stderr))
			if message != "" {
				return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), message, err)
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitBytes(directory string, args ...string) ([]byte, error) {
	if directory == "" {
		directory = "."
	}
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			message := strings.TrimSpace(string(exitError.Stderr))
			if message != "" {
				return nil, fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), message, err)
			}
		}
		return nil, err
	}
	return output, nil
}

func gitExitCode(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}
