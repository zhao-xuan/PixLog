# Recipe and Provenance Format

The current schema identifier is `pixlog.recipe/v1`. A recipe is normalized JSON,
stored in the same SHA-256 object store as image bytes, and referenced by an asset
entry. Only `schema` and non-empty `kind` are mandatory in the alpha; the remaining
shape is intentionally extensible while adapters stabilize.

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
```

Import updates the staged image entry, so it must be committed afterward. Recipe
diff flattens nested objects and arrays into stable field paths such as
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

## Authority and Portability

The repository recipe is authoritative. Applications may also embed a portable
copy in PNG/XMP/C2PA later, but embedded metadata can disappear during screenshot,
social upload, optimization, or format conversion. A missing embedded record must
not erase repository provenance.
