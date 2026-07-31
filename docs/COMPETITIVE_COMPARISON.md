# PixLog Competitive Comparison

This table is a positioning aid, not a benchmark or a claim that one tool should
replace every other tool. Capabilities refer to first-party behavior and should
be rechecked against upstream documentation before publication.

| Product/category | Primary job | Image-aware visual diff | Generation provenance | Git-native image policy | Large media / remote |
| --- | --- | --- | --- | --- | --- |
| **PixLog** | Git media history and provenance | Pixel metrics, regions, heatmap, visual blame | Recipe, capture, lineage, guarded replay | Format, size, recipe, SSIM, ratio, region and mask rules | Pointer/CAS with file and HTTP Batch endpoints |
| **Git LFS** | Replace large Git blobs with pointers | Uses host/client preview and diff facilities | No first-party generation recipe model | No first-party pixel/SSIM/region policy | Mature LFS servers and file locking |
| **DVC** | Version data, models, pipelines and experiments alongside Git | No first-party pixel-region history or visual blame | Pipeline/dependency metadata rather than image-generation recipes | No first-party image-region policy | Multiple cloud/object-storage remotes |
| **git-annex** | Manage file content across many remotes without placing it in Git | No first-party image-aware diff | No first-party generation recipe model | No first-party image-region policy | Broad content-location and remote model |
| **Perforce Helix Core** | Centralized versioning and locking for large binary production assets | Tool/extension dependent | No PixLog-style portable generation recipe | Workflow/tool dependent | Mature centralized large-binary workflows |
| **C2PA** | Signed content provenance and authenticity claims | Not a visual version-control diff | Signed assertions/actions, not a Git history or complete private workflow | Not a PR image policy engine | Embedded or externally referenced manifests |
| **DAM/review tools** | Search, organize, approve and distribute creative assets | Often strong visual review, comments and approval | Vendor dependent | Usually not Git revision-range policy | Hosted asset storage and delivery |

## Honest Boundary

PixLog should be presented as a complement where appropriate:

- Use Git LFS/DVC/git-annex when their remote ecosystem is the deciding need.
- Use C2PA when interoperable signed credentials are required; PixLog delegates
  cryptographic operations to the official `c2patool` and can import/export signals.
- Use a DAM/review product when nontechnical review, approvals and asset discovery
  are primary; PixLog does not yet ship a local review UI.
- Use PixLog when a Git team needs exact bytes, image-aware history, generation
  recipes, and enforceable visual rules in one scriptable workflow.

Current PixLog limitations relevant to evaluation: built-in visual decode is PNG,
JPEG and GIF; direct cloud adapters, hosted collaboration, GitHub Checks publishing,
geometric registration and a review UI remain planned or partial.
