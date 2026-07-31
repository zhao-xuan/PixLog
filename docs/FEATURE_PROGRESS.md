# PixLog Feature Progress

Status meanings:

- **Done**: implemented and covered by an executable test or build check.
- **Partial**: a usable subset is implemented; the remaining boundary is listed.
- **Planned**: product requirement only; it is not available in the binary.

## Phase Summary

| Phase | Status | Completion boundary |
| --- | --- | --- |
| Phase 1: Git-compatible core | **Done** | Git add/commit/checkout use one index and pointer-backed media |
| Phase 2: Remote | **Partial** | File and HTTP Batch work; cloud/provider adapters do not |
| Phase 3: Visual experience | **Partial** | CLI, diff driver, difftool, and safe PNG merge work; Web UI and semantic diff do not |
| Phase 4: AI provenance | **Partial** | Shared capture protocol, adapters, metadata/C2PA, graph, and guarded replay work; adapter host packaging and deterministic provider execution do not |
| Phase 5: Collaboration | **Partial** | File locks, verification, safe merge, and blame work; hosted collaboration does not |

## Phase 1: Git-Compatible Core

**Overall: Done**

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Git is the sole VCS authority | Done | Current CLI initialization and VCS commands use Git; no second HEAD/index is created |
| `pixlog` and `git-pixlog` entry points | Done | Both binaries build from the same CLI; enables `git pixlog` |
| `pixlog init` / `install` | Done | Idempotently writes attributes/config and local drivers/hooks |
| `pixlog track` | Done | Custom attribute patterns persist across reinstall |
| Canonical pointer format | Done | LFS-compatible required fields, validation, and 1024-byte limit tested |
| Local media CAS | Done | Blob, manifest, and recipe SHA-256 objects are stored under `.git/pixlog` |
| Git filter-process v2 | Done | Real `git add` stores a pointer and checkout restores exact image bytes |
| `pixlog add` | Done | Records optional recipe association and stages through Git |
| Git-backed status and diff | Done | HEAD/index/worktree and arbitrary Git revisions are supported |
| Porcelain status compatibility | Done | `--porcelain[=v2] -z` is a byte-for-byte Git passthrough |
| Git command proxy | Done | Arguments and Git exit codes are preserved; `pixlog git ...` is an escape hatch |
| Legacy Git asset reads | Done | Raw image blobs and valid `.pixlog-meta` sidecars remain readable |

Phase 1 acceptance is covered by pointer unit tests, filter-process integration,
CLI proxy tests, clone tests, and the full repository test suite.

## Phase 2: Remote

**Overall: Partial**

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Pre-push media ordering | Done | Referenced blob/manifest/recipe objects upload before Git refs |
| Transitive recipe transfer | Done | Inputs, outputs, masks, models, workflows, vendor payloads, and nested recipes upload recursively |
| Existing hook preservation | Done | A prior pre-push hook is retained and executed by the dispatcher |
| Local path and `file://` endpoint | Done | Upload, fetch, verification, dehydrate, and rehydrate round trip tested |
| HTTP Batch client | Done | Basic upload/download actions, headers, and verify action tested |
| Fetch-on-smudge | Done | Missing local objects are fetched from the configured `origin` endpoint |
| Explicit hydrate/dehydrate | Done | Tracked pointer assets round trip between pointer and exact worktree bytes |
| Clone integration | Done | Clone installs local drivers/hook and hydrates from a file endpoint |
| `pixlog push/pull/fetch/clone` Git routing | Done | Commands enter Git's implementation rather than maintaining PixLog refs |
| Direct S3-compatible adapter | Planned | Use an HTTP Batch service or file endpoint today |
| Direct Azure Blob adapter | Planned | Use an HTTP Batch service or file endpoint today |
| Hosted PixLog object service | Planned | This repository provides the client protocol, not a hosted server |
| Delayed/preview/metadata-only checkout | Planned | Current smudge and clone hydrate full original bytes |

## Phase 3: Visual Experience

