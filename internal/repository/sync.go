package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type SyncResult struct {
	Remote        string `json:"remote"`
	Branch        string `json:"branch"`
	CopiedObjects int    `json:"copied_objects"`
	LocalHead     string `json:"local_head,omitempty"`
	RemoteHead    string `json:"remote_head,omitempty"`
}

type VerifyResult struct {
	Objects int      `json:"objects"`
	Corrupt []string `json:"corrupt"`
}

func (r *Repository) AddRemote(name, remoteURL string) error {
	if !remoteNamePattern.MatchString(name) {
		return fmt.Errorf("invalid remote name %q", name)
	}
	if strings.TrimSpace(remoteURL) == "" {
		return errors.New("remote URL cannot be empty")
	}
	if _, exists := r.Config.Remotes[name]; exists {
		return fmt.Errorf("remote %q already exists", name)
	}
	r.Config.Remotes[name] = Remote{URL: remoteURL}
	return r.SaveConfig()
}

func (r *Repository) RemoveRemote(name string) error {
	if _, exists := r.Config.Remotes[name]; !exists {
		return fmt.Errorf("unknown remote %q", name)
	}
	delete(r.Config.Remotes, name)
	return r.SaveConfig()
}

func (r *Repository) Push(name string) (SyncResult, error) {
	remote, err := r.openNamedRemote(name)
	if err != nil {
		return SyncResult{}, err
	}
	if !remote.Config.Bare {
		return SyncResult{}, errors.New("push currently requires a bare PixLog remote")
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return SyncResult{}, err
	}
	localHead, err := r.HeadOID()
	if err != nil {
		return SyncResult{}, err
	}
	if localHead == "" {
		return SyncResult{}, errors.New("nothing to push: repository has no commits")
	}
	remoteHead, err := remote.branchOID(branch)
	if err != nil {
		return SyncResult{}, err
	}
	if remoteHead != "" {
		isAncestor, err := r.isAncestor(remoteHead, localHead)
		if err != nil {
			return SyncResult{}, fmt.Errorf("check remote history: %w", err)
		}
		if !isAncestor {
			return SyncResult{}, errors.New("push rejected: remote contains history not present locally; pull first")
		}
	}
	copied, err := remote.copyObjectsFrom(r)
	if err != nil {
		return SyncResult{}, err
	}
	if err := remote.setBranchOID(branch, localHead); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Remote: name, Branch: branch, CopiedObjects: copied, LocalHead: localHead, RemoteHead: localHead}, nil
}

func (r *Repository) Fetch(name string) (SyncResult, error) {
	remote, err := r.openNamedRemote(name)
	if err != nil {
		return SyncResult{}, err
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return SyncResult{}, err
	}
	remoteHead, err := remote.branchOID(branch)
	if err != nil {
		return SyncResult{}, err
	}
	if remoteHead == "" {
		return SyncResult{}, fmt.Errorf("remote %q has no branch %q", name, branch)
	}
	copied, err := r.copyObjectsFrom(remote)
	if err != nil {
		return SyncResult{}, err
	}
	trackingRef := filepath.Join(r.Control, "refs", "remotes", name, filepath.FromSlash(branch))
	if err := writeFileAtomic(trackingRef, []byte(remoteHead+"\n"), 0o644); err != nil {
		return SyncResult{}, err
	}
	localHead, err := r.HeadOID()
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Remote: name, Branch: branch, CopiedObjects: copied, LocalHead: localHead, RemoteHead: remoteHead}, nil
}

