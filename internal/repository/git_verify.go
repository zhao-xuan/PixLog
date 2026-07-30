package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DoctorCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type DoctorResult struct {
	Root   string        `json:"root"`
	Passed bool          `json:"passed"`
	Checks []DoctorCheck `json:"checks"`
}

func (g *GitRepository) VerifyObjects() (VerifyResult, error) {
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return VerifyResult{}, err
	}
	result := VerifyResult{Corrupt: []string{}}
	objectsRoot := filepath.Join(store.Root, "objects", "sha256")
	err = filepath.WalkDir(objectsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(objectsRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		result.Objects++
		if len(parts) != 2 || len(parts[0]) != 2 {
			result.Corrupt = append(result.Corrupt, filepath.ToSlash(relative))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hashBytes(data) != "sha256:"+parts[0]+parts[1] {
			result.Corrupt = append(result.Corrupt, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return VerifyResult{}, err
	}
	pointers, err := g.trackedPointers(nil)
	if err != nil {
		return VerifyResult{}, err
	}
	for path, pointerData := range pointers {
		pointer, _, _ := ParsePixLogPointer(pointerData)
		for _, oid := range []string{pointer.OID, pointer.ManifestOID, pointer.RecipeOID} {
			if oid == "" {
				continue
			}
			if _, err := store.Get(oid); err != nil {
				result.Corrupt = append(result.Corrupt, path+" -> "+oid)
			}
		}
	}
	sort.Strings(result.Corrupt)
	return result, nil
}

func (g *GitRepository) Doctor() (DoctorResult, error) {
	result := DoctorResult{Root: g.Root, Passed: true, Checks: []DoctorCheck{}}
	add := func(name string, passed bool, detail string) {
		result.Checks = append(result.Checks, DoctorCheck{Name: name, Passed: passed, Detail: detail})
		if !passed {
			result.Passed = false
		}
	}
	filter, _ := gitOptionalOutput(g.Root, []int{1}, "config", "--get", "filter.pixlog.process")
	required, _ := gitOptionalOutput(g.Root, []int{1}, "config", "--get", "filter.pixlog.required")
	add("filter", filter != "" && required == "true", filter)
	diff, _ := gitOptionalOutput(g.Root, []int{1}, "config", "--get", "diff.pixlog.command")
	add("diff", diff != "", diff)
	merge, _ := gitOptionalOutput(g.Root, []int{1}, "config", "--get", "merge.pixlog.driver")
	add("merge", merge != "", merge)
	attributes, err := os.ReadFile(filepath.Join(g.Root, ".gitattributes"))
	add("attributes", err == nil && strings.Contains(string(attributes), gitAttributesStart) && strings.Contains(string(attributes), "filter=pixlog"), filepath.Join(g.Root, ".gitattributes"))
	hookPath, hookErr := gitOutput(g.Root, "rev-parse", "--git-path", "hooks/pre-push")
	if hookErr == nil && !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(g.Root, hookPath)
	}
	hookData, hookReadErr := os.ReadFile(hookPath)
	add("pre-push", hookErr == nil && hookReadErr == nil && strings.Contains(string(hookData), pixLogHookMarker), hookPath)
	configPath := filepath.Join(g.Root, ".pixlog.toml")
	_, configErr := os.Stat(configPath)
	add("config", configErr == nil, configPath)
	verification, verifyErr := g.VerifyObjects()
	if verifyErr != nil {
		return DoctorResult{}, verifyErr
	}
	add("objects", len(verification.Corrupt) == 0, fmt.Sprintf("%d object(s), %d corrupt or missing", verification.Objects, len(verification.Corrupt)))
	return result, nil
}