**Overall: Partial**

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Worktree, staged, and revision diff | Done | Uses Git snapshots and hydrates PixLog pointers transparently |
| Native `git diff` driver | Done | Deterministic text output with visual metrics and changed regions |
| Git difftool integration | Done | `difftool.pixlog.cmd` invokes direct image comparison |
| Direct file comparison | Done | `pixlog compare OLD NEW` works without snapshot selection |
| File and manifest diff | Done | Exact content, format, dimensions, size, visual hash, and recipe IDs |
| Raster pixel diff | Partial | Built-in decode supports PNG, JPEG, and GIF |
| Embedded metadata inspection | Partial | PNG/JPEG EXIF, XMP, ICC, IPTC, and C2PA signals are extracted; coverage is intentionally selective rather than a complete metadata-editor implementation |
| Metrics and changed regions | Done | Change ratio, mean delta, RMSE, global SSIM, and connected regions |
| Heatmap export | Done | `pixlog diff --heatmap` writes a PNG for one changed asset |
| Conservative visual merge | Partial | Same-size, non-conflicting PNG pixels merge; other cases stay conflicted |
| Geometric registration | Planned | Crop, translation, rotation, flip, and perspective are not aligned |
| Local Web UI | Planned | No swipe/onion-skin/review UI is shipped |
| AI semantic change summary | Planned | No inferred natural-language summary is emitted |
| Before/after operation inference | Done | `recipe infer` emits geometry/pixel/region evidence and confidence, always marked `inferred` |

## Phase 4: AI Provenance

**Overall: Partial**

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Canonical recipe object and OID | Done | Normalized `pixlog.recipe/v1` JSON is stored in CAS |
| SQLite provenance journal | Done | Associations plus capture sessions, ordered events, jobs, checkpoints, and artifacts are tested |
| Ordinary `git add` recipe attachment | Done | Clean filter queries the journal and embeds the recipe OID in the pointer |
| ComfyUI PNG adapter | Done | Captures embedded workflow and prompt graph |
| AUTOMATIC1111 PNG adapter | Done | Captures embedded parameters text |
| `pixlog run -- command` | Done | Successful output/deletion delta is staged with Git source context |
| Manual recipe import/show/diff/infer | Done | Historical field diff and explicitly low-trust before/after inference are supported |
| Shared capture daemon | Done | Loopback `pixlog.capture/v1`, token-aware browser CORS, health, sessions, events, jobs, checkpoints, and artifacts are tested |
| Capture finalization | Done | A session becomes a canonical recipe associated with exact output bytes |
| Photoshop UXP adapter source | Partial | Allow-listed Action Descriptor capture and panel are implemented; release packaging and validation in a supported Photoshop host remain |
| Photoshop History Log fallback | Done | Scrubbed logs import as `application-history` / `provenance-only` recipes |
| Browser MV3 adapter source | Partial | User-triggered `ui-observed` capture is implemented; store packaging and per-site host validation remain |
| Explicit provider/API proxy | Done | ComfyUI, AUTOMATIC1111, OpenAI, Firefly, and generic explicit origins can be captured without TLS MITM |
| Provider image artifact harvesting | Partial | Direct image responses plus A1111 `images[]` and OpenAI `b64_json` enter CAS; provider-specific asynchronous polling/download remains |
| Credential and payload scrubbing | Done | Headers, recursive JSON/form fields, multipart values, signed URLs, and history text are scrubbed; unknown unstructured payloads become digest-only records |
| Guarded command reproduction | Done | Captured commands require unredacted arguments and matching clean HEAD/index |
| Guarded provider request replay | Done | Only `exact-request` plans run; caller supplies a new base URL and environment-owned bearer token; redacted bodies are rejected |
| Git and object lineage graph | Done | Rename-aware commits plus recursively verified/hydratable recipe inputs, outputs, models, masks, workflows, vendor objects, and nested recipes |
| Recipe trust contract | Done | Fidelity and reproducibility enums are separately validated while provider fields remain extensible |
| Metadata import | Done | Supported PNG/JPEG EXIF, XMP, ICC, IPTC, C2PA, ComfyUI, and A1111 records can become recipes |
| C2PA import/export/verify/sign | Done | Public recipe actions map to manifests; verification/signing delegate to official external `c2patool` |
| Deterministic provider output reproduction | Planned | Request replay cannot freeze a vendor's hidden model/runtime or guarantee identical output |

