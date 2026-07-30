# Recipe and Provenance Format

The current schema identifier is `pixlog.recipe/v1`. A recipe is normalized JSON,
stored as a SHA-256 object under `.git/pixlog/objects`, and referenced by the image
pointer. `.git/pixlog/journal.sqlite` associates current output content with its
recipe before staging. Only `schema` and a non-empty `kind` are mandatory in the
alpha; the remaining shape stays extensible while adapters stabilize.

## Recommended Shape

```json
{
  "schema": "pixlog.recipe/v1",
  "kind": "ai-generation",
  "tool": {
    "name": "ComfyUI",
    "version": "0.x",
    "adapter_version": "1.0.0"
  },
  "parents": [
    { "asset": "sha256:...", "role": "reference-image" },
    { "asset": "sha256:...", "role": "inpaint-mask" }
  ],
  "model": {
    "provider": "local",
    "name": "model-name",
    "file_hash": "sha256:...",
    "license": "unknown"
  },
  "parameters": {
    "prompt": "...",
    "negative_prompt": "...",
    "seed": 183729,
    "steps": 28,
    "cfg": 6.5,
    "sampler": "...",
    "width": 1024,
    "height": 1024
  },
  "workflow": {
    "object": "sha256:..."
  },
  "environment": {
    "os": "linux",
    "gpu": "...",
    "container_digest": "sha256:...",
    "custom_nodes": []
  },
  "source_control": {
    "provider": "git",
    "head_oid": "...",
    "branch": "main",
    "index_tree_oid": "...",
    "worktree_dirty": false
  },
  "reproducibility": {
    "status": "unverified"
  }
}
```

## Commands

```bash
pixlog recipe import output.png recipe.json
pixlog recipe show output.png
pixlog recipe show --revision HEAD~1 output.png
pixlog recipe diff HEAD~1 HEAD -- output.png
pixlog run -- magick input.png -resize 50% output.png
pixlog reproduce --revision HEAD output.png
```

Import stores the normalized recipe, records its content association, and restages
the image so the Git pointer carries the recipe OID. Recipe diff accepts Git
revisions and flattens nested objects and arrays into stable field paths such as
`parameters.seed` and `parents[0].asset`.

## Embedded Adapters

- ComfyUI: PNG `workflow` starts an `ai-generation` recipe; `prompt` is retained as
  `parameters.prompt_graph`.
- AUTOMATIC1111: PNG `parameters` is retained verbatim as `parameters.raw`.

Automatic capture records `capture.source = png-embedded-metadata`. It does not
guarantee that referenced models or custom nodes still exist.

Command capture records `capture.source = pixlog-run`, executable, arguments,
working directory, timestamps, exit code, OS/architecture, previous tracked
versions, output OIDs, and tracked deletions. It intentionally omits environment
variables. `--redact-args` stores `<redacted>` placeholders while running the real
arguments unchanged.

When command capture runs inside Git, `source_control` describes the state before
the child command starts. `head_oid` and `index_tree_oid` are immutable identities;
`branch` is only a display hint. `worktree_dirty = true` means the Git commit alone
cannot exactly reproduce the source state. The current schema does not embed the
uncommitted patch.

## Reproduction Guard

`pixlog reproduce` first creates a plan. `--execute` is accepted only when the
recipe contains a captured command, no argument is redacted, the captured source
worktree was clean, and the current HEAD/index exactly match the recorded source
state. Provider-specific AI recipes can be inspected but need adapter executors
before PixLog can run them.

## Authority and Portability

The recipe object referenced by the committed pointer is authoritative. Pre-push
uploads it with the blob and manifest; smudge and historical inspection fetch and
verify it by SHA-256 when necessary. Legacy `.pixlog-meta` recipes and sidecars
remain readable, but current writes do not create them.

Applications may also embed a portable copy in PNG/XMP/C2PA, but embedded metadata
can disappear during screenshot, social upload, optimization, or conversion. A
missing embedded record must not erase repository provenance.
