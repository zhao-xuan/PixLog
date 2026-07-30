# Product Specification

## Positioning

> PixLog is Git-native visual history and generation provenance for image assets.

The primary audience is developers, AI creators, design engineers, and teams that
already use Git but need image-aware diffs, large-media transport, and provenance.
PixLog extends Git rather than implementing a parallel VCS. It is not trying to
replace a general photo library in its first releases.

## Product Principles

1. Preserve original bytes so every checkout is exact.
2. Explain file, visual, and process changes separately.
3. Capture operations at generation/edit time; do not pretend flattened outputs
   reveal exact tool history.
4. Keep the default workflow local, scriptable, machine-readable, and auditable.
5. Move immutable objects before mutable refs during synchronization.
6. Degrade capabilities explicitly by format instead of claiming uniform support.
7. Keep Git as the only commit, branch, index, and ref authority.

## Target Object Model

Every asset version carries content and manifest references and may carry richer
provenance:

- `content_oid`: exact source-file SHA-256 for restore, deduplication, and sync.
- `manifest_oid`: decoded format, dimensions, color/metadata/capability facts.
- `visual_hash`: canonical or perceptual identity for similarity lookup only.
- `recipe_oid`: optional canonical workflow JSON identity.
- Parent/reference/mask OIDs for a future cross-asset lineage DAG.

Perceptual hashes must never be used as integrity or security identities.

## Diff Layers

### 1. File and Metadata

Show format, dimensions, byte size, color profile, EXIF/IPTC/XMP/ICC/C2PA changes,
and whether only encoding changed. The alpha implements a subset documented in
[FEATURE_PROGRESS.md](FEATURE_PROGRESS.md).

### 2. Geometry

Normalize orientation, then detect resize, crop, rotation, flip, translation, and
perspective changes. A production engine should return an old-to-new coordinate
transform before pixel comparison.

### 3. Visual

Target views and metrics include side-by-side, swipe, onion skin, heatmap, pixel
difference, Delta E, alpha/edge diff, SSIM/MS-SSIM, changed regions, bounding boxes,
and percentage changed. CLI JSON is the canonical automation interface.

### 4. Semantic

An optional local or cloud vision model may summarize changes such as a removed
logo or replaced background. Every such statement must be labeled
`AI-inferred change summary`; it is not an exact edit log.

## Format Capability Tiers

### Tier A: Raster

PNG, JPEG, WebP, TIFF, BMP, GIF/APNG, AVIF/HEIC, and EXR should eventually support
exact restore, metadata inspection, rendered diff, frame diff, and similarity.
Codec availability must be reported per build.

### Tier B: Structured Creative Formats

- SVG: XML/DOM, attributes, paths/text, plus final rendered diff.
- PSD/Krita/XCF/AI: layers, order, visibility, blend mode, opacity, masks, text,
  and linked objects when a parser is available.
- Unsupported structure falls back to exact binary versioning, preview/metadata
  when available, and an explicit capability marker.

### Tier C: RAW

Track the sensor file, XMP sidecar, and rendered preview separately. Many
non-destructive edits live in a sidecar or application catalog rather than the RAW
bytes.

## Recipe and Provenance

The recipe CAS object referenced by the committed pointer is authoritative;
embedded metadata is an import source and portable copy. A recipe should be able
to record:

- prompt and negative prompt;
- exact model/LoRA/VAE/ControlNet/embedding hashes and licenses;
- seed, sampler, scheduler, steps, CFG, dimensions;
- every reference image and inpaint/control mask;
- workflow graph and custom node versions;
- API provider/model version, cost, and latency;
- post-processing actions and source-code commit;
- OS, GPU, container digest, and reproducibility state.

Capture reliability order:

1. Direct SDK call.
2. Local AI API proxy.
3. ComfyUI/AUTOMATIC1111 plugin or embedded metadata adapter.
4. `pixlog run -- command` wrapper.
5. Filesystem save watcher.
6. Before/after AI inference, explicitly marked as inferred.

## Synchronization Target

Git transports commits and pointers. PixLog uploads immutable blob, manifest, and
recipe objects before Git pushes refs. The implemented transports are local/file
CAS and an HTTP LFS-style Batch client. Direct S3-compatible and Azure Blob
adapters remain targets, as do a first-party hosted Batch service and server-side
pointer validation.

Clone should eventually support metadata/thumbnails first and full-object hydration
on demand:

```bash
pixlog clone --preview-only <remote>
pixlog hydrate assets/final/**
```

Complex binary deltas are not an MVP requirement. Compressed image re-encoding can
invalidate naive byte deltas; content-defined chunking and codec-aware storage need
benchmarks on real histories before adoption.

## Merge and Locking

Flattened images cannot be merged reliably like source code. Default conflict tools
should offer locking, ours/theirs, both variants, and human review. Assisted merge
is allowed only when both sides share a base, transforms are compatible, and exact
non-overlapping edit masks or layers are available.

## Developer Workflows

Core workflow:

```bash
git init
pixlog init
git add assets/hero.png
pixlog status
pixlog diff --staged
pixlog check
git commit -m "Replace background"
git push
```

Pull-request CI evaluates a Git range rather than a synthetic staged index:

```bash
pixlog check --range origin/main...HEAD
```

Differentiating workflows:

- Visual blame: find the commit that changed a point or region.
- Visual threshold search: find the first revision crossing an SSIM/change limit.
- Change policy: ensure an AI edit stayed inside permitted regions.
- Bitmap policy masks: protect product pixels while permitting background edits.
- Recipe diff/reproduce: compare and rerun generation workflows.
- Lineage: traverse outputs, references, masks, variants, and approvals.
- Agent API/MCP: query image history and provenance programmatically.

Pointer storage, filter-process, file/HTTP transfer, worktree/staged/revision diff,
external diff/difftool, recipes, command capture, guarded reproduction, Git
lineage, visual blame, file locks, safe PNG merge, and range policy are implemented.
Cloud adapters, local review UI, provider API integration, hosted validation,
GitHub Checks, and HTTP locks remain future work.

## AI Asset Management Opportunities

- Prompt/model/seed/LoRA search and seed contact sheets.
- Parent/reference/mask/output lineage graph.
- Workflow and model-version diff.
- Exact and visual duplicate grouping.
- Approved/best version, rating, comments, and review.
- Generation cost, latency, model license, and usage restrictions.
- One-command recipe bundle export and reproducibility checks.
- Multimodal commit-message suggestions.

General photo-management features such as albums, faces, places, OCR, color search,
sharing, and backups are useful but are not the initial product wedge.

## Market Boundary

Existing categories cover portions of the problem:

- Git LFS, DVC, and git-annex: large-object storage and remotes.
- GitHub and image diff tools: result comparison.
- Perforce and artist-oriented Git clients: binary collaboration and locking.
- DAM/photo managers: search, review, approval, and organization.
- ComfyUI/AUTOMATIC1111: generation parameters and workflows.
- C2PA: signed content provenance.

PixLog's intended gap is one local binary connecting Git history with visual
regions, generation recipes, reproducibility, remote media sync, and developer
automation.

## Research References

- [Git LFS](https://git-lfs.com/)
- [GitHub image diff](https://docs.github.com/en/repositories/working-with-files/using-files/working-with-non-code-files)
- [DVC remote storage](https://dvc.org/doc/user-guide/data-management/remote-storage)
- [ComfyUI workflows](https://docs.comfy.org/development/core-concepts/workflow)
- [C2PA](https://c2pa.org/)
- [Adobe XMP](https://developer.adobe.com/xmp/docs/xmp-specifications/)
