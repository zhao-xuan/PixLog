# PixLog

PixLog is a local, Git-like binary for image assets and AI generation provenance.
It tracks three histories independently:

1. **File history**: exact bytes, format, size, and metadata.
2. **Visual history**: where pixels changed and how large the change is.
3. **Process history**: prompt, model, seed, workflow, references, and tool metadata.

PixLog is currently an alpha standalone VCS written in Go. It is not a Web app,
does not require a browser or service, and does not currently wrap a Git repository.

## Build

Go 1.24 or newer is required.

```bash
make build
./bin/pixlog version
```

Install directly into `GOBIN`:

```bash
go install ./cmd/pixlog
```

## Quick Start

```bash
mkdir artwork && cd artwork
pixlog init

pixlog add assets/hero.png
pixlog status
pixlog commit -m "Add hero artwork"

# Edit the image, then inspect the unstaged visual change.
pixlog diff assets/hero.png
pixlog diff --json assets/hero.png
pixlog diff --heatmap hero-heatmap.png assets/hero.png

pixlog add assets/hero.png
pixlog commit -m "Replace background"
pixlog log assets/hero.png
pixlog lineage assets/hero.png
pixlog blame --point 823,441 assets/hero.png
pixlog bisect --asset assets/hero.png --against baseline.png \
	--metric ssim --threshold 0.98
```

The control data lives in `.pixlog/`. Original bytes are stored by SHA-256, so
checkout and restore are byte-exact.

## Generation Recipes

Attach a repository-side recipe to a staged image:

```bash
pixlog add output.png
pixlog recipe import output.png recipe.json
pixlog recipe show output.png
pixlog commit -m "Generate product variant"
```

PixLog automatically captures ComfyUI `workflow`/`prompt` and
AUTOMATIC1111 `parameters` from PNG text chunks when present. Repository recipe
objects are authoritative because embedded metadata can be removed by export,
compression, or upload pipelines. See [docs/RECIPE.md](docs/RECIPE.md) and
[examples/recipe.json](examples/recipe.json).

Wrap an image-producing command to capture the process and stage its outputs:

```bash
pixlog run -- magick input.png -resize 50% output.png
pixlog run --redact-args -- private-editor --token-sensitive-argument
```

Only successful commands are staged. PixLog detects added, modified, and tracked
deleted images. It records command arguments by default but never captures the
child environment; use `--redact-args` when arguments contain secrets.

## Local Remotes

The alpha supports local paths and `file://` remotes:

```bash
pixlog init --bare /srv/pixlog/design-assets
pixlog remote add origin /srv/pixlog/design-assets
pixlog push

pixlog clone /srv/pixlog/design-assets teammate-copy
pixlog pull
```

Objects are copied and hash-verified before a remote branch ref is updated.
Push and pull reject divergent histories; merge is not implemented yet.

## Binary Locks

```bash
pixlog lock --remote origin assets/hero.psd
pixlog locks --remote origin
pixlog unlock --remote origin assets/hero.psd
```

Locks use atomic file creation on the selected repository. A shared filesystem
remote therefore provides cross-process lock contention without a server.

## Change Policy

Commit [examples/policy.json](examples/policy.json) as `.pixlog-policy.json`,
then check staged changes in CI:

```bash
pixlog check
pixlog check --json
```

Rules can constrain format, file size, recipe presence, visual change ratio,
SSIM, rectangular allowed-change regions, and a repository-relative bitmap mask.
In `allowed_change_mask`, opaque light pixels allow edits while transparent or dark
pixels protect content. A violation exits with status 1.

## Commands

| Area | Commands |
| --- | --- |
| Repository | `init`, `add`, `rm`, `status`, `commit`, `log`, `restore` |
| Visual history | `diff`, `inspect`, `lineage`, `blame`, `bisect` |
| Provenance | `recipe import`, `recipe show`, `recipe diff`, `run` |
| Refs | `branch`, `switch`, `tag` |
| Sync | `remote`, `push`, `fetch`, `pull`, `clone`, `verify` |
| Collaboration | `lock`, `unlock`, `locks`, `check` |

Most inspection commands support `--json`; `diff` also supports NDJSON.

## Format Capabilities

| Capability | Current support |
| --- | --- |
| Exact byte versioning | PNG, JPEG, GIF, WebP, BMP, TIFF, AVIF/HEIC, EXR, SVG, PSD/Krita/XCF/AI, common RAW files, XMP |
| Built-in raster decode and visual diff | PNG, JPEG, GIF |
| Structured inspection | SVG dimensions/viewBox only |
| Embedded generation metadata | PNG `tEXt` and uncompressed `iTXt` |
| Opaque fallback | Every recognized asset without a built-in decoder |

Tracking support does not imply full visual decoding. Unsupported visual formats
still retain exact bytes, content identity, manifest history, recipe references,
and synchronization.

## Project Documents

- [Product specification](docs/PRODUCT_SPEC.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Feature progress](docs/FEATURE_PROGRESS.md)
- [Recipe and provenance format](docs/RECIPE.md)

## Validate

```bash
make check
```
