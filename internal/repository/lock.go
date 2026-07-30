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

type Lock struct {
	Path      string    `json:"path"`
	Owner     string    `json:"owner"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	Remote    string    `json:"remote,omitempty"`
}

func (r *Repository) AcquireLock(assetPath, owner, remoteName string) (Lock, error) {
	path, err := r.normalizedAssetPath(assetPath)
	if err != nil {
		return Lock{}, err
	}
	if strings.TrimSpace(owner) == "" {
		owner = DefaultAuthor()
	}
	target := r
	if remoteName != "" {
		target, err = r.openNamedRemote(remoteName)
		if err != nil {
			return Lock{}, err
		}
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
	lockPath := target.lockPath(path)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return Lock{}, err
	}
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := target.readLock(lockPath)
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

func (r *Repository) ReleaseLock(assetPath, owner, remoteName string, force bool) (Lock, error) {
	path, err := r.normalizedAssetPath(assetPath)
	if err != nil {
		return Lock{}, err
	}
	target := r
	if remoteName != "" {
		target, err = r.openNamedRemote(remoteName)
		if err != nil {
			return Lock{}, err
		}
	}
	lockPath := target.lockPath(path)
	lock, err := target.readLock(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, fmt.Errorf("asset %s is not locked", path)
	}
	if err != nil {
		return Lock{}, err
	}
	if strings.TrimSpace(owner) == "" {
		owner = DefaultAuthor()
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

func (r *Repository) ListLocks(remoteName string) ([]Lock, error) {
	target := r
	var err error
	if remoteName != "" {
		target, err = r.openNamedRemote(remoteName)
		if err != nil {
			return nil, err
		}
	}
	root := filepath.Join(target.Control, "locks")
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
		lock, err := target.readLock(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		lock.Remote = remoteName
		locks = append(locks, lock)
	}
	sort.Slice(locks, func(left, right int) bool { return locks[left].Path < locks[right].Path })
	return locks, nil
}

func (r *Repository) normalizedAssetPath(assetPath string) (string, error) {
	absolute, err := filepath.Abs(assetPath)
	if err != nil {
		return "", err
	}
	path, err := r.relativePath(absolute)
	if err != nil {
		return "", err
	}
	index, err := r.ReadIndex()
	if err != nil {
		return "", err
	}
	if _, tracked := index.Entries[path]; !tracked {
		return "", fmt.Errorf("asset %s is not tracked", path)
	}
	return path, nil
}

func (r *Repository) lockPath(path string) string {
	digest := sha256.Sum256([]byte(path))
	return filepath.Join(r.Control, "locks", hex.EncodeToString(digest[:])+".json")
}

func (r *Repository) readLock(path string) (Lock, error) {
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