## Phase 5: Collaboration

**Overall: Partial**

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Local asset locks | Done | Atomic acquisition, ownership checks, listing, and release tested |
| Shared file-endpoint locks | Done | Two clients contend through the same lock directory |
| HTTP lock service | Planned | HTTP endpoints currently transfer objects only |
| Object verification | Done | `verify` detects missing or deliberately corrupted CAS objects |
| Installation diagnostics | Done | `doctor` checks filter, diff, merge, attributes, hook, config, and objects |
| Safe region merge | Done | Non-overlapping PNG edits merge and produce a provenance recipe |
| Visual point blame | Done | Git history and rename-aware changed regions identify the introducing commit |
| Server-side pointer validation | Planned | No pre-receive service is included |
| GitHub Checks integration | Planned | CI can run `pixlog check`, but no Checks API publisher exists |
| Review/approval UI | Planned | No comments, ratings, approvals, or browser review surface exists |

## Policy And CI

| Feature | Status | Evidence / limitation |
| --- | --- | --- |
| Staged policy check | Done | Evaluates Git HEAD to index and exits nonzero on violations |
| Git revision-range policy | Done | Supports `A..B` and merge-base semantics for `A...B` |
| Format, size, and recipe rules | Done | Evaluated against the selected Git snapshot |
| Maximum change and minimum SSIM | Done | Uses the visual diff engine |
| Rectangular allowed regions | Done | Changed boxes must be fully contained |
| Bitmap mask policy | Done | Changed pixels outside allowed mask pixels fail CI |

## Compatibility And Deferred Surface

- Existing raw image Git blobs and legacy `.pixlog-meta` sidecars are read-only
  compatibility inputs. New writes use pointers and CAS objects.
- The old standalone repository package remains for compatibility and future
  migration work; the current CLI uses Git as its version-control authority.
- Historical Git LFS pointers that are not PixLog pointers are not hydrated.
- SVG structural diff, advanced color metrics, OCR, face/place search, tags,
  ratings, approvals, MCP, and agent APIs remain planned.

## Verification Record

The current tree was validated in an isolated Git configuration:

```text
GIT_CONFIG_GLOBAL=/dev/null GOTELEMETRY=off go test ./... -count=1
  cmd/git-pixlog       NO TEST FILES
  cmd/pixlog           NO TEST FILES
  internal/c2pa        PASS
  internal/capture     PASS
  internal/cli         PASS
  internal/imaging     PASS
  internal/recipe      PASS
  internal/repository  PASS
```

The same tree passed `go vet ./...`, trimpath builds of both command packages,
`node --check` for all four adapter JavaScript files, JSON parsing for both adapter
manifests, `git diff --check`, and VS Code diagnostics. Before release, rerun these
checks after every cross-cutting capture/schema change.

Focused executable coverage includes:

- Real Git clean/smudge add and checkout.
- Pointer/CAS recipe and manifest integrity.
- File and HTTP Batch upload/download/verify.
- Pre-push preservation and media ordering.
- Clone installation and hydration.
- Non-overlap merge and overlap conflict.
- Rename-aware lineage and visual blame.
- Reproduction source-state guards.
- Provider replay endpoint/auth isolation and capture audit records.
- Capture daemon/proxy/finalization and recursive secret scrubbing.
- Metadata extraction, external C2PA integration, and inferred recipes.
- Recursive provenance verification/hydration and transitive pre-push transfer.
- Git command exit codes and porcelain output.

## Next Development Order

1. Validate and package the Photoshop UXP and Chromium adapters in supported host versions.
2. Add provider-specific asynchronous polling/download and richer artifact mapping.
3. Add direct S3/Azure adapters or ship a first-party HTTP Batch service.
4. Add delayed/selective hydration and a local visual review UI.
5. Add HTTP locks, server validation, and hosted review/Checks integration.