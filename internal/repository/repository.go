package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotRepository = errors.New("not a PixLog repository (or any parent directory)")

type Repository struct {
	Root    string
	Control string
	Config  Config
}

func Init(path string, bare bool) (*Repository, error) {
	if path == "" {
		path = "."
	}

	root, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	control := filepath.Join(root, ControlDirName)
	if bare {
		control = root
	}

	if _, err := os.Stat(filepath.Join(control, "config.json")); err == nil {
		return nil, fmt.Errorf("PixLog repository already exists at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect repository: %w", err)
	}

	for _, directory := range []string{
		filepath.Join(control, "objects", "sha256"),
		filepath.Join(control, "refs", "heads"),
		filepath.Join(control, "refs", "remotes"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create repository directory: %w", err)
		}
	}

	config := Config{
		Schema:        SchemaVersion,
		Bare:          bare,
		DefaultBranch: "main",
		Remotes:       map[string]Remote{},
	}
	repo := &Repository{Root: root, Control: control, Config: config}
	if err := writeJSON(filepath.Join(control, "config.json"), config); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(control, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		return nil, err
	}
	if !bare {
		index := Index{Schema: SchemaVersion, Entries: map[string]Entry{}}
		if err := repo.WriteIndex(index); err != nil {
			return nil, err
		}
	}

	return repo, nil
}

func Open(start string) (*Repository, error) {
	if start == "" {
		start = "."
	}
	current, err := filepath.Abs(start)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	if info, statErr := os.Stat(current); statErr == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}

	for {
		control := filepath.Join(current, ControlDirName)
		config, readErr := readConfig(filepath.Join(control, "config.json"))
		if readErr == nil {
			return &Repository{Root: current, Control: control, Config: config}, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return nil, ErrNotRepository
}

func OpenRemote(path string) (*Repository, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve remote path: %w", err)
	}

	for _, candidate := range []struct {
		root    string
		control string
	}{
		{root: root, control: filepath.Join(root, ControlDirName)},
		{root: root, control: root},
	} {
		config, readErr := readConfig(filepath.Join(candidate.control, "config.json"))
		if readErr == nil {
			return &Repository{Root: candidate.root, Control: candidate.control, Config: config}, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
	}

	return nil, fmt.Errorf("remote %s is not an initialized PixLog repository", path)
}

func readConfig(path string) (Config, error) {
	var config Config
	if err := readJSON(path, &config); err != nil {
		return Config{}, err
	}
	if config.Schema != SchemaVersion {
		return Config{}, fmt.Errorf("unsupported repository schema %d", config.Schema)
	}
	if config.Remotes == nil {
		config.Remotes = map[string]Remote{}
	}
	return config, nil
}

func (r *Repository) SaveConfig() error {
	return writeJSON(filepath.Join(r.Control, "config.json"), r.Config)
}

func (r *Repository) ReadIndex() (Index, error) {
	if r.Config.Bare {
		return Index{}, errors.New("bare repositories do not have an index")
	}
	var index Index
	if err := readJSON(filepath.Join(r.Control, "index.json"), &index); err != nil {
		return Index{}, fmt.Errorf("read index: %w", err)
	}
	if index.Schema != SchemaVersion {
		return Index{}, fmt.Errorf("unsupported index schema %d", index.Schema)
	}
	if index.Entries == nil {
		index.Entries = map[string]Entry{}
	}
	return index, nil
}

func (r *Repository) WriteIndex(index Index) error {
	if r.Config.Bare {
		return errors.New("bare repositories do not have an index")
	}
	index.Schema = SchemaVersion
	if index.Entries == nil {
		index.Entries = map[string]Entry{}
	}
	return writeJSON(filepath.Join(r.Control, "index.json"), index)
}

func (r *Repository) Store(data []byte) (string, error) {
	return r.StoreReader(bytes.NewReader(data))
}

func (r *Repository) StoreReader(reader io.Reader) (string, error) {
	temporary, err := os.CreateTemp(filepath.Join(r.Control, "objects"), ".pixlog-object-*")
	if err != nil {
		return "", fmt.Errorf("create temporary object: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), reader); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", fmt.Errorf("sync object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close object: %w", err)
	}

	hexDigest := hex.EncodeToString(hash.Sum(nil))
	oid := "sha256:" + hexDigest
	destination, err := r.ObjectPath(oid)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return oid, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect object: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("create object shard: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", fmt.Errorf("install object: %w", err)
	}
	if err := os.Chmod(destination, 0o444); err != nil {
		return "", fmt.Errorf("protect object: %w", err)
	}
	return oid, nil
}

func (r *Repository) StoreFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	return r.StoreReader(file)
}

func (r *Repository) StoreJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode object: %w", err)
	}
	return r.Store(data)
}

