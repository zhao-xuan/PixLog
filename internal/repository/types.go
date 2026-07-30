package repository

import "time"

const (
	ControlDirName = ".pixlog"
	SchemaVersion  = 1
)

type Config struct {
	Schema        int               `json:"schema"`
	Bare          bool              `json:"bare"`
	DefaultBranch string            `json:"default_branch"`
	Remotes       map[string]Remote `json:"remotes,omitempty"`
}

type Remote struct {
	URL string `json:"url"`
}

type Entry struct {
	Path        string `json:"path"`
	ContentOID  string `json:"content_oid"`
	ManifestOID string `json:"manifest_oid"`
	RecipeOID   string `json:"recipe_oid,omitempty"`
	Size        int64  `json:"size"`
	Format      string `json:"format"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	VisualHash  string `json:"visual_hash,omitempty"`
}

type Index struct {
	Schema  int              `json:"schema"`
	Entries map[string]Entry `json:"entries"`
}

type Commit struct {
	Schema    int              `json:"schema"`
	Parent    string           `json:"parent,omitempty"`
	Author    string           `json:"author"`
	Message   string           `json:"message"`
	CreatedAt time.Time        `json:"created_at"`
	Tree      map[string]Entry `json:"tree"`
}

type CommitRecord struct {
	OID    string `json:"oid"`
	Commit Commit `json:"commit"`
}

type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

type Change struct {
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
	Old  *Entry     `json:"old,omitempty"`
	New  *Entry     `json:"new,omitempty"`
}

type Status struct {
	Branch    string   `json:"branch"`
	Head      string   `json:"head,omitempty"`
	Staged    []Change `json:"staged"`
	Unstaged  []Change `json:"unstaged"`
	Untracked []string `json:"untracked"`
}