func (r *Repository) Pull(name string) (SyncResult, error) {
	result, err := r.Fetch(name)
	if err != nil {
		return SyncResult{}, err
	}
	if result.LocalHead == result.RemoteHead {
		return result, nil
	}
	status, err := r.Status()
	if err != nil {
		return SyncResult{}, err
	}
	if len(status.Staged) > 0 || len(status.Unstaged) > 0 {
		return SyncResult{}, errors.New("cannot pull with staged or unstaged changes")
	}
	if result.LocalHead != "" {
		isAncestor, err := r.isAncestor(result.LocalHead, result.RemoteHead)
		if err != nil {
			return SyncResult{}, err
		}
		if !isAncestor {
			return SyncResult{}, errors.New("pull requires a fast-forward; histories have diverged")
		}
	}
	remoteCommit, err := r.ReadCommit(result.RemoteHead)
	if err != nil {
		return SyncResult{}, err
	}
	index, err := r.ReadIndex()
	if err != nil {
		return SyncResult{}, err
	}
	if err := r.checkoutTree(index.Entries, remoteCommit.Tree); err != nil {
		return SyncResult{}, err
	}
	if err := r.WriteIndex(Index{Schema: SchemaVersion, Entries: cloneTree(remoteCommit.Tree)}); err != nil {
		return SyncResult{}, err
	}
	if err := r.SetHeadOID(result.RemoteHead); err != nil {
		return SyncResult{}, err
	}
	result.LocalHead = result.RemoteHead
	return result, nil
}

func Clone(remoteURL, target string) (*Repository, SyncResult, error) {
	remotePath, err := resolveRemoteURL("", remoteURL)
	if err != nil {
		return nil, SyncResult{}, err
	}
	remote, err := OpenRemote(remotePath)
	if err != nil {
		return nil, SyncResult{}, err
	}
	if target == "" {
		base := filepath.Base(filepath.Clean(remotePath))
		target = strings.TrimSuffix(base, filepath.Ext(base))
		if target == "" || target == "." {
			target = "pixlog-clone"
		}
	}
	if entries, readErr := os.ReadDir(target); readErr == nil && len(entries) > 0 {
		return nil, SyncResult{}, fmt.Errorf("clone target %s is not empty", target)
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, SyncResult{}, readErr
	}
	local, err := Init(target, false)
	if err != nil {
		return nil, SyncResult{}, err
	}
	local.Config.Remotes["origin"] = Remote{URL: remoteURL}
	if err := local.SaveConfig(); err != nil {
		return nil, SyncResult{}, err
	}
	branch := remote.Config.DefaultBranch
	remoteHead, err := remote.branchOID(branch)
	if err != nil {
		return nil, SyncResult{}, err
	}
	copied, err := local.copyObjectsFrom(remote)
	if err != nil {
		return nil, SyncResult{}, err
	}
	result := SyncResult{Remote: "origin", Branch: branch, CopiedObjects: copied, RemoteHead: remoteHead}
	if remoteHead == "" {
		return local, result, nil
	}
	commit, err := local.ReadCommit(remoteHead)
	if err != nil {
		return nil, SyncResult{}, err
	}
	if err := local.checkoutTree(map[string]Entry{}, commit.Tree); err != nil {
		return nil, SyncResult{}, err
	}
	if err := local.WriteIndex(Index{Schema: SchemaVersion, Entries: cloneTree(commit.Tree)}); err != nil {
		return nil, SyncResult{}, err
	}
	if err := local.SetHeadOID(remoteHead); err != nil {
		return nil, SyncResult{}, err
	}
	trackingRef := filepath.Join(local.Control, "refs", "remotes", "origin", filepath.FromSlash(branch))
	if err := writeFileAtomic(trackingRef, []byte(remoteHead+"\n"), 0o644); err != nil {
		return nil, SyncResult{}, err
	}
	result.LocalHead = remoteHead
	return local, result, nil
}

func (r *Repository) VerifyObjects() (VerifyResult, error) {
	result := VerifyResult{Corrupt: []string{}}
	root := filepath.Join(r.Control, "objects", "sha256")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digest := strings.ReplaceAll(relative, string(filepath.Separator), "")
		if len(digest) != sha256.Size*2 || verifyFileDigest(path, digest) != nil {
			result.Corrupt = append(result.Corrupt, relative)
		}
		result.Objects++
		return nil
	})
	return result, err
}