func (r *Repository) Load(oid string) ([]byte, error) {
	path, err := r.ObjectPath(oid)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", oid, err)
	}
	return data, nil
}

func (r *Repository) ObjectPath(oid string) (string, error) {
	digest, err := parseOID(oid)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.Control, "objects", "sha256", digest[:2], digest[2:]), nil
}

func parseOID(oid string) (string, error) {
	digest := strings.TrimPrefix(oid, "sha256:")
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("invalid object id %q", oid)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("invalid object id %q", oid)
	}
	return digest, nil
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *Repository) HeadRef() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.Control, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if !strings.HasPrefix(value, "ref: ") {
		return "", errors.New("detached HEAD is not supported yet")
	}
	return strings.TrimPrefix(value, "ref: "), nil
}

func (r *Repository) CurrentBranch() (string, error) {
	ref, err := r.HeadRef()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(ref, "refs/heads/"), nil
}

func (r *Repository) HeadOID() (string, error) {
	ref, err := r.HeadRef()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(r.Control, filepath.FromSlash(ref)))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", ref, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *Repository) SetHeadOID(oid string) error {
	if _, err := parseOID(oid); err != nil {
		return err
	}
	ref, err := r.HeadRef()
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.Control, filepath.FromSlash(ref)), []byte(oid+"\n"), 0o644)
}

func (r *Repository) ReadCommit(oid string) (Commit, error) {
	data, err := r.Load(oid)
	if err != nil {
		return Commit{}, err
	}
	var commit Commit
	if err := json.Unmarshal(data, &commit); err != nil {
		return Commit{}, fmt.Errorf("object %s is not a commit: %w", oid, err)
	}
	if commit.Schema != SchemaVersion || commit.Tree == nil || commit.Message == "" {
		return Commit{}, fmt.Errorf("object %s is not a valid commit", oid)
	}
	return commit, nil
}

func (r *Repository) ResolveCommit(revision string) (string, Commit, error) {
	if revision == "" {
		revision = "HEAD"
	}
	base := revision
	steps := 0
	if before, after, found := strings.Cut(revision, "~"); found {
		base = before
		if after == "" {
			steps = 1
		} else if _, err := fmt.Sscanf(after, "%d", &steps); err != nil || steps < 0 {
			return "", Commit{}, fmt.Errorf("invalid revision %q", revision)
		}
	}

	var oid string
	var err error
	switch {
	case base == "HEAD" || base == "":
		oid, err = r.HeadOID()
	case strings.HasPrefix(base, "sha256:"):
		oid = base
	default:
		if err := validateRefName(base); err != nil {
			return "", Commit{}, fmt.Errorf("invalid revision %q", base)
		}
		for _, namespace := range []string{"heads", "tags"} {
			data, readErr := os.ReadFile(filepath.Join(r.Control, "refs", namespace, filepath.FromSlash(base)))
			if readErr == nil {
				oid = strings.TrimSpace(string(data))
				break
			}
			if !errors.Is(readErr, os.ErrNotExist) {
				return "", Commit{}, fmt.Errorf("read revision %q: %w", base, readErr)
			}
		}
		if oid == "" {
			return "", Commit{}, fmt.Errorf("unknown revision %q", base)
		}
	}
	if err != nil {
		return "", Commit{}, err
	}
	if oid == "" {
		return "", Commit{}, errors.New("repository has no commits")
	}

	commit, err := r.ReadCommit(oid)
	if err != nil {
		return "", Commit{}, err
	}
	for range steps {
		if commit.Parent == "" {
			return "", Commit{}, fmt.Errorf("revision %q has no such ancestor", revision)
		}
		oid = commit.Parent
		commit, err = r.ReadCommit(oid)
		if err != nil {
			return "", Commit{}, err
		}
	}
	return oid, commit, nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pixlog-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return fmt.Errorf("set file mode: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
