package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/zhao-xuan/PixLog/internal/imaging"
	"github.com/zhao-xuan/PixLog/internal/repository"
)

const defaultPreviewSize = "30x14"

func terminalPreviewer(stdout io.Writer, force, disabled, structured bool) (string, bool, error) {
	if force && disabled {
		return "", false, errors.New("--preview and --no-preview cannot be combined")
	}
	if structured {
		if force {
			return "", false, errors.New("--preview cannot be combined with JSON or NDJSON output")
		}
		return "", false, nil
	}
	if disabled || (!force && !isTerminalWriter(stdout)) {
		return "", false, nil
	}

	executable := os.Getenv("PIXLOG_CHAFA")
	if executable == "" {
		executable = "chafa"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		if force {
			return "", false, fmt.Errorf("terminal image preview requires Chafa; install it with 'brew install chafa' or your system package manager")
		}
		return "", false, nil
	}
	return path, true, nil
}

func renderDiffPreviews(previewer string, stdout, stderr io.Writer, report repository.DiffReport) error {
	for _, asset := range report.Assets {
		if asset.Preview == nil {
			continue
		}
		temporary, err := os.MkdirTemp("", "pixlog-preview-*")
		if err != nil {
			return fmt.Errorf("create preview directory: %w", err)
		}
		defer os.RemoveAll(temporary)

		paths := make([]string, 0, 3)
		extension := previewExtension(asset.Path)
		if len(asset.Preview.Before) > 0 {
			path := filepath.Join(temporary, "before"+extension)
			if err := os.WriteFile(path, asset.Preview.Before, 0o600); err != nil {
				return fmt.Errorf("write before preview: %w", err)
			}
			paths = append(paths, path)
		}
		if len(asset.Preview.After) > 0 {
			path := filepath.Join(temporary, "after"+extension)
			if err := os.WriteFile(path, asset.Preview.After, 0o600); err != nil {
				return fmt.Errorf("write after preview: %w", err)
			}
			paths = append(paths, path)
		}
		if asset.Visual != nil {
			path := filepath.Join(temporary, "heatmap.png")
			file, err := os.Create(path)
			if err != nil {
				return fmt.Errorf("create heatmap preview: %w", err)
			}
			writeErr := imaging.WriteHeatmap(file, *asset.Visual)
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return fmt.Errorf("close heatmap preview: %w", closeErr)
			}
			paths = append(paths, path)
		}
		if len(paths) == 0 {
			continue
		}

		fmt.Fprintf(stdout, "\n  preview                  %s\n", asset.Path)
		arguments := []string{
			"--animate=off",
			"--probe=auto",
			"--grid", fmt.Sprintf("%dx1", len(paths)),
			"--label=on",
			"--margin-bottom=0",
			"--size", previewSize(),
		}
		if format := strings.TrimSpace(os.Getenv("PIXLOG_CHAFA_FORMAT")); format != "" {
			arguments = append(arguments, "--format", format)
		}
		arguments = append(arguments, paths...)
		command := exec.Command(previewer, arguments...)
		command.Stdin = os.Stdin
		command.Stdout = stdout
		command.Stderr = stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("render terminal image preview: %w", err)
		}
	}
	return nil
}

func isTerminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
}

func previewSize() string {
	if size := strings.TrimSpace(os.Getenv("PIXLOG_PREVIEW_SIZE")); size != "" {
		return size
	}
	return defaultPreviewSize
}

func previewExtension(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	if len(extension) > 1 && len(extension) <= 8 {
		return extension
	}
	return ".img"
}
