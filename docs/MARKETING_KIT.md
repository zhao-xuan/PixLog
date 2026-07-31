# PixLog 传播素材包

## 核心定位

中文：**面向 Git 图片资产的视觉差异与生成溯源。** Git 告诉你图片变了；PixLog
告诉你哪些像素变了、由谁修改，以及这张图片是如何生成的。

English: **Visual diffs and generation provenance for images in Git.** Git tells
you an image changed. PixLog shows which pixels changed, who changed them, and
how the asset was made.

首批用户应聚焦设计工程师、前端/游戏开发者、创意技术团队，以及已经使用 Git 的
AI 图像工作流团队。当前产品以 CLI 为主，不把普通非技术设计师作为冷启动人群。

## 素材清单

| 主题 | GIF / MP4 / 封面 | 用途 |
| --- | --- | --- |
| 主演示：视觉历史 | `pixlog-visual-history.*` | README、Show HN、产品总览 |
| CI 保护区域 | `pixlog-policy-check.*` | GitHub Action、工程团队 |
| Visual blame | `pixlog-visual-blame.*` | 开发者社区、问题导向短帖 |
| AI 生成溯源 | `pixlog-ai-provenance.*` | ComfyUI/A1111/创意开发社区 |

所有文件位于 `docs/assets/demos/`。视频为 `1200x720`，单支 9–20 秒；GIF 适合
GitHub 自动播放，MP4 适合社交平台，PNG 用作封面与无动画回退。

推荐替代文本：

- Visual history: “PixLog stages an image edit, reports changed pixels, SSIM and
  two changed regions, commits it, then identifies that commit with visual blame.”
- Policy: “PixLog rejects an image edit because it changes protected pixels,
  exceeds the visual-change limit, and falls below the required SSIM.”
- Blame: “PixLog maps coordinate 760,145 to the commit that changed its pixel region.”
- Provenance: “PixLog shows the model, prompt, seed, tool, and reproducibility
  status attached to an AI-generated image in Git.”

## 演示仓库

执行 `make marketing-demo` 会生成 `dist/marketing/pixlog-demo-public`。将该目录
作为独立的 `pixlog-demo` GitHub 仓库发布；它包含一条初始提交和确定性原图。访问者
运行 `bash setup.sh` 后，会得到视觉 diff、blame、recipe 与策略检查所需的完整历史。

发布前验证：

```bash
make marketing-demo
cd dist/marketing/pixlog-demo-public
PATH="../../../../bin:$PATH" bash setup.sh
pixlog diff HEAD~1 HEAD -- assets/hero.png
```

## GitHub Action

根目录的 `action.yml` 提供 composite Action，负责下载并校验 Release、安装 Git
集成、运行 PR revision range 策略，并把结果写入 Actions Summary。完整示例位于
`examples/pixlog-check.yml`。发布包含 Action 的稳定标签后，生产仓库应把 `@main`
改为版本标签或完整 commit SHA。

## AI 自动录屏

录制系统使用 VHS 声明式 tape，而不是桌面坐标自动化：

```bash
brew install chafa imagemagick vhs ffmpeg
make marketing-record
```

AI 可编辑 `marketing/tapes/*.tape` 改写命令、节奏和主题，再调用单场景录制：

```bash
bash scripts/marketing/record-demos.sh ai-provenance
```

每次录制都会重建隔离仓库，运行真实 PixLog 命令，并同时生成 GIF、MP4 和封面。
详细约束见 `marketing/README.md`。

## 发布检查

1. 主 GIF 首帧两秒内出现产品名与问题，不先展示安装过程。
2. 文案只承诺已实现能力；Web UI、云适配器和托管协作明确标为规划项。
3. 每篇内容只讲一个具体问题，再链接演示仓库和主仓库。
4. 所有帖子使用带 UTM 的独立链接，区分 HN、V2EX、Reddit 与社区来源。
5. 记录安装成功、首次 diff、7 日复用、启用 Action 的仓库数，不只统计 Star。
