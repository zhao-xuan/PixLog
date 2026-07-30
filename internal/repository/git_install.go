package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	gitAttributesStart = "# BEGIN PixLog visual diff"
	gitAttributesEnd   = "# END PixLog visual diff"
	gitTrackingStart   = "# BEGIN PixLog custom tracking"
	gitTrackingEnd     = "# END PixLog custom tracking"
)

var gitAttributesPatterns = []string{
	"*.png filter=pixlog diff=pixlog merge=pixlog -text",
	"*.jpg filter=pixlog diff=pixlog merge=pixlog -text",
	"*.jpeg filter=pixlog diff=pixlog merge=pixlog -text",
	"*.gif filter=pixlog diff=pixlog merge=pixlog -text",
	"*.webp filter=pixlog diff=pixlog merge=pixlog -text",
	"*.avif filter=pixlog diff=pixlog merge=pixlog -text",
	"*.heic filter=pixlog diff=pixlog merge=pixlog -text",
	"*.tif filter=pixlog diff=pixlog merge=pixlog -text",
	"*.tiff filter=pixlog diff=pixlog merge=pixlog -text",
	"*.psd filter=pixlog diff=pixlog merge=pixlog -text",
}

type GitInstallResult struct {
	Root           string `json:"root"`
	AttributesPath string `json:"attributes_path"`
	ConfigPath     string `json:"config_path"`
	DriverCommand  string `json:"driver_command"`
	FilterCommand  string `json:"filter_command"`
	MergeCommand   string `json:"merge_command"`
	HookPath       string `json:"hook_path"`
}

func InstallGitIntegration(start, executable string) (GitInstallResult, error) {
	gitRepo, err := OpenGit(start)
	if err != nil {
		return GitInstallResult{}, err
	}
	standaloneConfig := filepath.Join(gitRepo.Root, ControlDirName, "config.json")
	if _, err := os.Stat(standaloneConfig); err == nil {
		return GitInstallResult{}, errors.New("cannot install Git companion mode over a standalone PixLog repository; migrate or remove the standalone repository first")
	} else if !errors.Is(err, os.ErrNotExist) {
		return GitInstallResult{}, fmt.Errorf("inspect standalone PixLog repository: %w", err)
	}
	if strings.TrimSpace(executable) == "" {
		executable = "pixlog"
	}
	driverCommand := shellQuote(executable) + " git-diff"
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "diff.pixlog.command", driverCommand); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog Git diff driver: %w", err)
	}
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "diff.pixlog.binary", "true"); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog binary diff: %w", err)
	}
	difftoolCommand := shellQuote(executable) + " compare \"$LOCAL\" \"$REMOTE\""
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "difftool.pixlog.cmd", difftoolCommand); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog Git difftool: %w", err)
	}
	filterCommand := shellQuote(executable) + " filter-process"
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "filter.pixlog.process", filterCommand); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog Git filter: %w", err)
	}
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "filter.pixlog.required", "true"); err != nil {
		return GitInstallResult{}, fmt.Errorf("require PixLog Git filter: %w", err)
	}
	mergeCommand := shellQuote(executable) + " git-merge-driver %O %A %B %L %P"
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "merge.pixlog.name", "PixLog visual merge"); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog merge name: %w", err)
	}
	if _, err := gitOutput(gitRepo.Root, "config", "--local", "merge.pixlog.driver", mergeCommand); err != nil {
		return GitInstallResult{}, fmt.Errorf("configure PixLog merge driver: %w", err)
	}

	attributesPath := filepath.Join(gitRepo.Root, ".gitattributes")
	existing, err := os.ReadFile(attributesPath)
	if err != nil && !os.IsNotExist(err) {
		return GitInstallResult{}, fmt.Errorf("read .gitattributes: %w", err)
	}
	updated := updateGitAttributes(string(existing))
	if string(existing) != updated {
		if err := writeFileAtomic(attributesPath, []byte(updated), 0o644); err != nil {
			return GitInstallResult{}, err
		}
	}
	configPath := filepath.Join(gitRepo.Root, ".pixlog.toml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		config := "version = 1\n\n[remote]\nendpoint = \"auto\"\n\n[checkout]\nhydrate = \"auto\"\n\n[provenance]\ncapture_embedded_metadata = true\nrequire_recipe = false\n"
		if err := writeFileAtomic(configPath, []byte(config), 0o644); err != nil {
			return GitInstallResult{}, err
		}
	} else if err != nil {
		return GitInstallResult{}, fmt.Errorf("inspect PixLog config: %w", err)
	}
	hook, err := InstallPrePushHook(gitRepo.Root, executable)
	if err != nil {
		return GitInstallResult{}, err
	}
	return GitInstallResult{
		Root:           gitRepo.Root,
		AttributesPath: attributesPath,
		ConfigPath:     configPath,
		DriverCommand:  driverCommand,
		FilterCommand:  filterCommand,
		MergeCommand:   mergeCommand,
		HookPath:       hook.HookPath,
	}, nil
}

func updateGitAttributes(existing string) string {
	block := gitAttributesStart + "\n" + strings.Join(gitAttributesPatterns, "\n") + "\n" + gitAttributesEnd
	start := strings.Index(existing, gitAttributesStart)
	end := strings.Index(existing, gitAttributesEnd)
	if start >= 0 && end >= start {
		end += len(gitAttributesEnd)
		updated := existing[:start] + block + existing[end:]
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		return updated
	}
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	if existing != "" {
		existing += "\n"
	}
	return existing + block + "\n"
}

func (g *GitRepository) TrackPatterns(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, errors.New("no tracking patterns specified")
	}
	attributesPath := filepath.Join(g.Root, ".gitattributes")
	existing, err := os.ReadFile(attributesPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	content := updateGitAttributes(string(existing))
	tracked := parseTrackedPatterns(content)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || strings.ContainsAny(pattern, "\r\n\t ") || strings.HasPrefix(pattern, "!") {
			return nil, fmt.Errorf("invalid Git tracking pattern %q", pattern)
		}
		tracked[pattern] = struct{}{}
	}
	ordered := make([]string, 0, len(tracked))
	for pattern := range tracked {
		ordered = append(ordered, pattern)
	}
	sort.Strings(ordered)
	lines := make([]string, 0, len(ordered))
	for _, pattern := range ordered {
		lines = append(lines, pattern+" filter=pixlog diff=pixlog merge=pixlog -text")
	}
	block := gitTrackingStart + "\n" + strings.Join(lines, "\n") + "\n" + gitTrackingEnd
	content = replaceMarkedBlock(content, gitTrackingStart, gitTrackingEnd, block)
	if err := writeFileAtomic(attributesPath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return ordered, nil
}

func parseTrackedPatterns(content string) map[string]struct{} {
	result := map[string]struct{}{}
	start := strings.Index(content, gitTrackingStart)
	end := strings.Index(content, gitTrackingEnd)
	if start < 0 || end <= start {
		return result
	}
	for _, line := range strings.Split(content[start+len(gitTrackingStart):end], "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			result[fields[0]] = struct{}{}
		}
	}
	return result
}

func replaceMarkedBlock(content, startMarker, endMarker, block string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 && end >= start {
		end += len(endMarker)
		updated := content[:start] + block + content[end:]
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		return updated
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + block + "\n"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
