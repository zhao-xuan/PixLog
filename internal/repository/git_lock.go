package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (g *GitRepository) AcquireLock(assetPath, owner, remoteName string) (Lock, error) {
	path, err := g.normalizedTrackedAssetPath(assetPath)
	if err != nil {
		return Lock{}, err
	}
	if strings.TrimSpace(owner) == "" {
		owner = gitLockOwner(g.Root)
	}
	root, err := g.gitLockRoot(remoteName)
	if err != nil {
		return Lock{}, err
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Lock{}, fmt.Errorf("create lock token: %w", err)
	}
	lock := Lock{
		Path:      path,
		Owner:     owner,
		Token:     hex.EncodeToString(tokenBytes),
		CreatedAt: time.Now().UTC(),
		Remote:    remoteName,
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return Lock{}, err
	}
	data = append(data, '\n')
	lockPath := gitLockPath(root, path)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return Lock{}, err
	}
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := readGitLock(lockPath)
		if readErr != nil {
			return Lock{}, fmt.Errorf("asset %s is already locked", path)
		}
		return Lock{}, fmt.Errorf("asset %s is locked by %s since %s", path, existing.Owner, existing.CreatedAt.Format(time.RFC3339))
	}
	if err != nil {
		return Lock{}, fmt.Errorf("create lock: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(lockPath)
		return Lock{}, fmt.Errorf("write lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(lockPath)
		return Lock{}, fmt.Errorf("sync lock: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(lockPath)
		return Lock{}, fmt.Errorf("close lock: %w", err)
	}
	return lock, nil
}

func (g *GitRepository) ReleaseLock(assetPath, owner, remoteName string, force bool) (Lock, error) {
	path, err := g.normalizedTrackedAssetPath(assetPath)
	if err != nil {
		return Lock{}, err
	}
	root, err := g.gitLockRoot(remoteName)
	if err != nil {
		return Lock{}, err
	}
	lockPath := gitLockPath(root, path)
	lock, err := readGitLock(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, fmt.Errorf("asset %s is not locked", path)
	}
	if err != nil {
		return Lock{}, err
	}
	if strings.TrimSpace(owner) == "" {
		owner = gitLockOwner(g.Root)
	}
	if !force && lock.Owner != owner {
		return Lock{}, fmt.Errorf("asset %s is locked by %s, not %s", path, lock.Owner, owner)
	}
	if err := os.Remove(lockPath); err != nil {
		return Lock{}, fmt.Errorf("remove lock: %w", err)
	}
	lock.Remote = remoteName
	return lock, nil
}

func (g *GitRepository) ListLocks(remoteName string) ([]Lock, error) {
	root, err := g.gitLockRoot(remoteName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	locks := make([]Lock, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		lock, err := readGitLock(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		lock.Remote = remoteName
		locks = append(locks, lock)
	}
	sort.Slice(locks, func(left, right int) bool { return locks[left].Path < locks[right].Path })
	return locks, nil
}

func (g *GitRepository) normalizedTrackedAssetPath(assetPath string) (string, error) {
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return "", err
	}
	path, err := g.relativePath(absolute)
	if err != nil {
		return "", err
	}
	data, err := gitBytes(g.Root, "show", ":"+path)
	if err != nil {
		return "", fmt.Errorf("asset %s is not tracked", path)
	}
	if _, found, err := ParsePixLogPointer(data); err != nil {
		return "", err
	} else if !found {
		return "", fmt.Errorf("asset %s is not tracked by PixLog", path)
	}
	return path, nil
}

func (g *GitRepository) gitLockRoot(remoteName string) (string, error) {
	if remoteName == "" {
		store, err := OpenGitMediaStore(g.Root)
		if err != nil {
			return "", err
		}
		return filepath.Join(store.Root, "locks"), nil
	}
	endpoint, err := g.MediaEndpoint(remoteName)
	if err != nil {
		return "", err
	}
	if isHTTPURL(endpoint) {
		return "", errors.New("HTTP PixLog locks require a lock server; file endpoints are supported by this client")
	}
	root, err := fileEndpointPath(endpoint)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "locks"), nil
}

func gitLockPath(root, path string) string {
	digest := sha256.Sum256([]byte(path))
	return filepath.Join(root, hex.EncodeToString(digest[:])+".json")
}

func readGitLock(path string) (Lock, error) {
	var lock Lock
	data, err := os.ReadFile(path)
	if err != nil {
		return Lock{}, err
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return Lock{}, fmt.Errorf("decode lock %s: %w", path, err)
	}
	return lock, nil
}

func gitLockOwner(root string) string {
	if owner, err := gitOutput(root, "config", "user.name"); err == nil && strings.TrimSpace(owner) != "" {
		return owner
	}
	return DefaultAuthor()
}
