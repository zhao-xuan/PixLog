package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Branch struct {
	Name    string `json:"name"`
	OID     string `json:"oid,omitempty"`
	Current bool   `json:"current"`
}

type Tag struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

func (r *Repository) ListBranches() ([]Branch, error) {
	current, err := r.CurrentBranch()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(r.Control, "refs", "heads")
	branches := []Branch{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		branches = append(branches, Branch{Name: name, OID: strings.TrimSpace(string(data)), Current: name == current})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		branches = append(branches, Branch{Name: current, Current: true})
	}
	sort.Slice(branches, func(left, right int) bool { return branches[left].Name < branches[right].Name })
	return branches, nil
}

func (r *Repository) CreateBranch(name, revision string) (Branch, error) {
	if err := validateRefName(name); err != nil {
		return Branch{}, err
	}
	path := filepath.Join(r.Control, "refs", "heads", filepath.FromSlash(name))
	if _, err := os.Stat(path); err == nil {
		return Branch{}, fmt.Errorf("branch %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Branch{}, err
	}
	oid, _, err := r.ResolveCommit(revision)
	if err != nil {
		return Branch{}, err
	}
	if err := writeFileAtomic(path, []byte(oid+"\n"), 0o644); err != nil {
		return Branch{}, err
	}
	return Branch{Name: name, OID: oid}, nil
}

func (r *Repository) DeleteBranch(name string) error {
	current, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	if name == current {
		return errors.New("cannot delete the current branch")
	}
	if err := validateRefName(name); err != nil {
		return err
	}
	path := filepath.Join(r.Control, "refs", "heads", filepath.FromSlash(name))
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unknown branch %q", name)
	} else if err != nil {
		return err
	}
	return nil
}

func (r *Repository) SwitchBranch(name string) error {
	if err := validateRefName(name); err != nil {
		return err
	}
	targetOID, err := r.branchOID(name)
	if err != nil {
		return err
	}
	if targetOID == "" {
		return fmt.Errorf("unknown branch %q", name)
	}
	current, err := r.CurrentBranch()
	if err != nil {
		return err
	}
	if current == name {
		return nil
	}
	status, err := r.Status()
	if err != nil {
		return err
	}
	if len(status.Staged) > 0 || len(status.Unstaged) > 0 {
		return errors.New("cannot switch branches with staged or unstaged changes")
	}
	index, err := r.ReadIndex()
	if err != nil {
		return err
	}
	target, err := r.ReadCommit(targetOID)
	if err != nil {
		return err
	}
	if err := r.checkoutTree(index.Entries, target.Tree); err != nil {
		return err
	}
	if err := r.WriteIndex(Index{Schema: SchemaVersion, Entries: cloneTree(target.Tree)}); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.Control, "HEAD"), []byte("ref: refs/heads/"+name+"\n"), 0o644)
}

func (r *Repository) CreateTag(name, revision string) (Tag, error) {
	if err := validateRefName(name); err != nil {
		return Tag{}, err
	}
	path := filepath.Join(r.Control, "refs", "tags", filepath.FromSlash(name))
	if _, err := os.Stat(path); err == nil {
		return Tag{}, fmt.Errorf("tag %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Tag{}, err
	}
	oid, _, err := r.ResolveCommit(revision)
	if err != nil {
		return Tag{}, err
	}
	if err := writeFileAtomic(path, []byte(oid+"\n"), 0o644); err != nil {
		return Tag{}, err
	}
	return Tag{Name: name, OID: oid}, nil
}

func (r *Repository) ListTags() ([]Tag, error) {
	root := filepath.Join(r.Control, "refs", "tags")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return []Tag{}, nil
	}
	tags := []Tag{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tags = append(tags, Tag{Name: filepath.ToSlash(name), OID: strings.TrimSpace(string(data))})
		return nil
	})
	sort.Slice(tags, func(left, right int) bool { return tags[left].Name < tags[right].Name })
	return tags, err
}

func validateRefName(name string) error {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.ContainsAny(name, " ~^:?*[\\") {
		return fmt.Errorf("invalid ref name %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid ref name %q", name)
		}
	}
	return nil
}