func (r *Repository) openNamedRemote(name string) (*Repository, error) {
	if name == "" {
		name = "origin"
	}
	remote, exists := r.Config.Remotes[name]
	if !exists {
		return nil, fmt.Errorf("unknown remote %q", name)
	}
	path, err := resolveRemoteURL(r.Root, remote.URL)
	if err != nil {
		return nil, err
	}
	return OpenRemote(path)
}

func resolveRemoteURL(root, remoteURL string) (string, error) {
	path := remoteURL
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	} else if strings.Contains(path, "://") {
		return "", fmt.Errorf("remote scheme in %q is not available in this build; use a local path or file:// URL", remoteURL)
	}
	if !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), nil
}

func (r *Repository) copyObjectsFrom(source *Repository) (int, error) {
	sourceRoot := filepath.Join(source.Control, "objects", "sha256")
	copied := 0
	err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		digest := strings.ReplaceAll(relative, string(filepath.Separator), "")
		if len(digest) != sha256.Size*2 {
			return fmt.Errorf("invalid source object path %s", relative)
		}
		destination := filepath.Join(r.Control, "objects", "sha256", relative)
		if _, err := os.Stat(destination); err == nil {
			return verifyFileDigest(destination, digest)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyVerifiedFile(path, destination, digest); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func copyVerifiedFile(source, destination, expectedDigest string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".pixlog-sync-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedDigest {
		return fmt.Errorf("source object hash mismatch: expected %s, got %s", expectedDigest, actual)
	}
	if err := os.Chmod(temporaryPath, 0o444); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func verifyFileDigest(path, expected string) error {
	oid, err := HashFile(path)
	if err != nil {
		return err
	}
	actual := strings.TrimPrefix(oid, "sha256:")
	if actual != expected {
		return fmt.Errorf("object %s has hash %s, expected %s", path, actual, expected)
	}
	return nil
}

func (r *Repository) branchOID(branch string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.Control, "refs", "heads", filepath.FromSlash(branch)))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *Repository) setBranchOID(branch, oid string) error {
	if _, err := parseOID(oid); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.Control, "refs", "heads", filepath.FromSlash(branch)), []byte(oid+"\n"), 0o644)
}

func (r *Repository) isAncestor(ancestor, descendant string) (bool, error) {
	for descendant != "" {
		if descendant == ancestor {
			return true, nil
		}
		commit, err := r.ReadCommit(descendant)
		if err != nil {
			return false, err
		}
		descendant = commit.Parent
	}
	return false, nil
}

func (r *Repository) checkoutTree(oldTree, newTree map[string]Entry) error {
	for path, entry := range newTree {
		relative, err := safeTreePath(path)
		if err != nil {
			return err
		}
		destination := filepath.Join(r.Root, relative)
		if _, wasTracked := oldTree[path]; !wasTracked {
			if _, err := os.Stat(destination); err == nil {
				return fmt.Errorf("untracked file %s would be overwritten", path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if _, err := r.Load(entry.ContentOID); err != nil {
			return fmt.Errorf("verify checkout object for %s: %w", path, err)
		}
	}
	for path := range oldTree {
		if _, remains := newTree[path]; remains {
			continue
		}
		relative, err := safeTreePath(path)
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(r.Root, relative)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for path, entry := range newTree {
		oldEntry, existed := oldTree[path]
		if existed && oldEntry.ContentOID == entry.ContentOID {
			continue
		}
		data, err := r.Load(entry.ContentOID)
		if err != nil {
			return err
		}
		relative, _ := safeTreePath(path)
		if err := writeFileAtomic(filepath.Join(r.Root, relative), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func safeTreePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("unsafe tree path %q", path)
	}
	relative := filepath.Clean(filepath.FromSlash(path))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ControlDirName || strings.HasPrefix(relative, ControlDirName+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe tree path %q", path)
	}
	return relative, nil
}
