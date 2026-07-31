# PixLog Launch Copy

Replace `<DEMO_URL>` after publishing the standalone demo repository. Use MP4 on
platforms that do not autoplay GitHub GIFs.

## Show HN

**Title**

Show HN: PixLog – visual diffs and generation provenance for images in Git

**Post**

Git can tell me that an image blob changed, but not which pixels changed or how an
AI-generated asset was made. I built PixLog to add that missing layer while keeping
Git as the only source of commits, branches and refs.

PixLog stores exact image bytes in a local content-addressed store and commits small
pointers. It can report changed regions, visual-change ratio, RMSE and SSIM; generate
heatmaps; blame a coordinate across Git history; attach generation recipes; and fail
CI when edits cross protected regions or masks.

The core, file/HTTP Batch transfer, PNG/JPEG/GIF visual diff, recipe capture, visual
blame and policy checks work today. A review UI, direct cloud adapters and hosted
collaboration do not exist yet.

Install: `brew install zhao-xuan/tap/pixlog`

Repository: https://github.com/zhao-xuan/PixLog

Runnable demo: `<DEMO_URL>`

I would especially value feedback from design engineers, game/creative tooling
teams, and people managing generated images in Git.

## V2EX / 掘金

**标题**

我做了 PixLog：让 Git 看懂图片变化，并记录 AI 图片是怎么生成的

**正文**

Git 很擅长代码历史，但面对 PNG/JPEG 往往只能告诉你“二进制文件变了”。PixLog
保留 Git 的提交、分支和远端模型，在其上增加图片能力：像素变化比例、SSIM、变化
区域与热力图，按坐标追踪修改提交，以及 prompt、模型、seed 和 workflow 溯源。

它还可以在 CI 中限制格式、大小、最大视觉变化和允许编辑区域。例如商品主体像素被
误改时，`pixlog check` 会直接让 PR 失败。

目前 CLI、Git filter、file/HTTP Batch、PNG/JPEG/GIF visual diff、recipe、blame 和
策略检查已经可用；本地 review UI、直接云存储适配器和托管协作仍在规划中。

安装：`brew install zhao-xuan/tap/pixlog`

项目：https://github.com/zhao-xuan/PixLog

可运行演示：`<DEMO_URL>`

希望找到正在用 Git 管理设计素材、游戏资源或 AI 图片的开发者/设计工程师做真实试用。

## Reddit: r/git / r/gamedev

**Title**

I built a Git-native visual history layer for image assets

**Body**

PixLog keeps Git as the VCS, but adds exact media storage, pixel-aware diffs,
heatmaps, visual blame, generation recipes, and visual policy checks. The demo below
stages a PNG edit, reports two changed regions and SSIM, commits it, then identifies
the commit responsible for one coordinate.

It is aimed at teams that already keep UI art, game assets, or generated images in
Git. Current caveat: the experience is CLI-first and built-in visual decoding is
PNG/JPEG/GIF. Feedback on real repository workflows is more useful to me than stars.

Repo: https://github.com/zhao-xuan/PixLog

Demo: `<DEMO_URL>`

## ComfyUI / AI Creator Community

**Post**

What produced this image six commits ago?

PixLog attaches a normalized recipe to the exact output bytes in Git, so a team can
inspect model, prompt, seed, workflow and reproducibility state later. It can import
ComfyUI/AUTOMATIC1111 PNG metadata, capture explicit API traffic, compare recipes,
and traverse referenced inputs, masks and models.

It does not claim deterministic reproduction of a hosted model, and inferred
before/after recipes are always labeled low-trust.

Repo: https://github.com/zhao-xuan/PixLog

Runnable demo: `<DEMO_URL>`

## Short Posts

**Developer**

Git says `hero.png` changed. PixLog says 1.28% of pixels changed, SSIM is 0.98882,
shows the two regions, and can identify the responsible commit. Visual diffs and
provenance for images in Git: https://github.com/zhao-xuan/PixLog

**Designer / design engineer**

Protect the product, allow the background. PixLog can fail a PR when image edits
cross approved regions or bitmap masks, while keeping the normal Git workflow.
https://github.com/zhao-xuan/PixLog

**AI workflow**

Prompt, model, seed and workflow should survive longer than the app session that
created an image. PixLog records them against exact image bytes in Git.
https://github.com/zhao-xuan/PixLog
