<p align="center">
   <img src="docs/assets/pixlog-logo.png" alt="PixLog logo" width="600">
</p>

# PixLog

**English** | [简体中文](README.zh-CN.md)

PixLog is a Git-compatible media and provenance layer for image assets. Git owns
the index, commits, branches, merges, and remotes. PixLog adds:

1. **Exact file history** through SHA-256 content-addressed media objects.
2. **Visual history** through pixel metrics, regions, heatmaps, and visual blame.
3. **Generation history** through recipes, a local provenance journal, and guarded
   reproduction.

Git commits small PixLog pointers; worktrees contain the original image bytes.
There is no second PixLog commit graph or staging area.

## Build

PixLog requires Go 1.24 and Git.

```bash
make build
./bin/pixlog version
```

Install both command entry points into `GOBIN`:

```bash
go install ./cmd/pixlog ./cmd/git-pixlog
```

With `git-pixlog` on `PATH`, `git pixlog diff` is equivalent to
`pixlog diff`.

## Quick Start

```bash
git init artwork
cd artwork
pixlog init

# Commit the shared tracking policy. Drivers and media stay under .git/.
git add .gitattributes .pixlog.toml
git commit -m "Configure PixLog"

# Direct Git commands invoke the PixLog clean filter.
git add assets/hero.png
pixlog diff --staged
git commit -m "Add hero artwork"
git push
```

The Git index and commit contain a canonical text pointer. The clean filter stores
the original blob and its manifest under `.git/pixlog/objects`; checkout invokes
the smudge filter and restores the image bit-exactly.

`pixlog add`, `commit`, `push`, `pull`, `branch`, `rebase`, and other Git-owned
commands enter the same Git operations. Use `pixlog git <any-command>` as an
explicit Git escape hatch.

## Tracking

Installation tracks common PNG, JPEG, GIF, WebP, AVIF, HEIC, TIFF, and PSD
patterns. Add project-specific patterns without losing them on reinstall:

```bash
pixlog track 'art/**/*.kra' 'renders/*.bmp'
pixlog install
```

Commit the updated `.gitattributes`.

## Visual Diff

```bash
# Index to worktree, HEAD to index, or two Git revisions:
pixlog diff
pixlog diff --staged
pixlog diff HEAD~1 HEAD -- assets/hero.png

# Direct comparison and heatmap:
pixlog compare old.png new.png
pixlog diff --heatmap hero-heatmap.png -- assets/hero.png

# Installed Git integrations:
git diff -- assets/hero.png
git difftool --tool=pixlog HEAD~1 HEAD -- assets/hero.png
```

Supported raster decoding currently covers PNG, JPEG, and GIF. Other recognized
formats still receive exact-byte identity, manifests, pointers, recipes, transfer,
and locking, but may report that visual comparison is unavailable.

The merge driver automatically combines same-size PNG changes only when their
changed pixels do not conflict. All unsafe cases remain normal Git conflicts.

## Generation Provenance

```bash
pixlog run -- magick input.png -resize 50% output.png
pixlog recipe show output.png
git commit -m "Generate resized artwork"

# Attach a normalized recipe explicitly:
pixlog recipe import output.png examples/recipe.json

# Plan or execute a captured command from a verified source state:
pixlog reproduce --revision HEAD output.png
pixlog reproduce --revision HEAD --execute output.png
```

`pixlog run` stages outputs only after a successful command and records the source
HEAD/index state. Use `--redact-args` when command arguments contain secrets.
ComfyUI and AUTOMATIC1111 metadata embedded in PNG files is imported when present.

Recipe associations are recorded in `.git/pixlog/journal.sqlite`, so a later
ordinary `git add` preserves provenance in the pointer.

## Media Remote And Hydration

The pre-push hook scans commits being pushed and uploads referenced blobs,
manifests, and recipes before Git updates refs. Existing user pre-push hooks are
preserved.

Local Git remotes automatically use a sibling `.pixlog` media directory. Configure
an explicit file or HTTP Batch endpoint when needed:

```bash
git config pixlog.remote.origin.endpoint /srv/pixlog/project-media
# or
git config pixlog.remote.origin.endpoint https://media.example.test/project
```

```bash
pixlog dehydrate assets/hero.png
pixlog hydrate assets/hero.png
pixlog clone /srv/git/artwork.git teammate-copy
```

File endpoints and the HTTP Batch basic transfer client are implemented. Direct
S3/Azure adapters and selective or delayed checkout remain planned.

## History, Integrity, And Collaboration

```bash
pixlog lineage assets/hero.png
pixlog blame --point 823,441 assets/hero.png

pixlog verify
pixlog doctor

pixlog lock --remote origin assets/hero.psd
pixlog locks --remote origin
pixlog unlock --remote origin assets/hero.psd
```

Lineage and visual blame follow Git history across renames. Locks work locally and
through shared file endpoints; HTTP locks require a future service.

## Policy And CI

Commit [examples/policy.json](examples/policy.json) as `.pixlog-policy.json`, then
check staged changes or a pull-request range:

```bash
pixlog check
pixlog check --json
pixlog check --range origin/main...HEAD
```

Rules can constrain format, size, recipe presence, visual change, SSIM, rectangular
regions, and bitmap masks. A violation exits with status 1.

## Command Surface

| Area | Commands |
| --- | --- |
| Setup | `init`, `install`, `track`, `git install` |
| Image state | `add`, `status`, `diff`, `compare`, `inspect` |
| Provenance | `recipe import/show/diff`, `run`, `reproduce`, `lineage`, `blame` |
| Media | `hydrate`, `dehydrate`, `verify`, `doctor` |
| Collaboration | `lock`, `unlock`, `locks`, `check` |
| Git proxy | `commit`, `log`, `show`, `push`, `pull`, `merge`, `rebase`, and other Git-owned commands |
| Escape hatch | `pixlog git <any git command>` |

`pixlog status --porcelain[=v2] -z` preserves Git's machine-readable output.
PixLog-native inspection commands generally support `--json`; diff also supports
NDJSON.

## Project Documents

- [Product specification](docs/PRODUCT_SPEC.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Git integration and Phase 1-5 design](docs/GIT_INTEGRATION.md)
- [Feature progress](docs/FEATURE_PROGRESS.md)
- [Recipe and provenance format](docs/RECIPE.md)

## Verification

```bash
make check
```