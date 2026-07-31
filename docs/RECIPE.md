# Recipe and Provenance Format

The current schema identifier is `pixlog.recipe/v1`. A recipe is normalized JSON,
stored as a SHA-256 object under `.git/pixlog/objects`, and referenced by the image
pointer. `.git/pixlog/journal.sqlite` associates current output content with its
recipe before staging. Only `schema` and a non-empty `kind` are mandatory at the
top level; when present, capture fidelity, reproducibility status, vendor payload
OIDs, and all discovered SHA-256 references are validated. The remaining shape
stays extensible while adapters stabilize.

## Trust Contract

Capture fidelity and reproducibility are separate fields. Consumers must not infer
one from the other.

| `capture.fidelity` | Meaning |
| --- | --- |
| `exact-request` | Structured request captured before provider execution |
| `exact-command` | Local command or native application command captured |
| `embedded-metadata` | Imported from image metadata after execution |
| `application-history` | Imported from an application History Log |
| `ui-observed` | User-triggered observation of a closed UI |
| `inferred` | Derived only from before/after evidence |

| `reproducibility.status` | Meaning |
| --- | --- |
| `exact` | Deterministic execution inputs and environment are available |
| `best-effort` | Important inputs are available but runtime variance remains |
| `request-reproducible` | Request can be resubmitted; provider internals may change |
| `provenance-only` | Evidence is useful but is not an executable recipe |
| `inferred` | The operation itself is a hypothesis |
| `unverified` | No reproduction claim has been validated |

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
  "capture": {
    "adapter": "comfyui-proxy",
    "adapter_version": "1",
    "fidelity": "exact-request",
    "unknown_fields": []
  },
  "vendor": {
    "name": "comfyui",
    "raw_payload_oid": "sha256:..."
  },
  "reproducibility": {
    "status": "best-effort"
  }
}
```

## Commands

```bash
pixlog recipe import output.png recipe.json
pixlog recipe show output.png
pixlog recipe show --revision HEAD~1 output.png
pixlog recipe diff HEAD~1 HEAD -- output.png
pixlog recipe infer before.png output.png
pixlog run -- magick input.png -resize 50% output.png
pixlog reproduce --revision HEAD output.png

pixlog capture guide photoshop
pixlog capture sessions
pixlog capture finalize <session-id> output.png
```

Import stores the normalized recipe, records its content association, and restages
the image so the Git pointer carries the recipe OID. Recipe diff accepts Git
revisions and flattens nested objects and arrays into stable field paths such as
`parameters.seed` and `parents[0].asset`.

Inference stores the exact before/after bytes in CAS and creates a visual evidence
record containing geometry, pixel metrics, changed regions, metadata key changes,
an operation hypothesis, and confidence. It always writes
`capture.fidelity = inferred`, `reproducibility.status = inferred`, and explicit
`unknown_fields`; it is never executable.

## Embedded Adapters

- ComfyUI: PNG `workflow` starts an `ai-generation` recipe; `prompt` is retained as
  `parameters.prompt_graph`.
- AUTOMATIC1111: PNG `parameters` is retained verbatim as `parameters.raw`.

Automatic capture records `capture.source = png-embedded-metadata`. It does not
guarantee that referenced models or custom nodes still exist.

`pixlog metadata import` additionally supports the EXIF/XMP/ICC/IPTC/C2PA fields
understood by the image inspector. `pixlog capture history` imports a Photoshop
History Log as `application-history` and `provenance-only`, preserving a scrubbed
raw log object while treating exact operation parameters, masks, and layer state as
unknown.

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
state.

Finalized proxy recipes with `exact-request` operations expose an HTTP request
plan. Execution requires all of the following:

1. The method is `POST`, `PUT`, or `PATCH` and the captured path is relative.
2. The caller supplies a fresh HTTP(S) `--base-url`; the captured host is ignored.
3. Optional bearer authentication comes only from `--auth-env <name>`.
4. The request object exists, verifies by SHA-256, and contains no `[REDACTED]`
  marker.

The response is size-limited, scrubbed into CAS, and recorded in a new capture
session. Image responses are registered as output artifacts. This is request
reproduction, not a claim that a remote provider will return identical pixels.

## Reference Graph

Recipes can reference content and other recipes through standard fields or
provider extensions. PixLog recursively discovers valid `sha256:` values and
classifies references by their surrounding path, including parents, outputs,
models, workflows, masks, vendor raw payloads, and nested recipes. The pre-push
hook transfers this full closure; `pixlog lineage --graph --verify` checks it, and
`--hydrate` fetches missing objects before verification.

Only syntactically valid SHA-256 OIDs enter the generic graph. Defined contract
fields such as `vendor.raw_payload_oid` are validated; malformed values in unknown
extension fields remain ordinary preserved data rather than graph edges. This lets
a future adapter reinterpret the original object without breaking its identity.

## C2PA Mapping

`pixlog c2pa export` maps a recipe to public `c2pa.actions`; parent presence adds a
public `c2pa.opened` action, while standard ingredient assertions remain future
work. It deliberately omits private prompts and raw vendor payloads.
Verification, certificate trust, and signing are performed by the official
external `c2patool`; `c2pa import` stores the verified report as a scrubbed CAS
object and links it from the recipe.

## Authority and Portability

The recipe object referenced by the committed pointer is authoritative. Pre-push
uploads it, the blob, manifest, and its transitive reference graph; graph hydration
and historical inspection fetch and verify objects by SHA-256 when necessary.
Legacy `.pixlog-meta` recipes and sidecars remain readable, but current writes do
not create them.

Applications may also embed a portable copy in PNG/XMP/C2PA, but embedded metadata
can disappear during screenshot, social upload, optimization, or conversion. A
missing embedded record must not erase repository provenance.
