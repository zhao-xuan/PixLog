# PixLog Feature Progress

Last updated: 2026-07-30

Status meanings:

- **Done**: implemented and covered by an executable test or binary smoke test.
- **Partial**: usable subset exists; listed limitations remain.
- **Planned**: product requirement only, not available in the binary.

## Current Milestone: Local Binary Alpha

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Single local Go binary | Done | `go build ./cmd/pixlog`; no runtime dependencies |
| Repository discovery and `.pixlog` control directory | Done | Parent-directory discovery and bare repositories |
| SHA-256 content-addressed object store | Done | Immutable sharded objects and `pixlog verify` |
| Index, status, add, remove, commit, log | Done | HEAD/index/worktree state is separated |
| Exact restore and checkout | Done | Original object bytes are restored |
| Branches, tags, branch switching | Done | Clean-worktree switching; single-parent commits |
| JSON/NDJSON output | Done | Status, diff, history queries, sync, locks, policy |

## Visual History

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| File and manifest diff | Done | Content OID, format, dimensions, size, visual/recipe IDs |
| PNG metadata diff | Partial | `tEXt` and uncompressed `iTXt`; EXIF/IPTC/XMP/ICC not parsed |
| Raster pixel diff | Partial | Built-in PNG/JPEG/GIF decode only |
| Metrics | Done | Changed ratio, mean channel delta, RMSE, global SSIM |
| Changed regions | Done | Four-connected components and bounding boxes |
| Heatmap export | Done | `pixlog diff --heatmap output.png` |
| Geometry classification | Partial | Identity/resize/dimension heuristics; no registration matrix |
| Visual point blame | Done | Walks commit history and changed regions |
| SVG structural diff | Planned | Current implementation records SVG dimensions only |
| Crop/rotation/flip/perspective alignment | Planned | Requires registration before pixel comparison |
| Lab/Delta E, edge, alpha-specific, MS-SSIM | Planned | Current engine compares NRGBA channels |
| AI semantic change summary | Planned | Must be labeled as inferred, never exact history |

## Provenance and AI Workflows

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Canonical recipe object and recipe OID | Done | Normalized JSON stored in CAS |
| Manual recipe import/show/diff | Done | Field-level flattened diff |
| ComfyUI PNG adapter | Done | Captures embedded workflow and prompt graph |
| AUTOMATIC1111 PNG adapter | Done | Captures raw parameters text |
| Recipe attached to image commit history | Done | Entry carries immutable recipe OID |
| Strict recipe JSON Schema validation | Partial | `schema` and `kind` validated; nested fields are extensible |
| Reference/mask/model artifact verification | Planned | Schema can describe them, but hashes are not resolved yet |
| `pixlog run -- command` capture | Done | Successful output delta is staged with command recipe; arguments can be redacted |
| AI API proxy and SDK | Planned | No network or provider integration in alpha |
| Reproduce a historical generation | Planned | Needs adapter-specific executors and dependency hydration |
| C2PA import/export/signature verification | Planned | Repository provenance remains unsigned |
| XMP sidecar semantic parsing | Planned | XMP is currently tracked as an exact sidecar blob |

## Sync and Collaboration

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Local path and `file://` remote | Done | Push/fetch/pull/clone round-trip tested |
| Object-before-ref push ordering | Done | Remote ref updates only after verified object copy |
| Fast-forward protection | Done | Divergent push/pull is rejected |
| Object integrity verification | Done | SHA-256 checked on transfer and by `verify` |
| Local/shared-filesystem asset locks | Done | Atomic acquisition and owner-checked release |
| SSH remote | Planned | Transport abstraction not implemented |
| Azure Blob and S3-compatible remote | Planned | Requires object-store adapters and auth |
| Lazy/preview-only clone and hydrate | Planned | Clone currently downloads all objects |
| Git LFS Batch API compatibility | Planned | No HTTP server/protocol adapter |
| Binary delta/chunking | Planned | Needs benchmark before adopting CDC or packs |

## Policy and CI

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Staged-change policy command | Done | `pixlog check` exits nonzero on violations |
| Format, size, recipe rules | Done | Evaluated against the staged index |
| Max visual change and minimum SSIM | Done | Evaluated against HEAD-to-index diff |
| Rectangular allowed-change regions | Done | Changed boxes must be fully contained |
| Bitmap mask policy | Done | Light/opaque pixels allow edits; changed pixels outside fail CI |
| Visual bisect | Done | Finds first SSIM/RMSE/change-ratio crossing against a baseline |

## Product Surface Not Yet Implemented

- Automatic image merge; flattened binary assets use locks and ours/theirs in the target design.
- Git commit/tree interoperability. The alpha is a standalone Git-like repository.
- SQLite metadata/search index, tags, ratings, reviews, approvals, OCR, face/place search.
- Lineage DAG across reference images. Current lineage is per-path commit history.
- Local Web UI. This is intentionally not part of the binary alpha milestone.
- MCP server and agent query API.

## Verification Record

Validated on macOS arm64 with Go 1.24.3 on 2026-07-30:

```text
go test ./... -count=1
  internal/imaging     PASS
  internal/repository  PASS

binary smoke workflow
  init/add/commit      PASS
  lock contention      PASS (second owner exits 1)
  policy failure       PASS (exits 1)
  policy success       PASS (exits 0)
```

## Next Development Order

1. Expand automated CLI process tests for all JSON contracts.
2. Add image registration for crop, translation, rotation, and flip.
3. Add SSH remote and lazy object hydration behind a remote interface.
4. Validate recipes with a published JSON Schema and resolve dependency OIDs.
5. Add C2PA/XMP adapters, then optional AI semantic summaries.
6. Add structured SVG diff and creative-format adapters.
