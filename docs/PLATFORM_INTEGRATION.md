# Platform Capture And Integration

本页前半部分描述当前二进制和仓库中已经存在的接入面；后半部分保留设计背景，不能
当作已交付功能清单。最新完成度以 [FEATURE_PROGRESS.md](FEATURE_PROGRESS.md) 为准。

核心原则不变：优先在操作发生时捕获；只有没有原生事件、API、workflow 或 metadata
时，才根据最终图片推断。PixLog 不做透明 TLS MITM，也不把捕获完整度等同于可复现性。

## Current Adapter Matrix

| 平台 | 当前入口 | Fidelity | Reproducibility | 状态与边界 |
| --- | --- | --- | --- | --- |
| Photoshop | `adapters/photoshop` UXP + `capture serve` | `exact-command` | `best-effort` | allow-list 事件、raw descriptor、checkpoint 已实现；需在目标 Photoshop 版本中加载验证与发布打包 |
| Photoshop fallback | `capture history` | `application-history` | `provenance-only` | 导入详细 History Log，无法恢复完整参数、mask 或 layer state |
| ComfyUI | `capture proxy --platform comfyui` | `exact-request` | `best-effort` | request/response 与 prompt job ID 已记录；经代理返回的 `image/*` 自动成为 artifact，尚未主动轮询所有异步输出 |
| AUTOMATIC1111 / Forge | `capture proxy --platform automatic1111` | `exact-request` | `best-effort` | txt2img/img2img 请求与 `images[]` base64 输出捕获；PNG metadata 仍是 fallback |
| OpenAI image API | `capture proxy --platform openai` | `exact-request` | `request-reproducible` | request/response 与 `b64_json` 输出捕获；provider 后台模型不可冻结 |
| Firefly | `capture proxy --platform firefly` | `exact-request` | `request-reproducible` | 异步 job envelope 捕获；多 header 认证请求目前只能捕获，Bearer-only endpoint 才能直接重放 |
| 封闭 Web 工具 | `adapters/browser` MV3 + `capture serve` | `ui-observed` | `provenance-only` | 仅用户主动点击时采集；需按站点验证 selector 与条款，不监听私有网络流量 |
| 文件 metadata | `metadata inspect/import` | `embedded-metadata` | `best-effort` 或 `provenance-only` | 支持已实现的 PNG/JPEG EXIF、XMP、ICC、IPTC、C2PA、ComfyUI、A1111 字段 |
| Before/after | `recipe infer` | `inferred` | `inferred` | 本地几何/像素/区域启发式与 confidence；不声称识别出真实操作参数 |

运行 `pixlog capture guide` 可列出平台，运行
`pixlog capture guide <platform>` 可得到与当前版本一致的 CLI 步骤和验证命令。

## Shared Capture Workflow

原生 Photoshop 与浏览器 adapter 使用 token-protected loopback daemon：

```bash
export PIXLOG_CAPTURE_TOKEN="$(pixlog capture token)"
pixlog capture serve
pixlog capture status
```

daemon 只允许 loopback bind。配置 token 后，浏览器 origin 必须携带
`X-PixLog-Token`；未配置 token 时浏览器 CORS 被拒绝。adapter 写入 session、ordered
events、jobs、checkpoints 和 artifacts，不会直接创建 Git commit。

API 工具使用显式代理。例如 AUTOMATIC1111：

```bash
pixlog capture proxy \
  --platform automatic1111 \
  --upstream http://127.0.0.1:7860 \
  --listen 127.0.0.1:7861
```

用户必须主动把 API client 的 base URL 改到 PixLog 端口。代理把请求原样转发到
显式 upstream，但只保存脱敏后的 payload；Authorization/cookie 不进入 recipe 或
CAS，JSON/form/multipart 的敏感字段和 signed URL query 也会被清除。

生成或编辑完成后停止 adapter/proxy，再把 session 与精确输出绑定：

```bash
pixlog capture sessions
pixlog capture show <session-id>
pixlog capture finalize <session-id> assets/output.png
pixlog recipe show assets/output.png
```

`finalize` 将 events、jobs、checkpoints、inputs、outputs 和 raw payload OID 规范化为
`pixlog.recipe/v1`，写入 CAS，记录 content-to-recipe association，并通过 Git filter
重新暂存该资产。

## Platform Commands

### Photoshop UXP

```bash
pixlog capture guide photoshop
export PIXLOG_CAPTURE_TOKEN="$(pixlog capture token)"
pixlog capture serve
```

在 Photoshop UXP Developer Tool 中加载 `adapters/photoshop`，在面板中设置 daemon
URL 与同一个 token。插件只发送 allow-list Action Descriptor 和轻量状态，不在 UI
thread 中 hash、diff 或上传大文件。保存文档后运行 `capture sessions/show/finalize`。

无法加载 UXP 时，在 Photoshop 中启用详细 History Log，并导入：

```bash
pixlog capture history photoshop-history.txt assets/output.psd
```

日志进入 CAS 前会清除常见 token/API key/password；recipe 明确标记
`application-history` 和 `provenance-only`。

### ComfyUI

```bash
pixlog capture proxy --platform comfyui \
  --upstream http://127.0.0.1:8188 --listen 127.0.0.1:8189
```

把 ComfyUI client 指向 `127.0.0.1:8189`。`POST /prompt` 和 `/history` envelope 会
进入同一 session，`prompt_id` 进入 job 表；经代理读取的 `image/*` 响应自动登记为
output artifact。完成后仍应将下载到仓库的实际输出文件传给 `capture finalize`。模型
路径、custom node commit 和运行时差异仍需调用方维护；相同 workflow/seed 不代表
跨 GPU/runtime 的 bit-exact 输出。

### AUTOMATIC1111 / Forge

```bash
pixlog capture proxy --platform automatic1111 \
  --upstream http://127.0.0.1:7860 --listen 127.0.0.1:7861
```

API client 改用端口 7861。代理区分 txt2img/img2img，并保留 extension-owned JSON
字段；合法 `images[]` base64 图片会解码、MIME 校验并成为 output artifact。旧 PNG
可继续用 `pixlog metadata import <asset>` 导入 infotext fallback。

### OpenAI / Firefly / Generic HTTP API

```bash
pixlog capture proxy --platform openai \
  --upstream https://api.openai.com --listen 127.0.0.1:4780

pixlog capture proxy --platform firefly \
  --upstream https://firefly-api.adobe.io --listen 127.0.0.1:4781

pixlog capture proxy --platform generic \
  --upstream https://provider.example --listen 127.0.0.1:4782
```

API key 继续由原 SDK/环境持有；代理转发 header 但不存储。finalize 后先查看重放计划：

```bash
pixlog reproduce --revision HEAD assets/output.png
```

只有 `exact-request`、相对 path、`POST`/`PUT`/`PATCH` 且 payload 不含
`[REDACTED]` 时才能执行。目标 origin 必须重新显式提供，recipe 中捕获的 host 永远
不会执行；当前认证注入是可选 Bearer token，只能来自用户指定的环境变量：

```bash
pixlog reproduce --revision HEAD --execute \
  --base-url https://provider.example \
  --auth-env PROVIDER_API_TOKEN \
  --response-output response.json \
  assets/output.png
```

响应会被 size-limit、脱敏并写入新的 audit session；非 2xx 也留下记录后返回失败。
这叫 request reproduction，不承诺 provider 返回同一像素。

### Closed Web Tools

```bash
pixlog capture guide browser
export PIXLOG_CAPTURE_TOKEN="$(pixlog capture token)"
pixlog capture serve
```

在 Chromium 中以 unpacked extension 加载 `adapters/browser`，配置 daemon 与 token。
只有用户点击扩展 action 时才采集当前页面可观察字段；输出必须由用户下载并在
`capture finalize` 时指定。adapter 固定标记 `ui-observed` / `provenance-only`。

### Metadata, C2PA, And Inference

```bash
pixlog metadata inspect assets/output.jpg
pixlog metadata import assets/output.jpg

pixlog c2pa verify assets/output.jpg
pixlog c2pa import assets/output.jpg
pixlog c2pa export --output manifest.json assets/output.jpg
pixlog c2pa sign --output signed.jpg --manifest manifest.json assets/output.jpg

pixlog recipe infer before.png after.png
```

C2PA 密码学和 trust store 由官方外部 `c2patool` 负责。PixLog 当前导出公开 action，
有 parent 时增加 `c2pa.opened`；标准 ingredient assertion 仍是后续工作。私有 prompt
或完整 vendor payload 不写入 C2PA。视觉推断只输出证据、候选 operation 与
confidence，永远不可执行。

## Portability And Verification

```bash
pixlog lineage --graph --verify assets/output.png
pixlog lineage --graph --hydrate --verify assets/output.png
```

graph 递归覆盖 input/output、mask、model、workflow、vendor raw payload 和 nested
recipe。pre-push 使用同一个引用闭包，在 Git ref 更新前上传所有对象；`--hydrate`
从 origin media endpoint 补齐缺失对象并逐个校验 SHA-256。

## Remaining Boundaries

- Photoshop UXP 与 Chromium MV3 源码已通过静态语法/manifest 检查，但仍需要在目标
  host 版本中做安装、权限、事件和真实下载流程验收。
- ComfyUI/Firefly 等异步 provider 的通用 job ID 已记录，直接图片和已知 base64 字段
  已 harvest；provider-specific polling、output URL 下载和 cost/usage 归一化仍需继续实现。
- PixLog 不知道 provider 隐藏的 model revision、prompt rewrite、safety pipeline 或
  nondeterministic runtime，因此不会提升 request recipe 的可信度标签。
- 当前不包含透明 TLS interception、移动端捕获、hosted capture service 或浏览器商店
  发布包。

## Design Background

以下内容是原始平台设计依据与更深的目标形态。出现“建议”“可以”“应提供”时表示
设计方向，不代表当前二进制已经实现。

捕获层级从高到低为 Native/API、Embedded metadata、Application history、Visual
inference；每条 recipe 必须同时记录 capture fidelity 和 reproducibility status。

---

一、Photoshop：开发一个 Pixlog UXP 插件

这是最合理的 Photoshop 集成方式。

Photoshop 的 UXP API 可以注册 action.addNotificationListener，接收会修改文档的事件以及对应的 Action Descriptor；另外还有 core.addNotificationListener，用于 UI、操作系统和非文档修改事件。Adobe 提供了大量事件代码，例如 crop、curves、canvas size、layer、save 等。 

架构建议：

Photoshop
   │
   │ UXP notifications
   ▼
Pixlog UXP Plugin
   │ localhost HTTP/WebSocket
   ▼
pixlogd
   │
   ├── journal.sqlite
   ├── recipe objects
   ├── input asset CAS
   └── Git/Pixlog repository

UXP 插件只负责轻量采集，复杂处理交给本地 pixlogd：

Photoshop UI thread
不要计算图片 hash
不要生成 diff
不要上传大文件
只发送事件和必要状态

UXP Manifest 可以声明网络域名和本地文件访问权限，因此插件可以与本机 Pixlog daemon 通信，或者写入插件私有存储。 

Photoshop 捕获流程

1. 文档打开

记录：

{
  "session_id": "ps-session-123",
  "document_id": 84,
  "event": "open",
  "path": "assets/hero.psd",
  "input_oid": "sha256:...",
  "photoshop_version": "27.x",
  "color_profile": "Display P3",
  "width": 2048,
  "height": 2048
}

同时记录：

* PSD 原始文件 hash
* layer tree
* smart object 引用
* linked file
* font
* colour profile
* active layer
* Content Credentials
* XMP/EXIF

2. 用户执行操作

例如用户进行了曲线调整，UXP 收到：

{
  "event": "curves",
  "timestamp": "2026-07-30T22:41:14Z",
  "document_id": 84,
  "descriptor": {
    "_obj": "curves",
    "_target": [...],
    "adjustment": [...]
  }
}

Pixlog 同时保存两份：

raw_action_descriptor
normalized_pixlog_operation

例如：

{
  "schema": "pixlog.operation/v1",
  "type": "color.curves",
  "target": {
    "layer_id": 12,
    "layer_name": "Product"
  },
  "parameters": {
    "channel": "rgb",
    "points": [
      [0, 0],
      [95, 112],
      [255, 255]
    ]
  },
  "vendor": {
    "name": "adobe.photoshop",
    "raw_object": "sha256:..."
  }
}

永远保留原始 Action Descriptor。 标准化 schema 将来可能改变，但原始 descriptor 仍可用于诊断和尝试重放。

如何研究每个 Photoshop 操作的 descriptor

Adobe 的开发者模式提供：

Plugins > Development > Record Action Commands...
Plugins > Development > Record Action Notifications...

前者记录命令，后者同时记录命令和修改通知，结果是 actionJSON。这非常适合建立 Pixlog 的 Photoshop event mapping 测试库。Adobe 还支持从 Actions 面板导出 Action JSON。 

开发阶段可以这样构建测试数据：

fixtures/photoshop/
  crop.json
  curves.json
  generative-fill.json
  remove-background.json
  neural-filter.json
  brush-stroke.json
  layer-mask.json
  smart-object.json

生产插件不要依赖开发模式的 all event listener；应使用已经发现并测试过的 event allow-list。

⸻

二、不要把每次 Photoshop 事件都变成图片版本

例如绘画、拖动滑块或者 transform preview，可能产生大量事件。

建议分成：

Operation events     每个可识别命令
Checkpoints          可恢复的关键状态
Commits              用户明确保存的版本

典型时间线：

open hero.psd
  ├── brush × 147
  ├── curves preview × 23
  ├── curves committed
  ├── generative fill
  ├── transform
  └── save
          ↓
       Pixlog version

可以将连续事件压缩成 transaction：

{
  "type": "paint.stroke_group",
  "count": 147,
  "started_at": "...",
  "ended_at": "...",
  "target_layer": 12,
  "affected_bbox": [120, 84, 782, 963]
}

建议在以下时刻做 checkpoint：

* 保存 PSD
* 导出 PNG/JPEG
* Generative Fill 完成
* flatten
* merge layers
* resize/crop
* smart object 被替换
* 用户点击“Create Pixlog checkpoint”
* 空闲一段时间且修改量较大

⸻

三、Photoshop History Log 可以作为 fallback

Photoshop 本身可以将 History Log 写入：

* 文件 metadata
* 单独的文本文件
* 两者同时

用户之后能在 File Info 的 Photoshop 页面查看历史。 

Pixlog 可以提供：

pixlog adobe configure

引导用户打开：

History Log: enabled
Save Log Items To: Both
Edit Log Items: Detailed

但 History Log 只适合作为补充，因为它通常缺少：

* 完整参数结构
* 输入图片 hash
* mask
* brush 路径
* 插件内部状态
* AI provider request
* 可直接重放的命令数据

推荐优先级：

UXP raw events
    >
Photoshop History Log
    >
before/after visual inference

⸻

四、Photoshop Content Credentials 的定位

Photoshop 可以在导出时附带 Content Credentials，其中包含 attribution 和编辑历史，也可配置为从文档创建开始持续捕获。 

但对 Pixlog 来说，它应是：

可携带、可验证的 provenance 摘要，而不是完整内部 recipe。

合理分工：

Pixlog Recipe
├── 私有、完整
├── prompt / mask / precise parameters
├── Git/CAS 同步
└── 可重放
C2PA Content Credential
├── 签名
├── 随图片移动
├── 公开摘要
└── 验证来源与主要操作

C2PA 定义了 c2pa.actions 和 ingredient relationships，可以表达 opened、placed、removed、transcoded 等来源和操作关系。 

Pixlog 可以在导出时将内部 recipe 降级映射成：

{
  "c2pa.actions": [
    {
      "action": "c2pa.opened",
      "softwareAgent": "Adobe Photoshop"
    },
    {
      "action": "c2pa.color_adjustments"
    },
    {
      "action": "c2pa.placed",
      "ingredient": "reference-image"
    }
  ]
}

敏感 prompt 可以只保存在 Pixlog，不写入公开 C2PA。

⸻

五、Photoshop Generative Fill 怎么 capture

这里需要区分两种情况。

情况 A：Adobe 在 Action Descriptor 中暴露参数

UXP 插件捕获：

* 操作类型
* prompt
* selection/mask bounds
* target layer
* reference image
* generation ID
* 结果 layer
* exposed model information

然后保存成完整 recipe。

情况 B：Adobe 只暴露“发生了 Generative Fill”

那么 Pixlog只能可靠知道：

用户在这个 selection 上执行了生成式修改
产生了这个新 layer
before/after 是这些像素

不能假装知道：

* 内部 seed
* 真实模型版本
* 服务端预处理 prompt
* safety rewrite
* hidden conditioning
* Adobe 内部参数

此时 recipe 应标记：

{
  "type": "ai.inpaint",
  "capture_fidelity": "provenance-only",
  "prompt": "captured-if-exposed",
  "selection_mask_oid": "sha256:...",
  "input_oid": "sha256:...",
  "output_oid": "sha256:...",
  "vendor": "adobe.photoshop.generative-fill",
  "unknown_fields": [
    "seed",
    "exact_model_revision",
    "server_side_prompt_processing"
  ]
}

更强的方案：Pixlog 自己提供生成面板

在 Photoshop 中加入：

Window > Extensions > Pixlog Generate

用户从 Pixlog 面板调用 Firefly API，然后 Pixlog 将结果放回 Photoshop。

这样 Pixlog 可以完整记录：

* API request
* prompt
* seed
* style preset
* structure reference
* style reference
* input image
* mask
* job ID
* API response
* output

Firefly 异步 API 会返回 jobId、状态 URL 和结果 URL；请求中还可以包含 prompt、style、structure 等参数，Firefly API也支持 seed 和 style presets。 

这是比“监听 Adobe 自己的 Generative Fill UI”更可靠、可控的方案。

⸻

六、AI 工具应按集成类型 capture

1. ComfyUI：捕获工作流提交，而不只是读取 PNG

ComfyUI 是最适合 recipe capture 的工具之一。

工作流本身是完整 JSON node graph，包括：

* seed
* steps
* CFG
* sampler
* scheduler
* denoise
* checkpoint
* positive/negative prompt
* 节点连接
* input image
* mask
* ControlNet/LoRA/custom nodes

官方 API workflow 格式明确包含这些节点和参数。 

Pixlog 应提供 ComfyUI server extension：

POST /prompt
     │
     ├── 保存 workflow JSON
     ├── 规范化模型引用
     ├── hash input images
     ├── hash checkpoint / LoRA
     └── 记录 prompt_id

生成完成后：

WebSocket execution events
        +
/history output mapping
        ↓
prompt_id → output image OID

ComfyUI 官方 API使用 /prompt 提交工作流，通过 WebSocket 监控执行，并从 /history 获取输出。 

最终保存：

{
  "source": "comfyui",
  "capture_fidelity": "exact-request",
  "prompt_id": "...",
  "workflow_oid": "sha256:...",
  "models": [
    {
      "path": "flux1-dev.safetensors",
      "sha256": "..."
    }
  ],
  "custom_nodes": [
    {
      "package": "comfyui-controlnet-aux",
      "git_commit": "..."
    }
  ],
  "inputs": ["sha256:..."],
  "outputs": ["sha256:..."]
}

ComfyUI 生成图片通常也可以嵌入完整 workflow metadata，但应该作为 fallback；API提交时捕获更可靠，因为图片 metadata 可能被转换或清除。 

⸻

2. AUTOMATIC1111 / Forge：扩展 hook + API proxy

A1111 会将生成参数保存到 PNG text chunks，JPEG 则可写入 EXIF，并能把这些参数重新载入 UI。 

Pixlog 可以有两个模式：

Extension 模式

安装：

extensions/pixlog-capture/

在以下阶段记录：

before_process
process
postprocess_image
image_saved

捕获：

* txt2img/img2img
* prompt
* negative prompt
* seed/subseed
* sampler
* scheduler
* dimensions
* denoise
* checkpoint hash
* VAE
* LoRA
* ControlNet
* scripts
* extensions
* output path

API proxy 模式

Client
   ↓
pixlog proxy :7861
   ↓
A1111 API :7860

Proxy 保存 request/response，再把请求原样转发。

API proxy 比解析 PNG metadata 更完整，因为 PNG 参数有可能被关闭，某些 extension 的私有参数也未必写入标准 infotext。

⸻

3. 云端 AI API：提供 drop-in SDK wrapper

例如：

from pixlog.providers import openai
result = openai.images.generate(
    model="...",
    prompt="...",
    size="1024x1024"
)

Pixlog wrapper 记录：

provider
endpoint
model name
request parameters
reference image hashes
mask hash
request timestamp
response/request ID
output bytes hash
latency
cost/usage
SDK version

或者允许设置兼容代理：

export OPENAI_BASE_URL=http://127.0.0.1:4777/openai
export FIREFLY_BASE_URL=http://127.0.0.1:4777/firefly

但不要做透明 TLS MITM。更合理的是用户显式配置：

* Pixlog SDK
* Base URL
* provider adapter
* API gateway

同时必须删除：

Authorization
API key
session cookie
signed download query parameters

OpenAI 生成图片目前包含 C2PA metadata 和 SynthID，可用于验证图片是否来自 OpenAI，但这些 provenance 信号不能替代 Pixlog 保存的完整 API request。 

⸻

4. 封闭 Web 工具：只能做到有限捕获

例如没有公开 API、插件接口或可导出 workflow 的 Web 工具。

可选方案按可靠度排序：

官方 API
>
官方导出/job history
>
浏览器扩展
>
用户手动导入
>
图片 metadata
>
AI visual inference

浏览器扩展可以捕获：

* 用户提交的 prompt
* 当前模型名称
* UI 参数
* reference image
* 页面 job ID
* 下载结果

但它有明显问题：

* UI DOM 经常变化
* A/B test 会改变结构
* 某些数据只在内存或内部网络请求中
* 可能涉及平台条款和隐私
* 不能保证捕获服务端隐藏参数
* 用户可能在手机或另一台机器继续编辑

所以这种 adapter 必须标注：

capture_fidelity = ui-observed

而不能标记 exact-request。

⸻

七、统一 Recipe Schema：标准字段 + 原始 vendor payload

不要试图把所有 AI 工具强行压平到一个很小的 schema。

推荐双层结构：

{
  "schema": "pixlog.recipe/v1",
  "normalized": {
    "operation": "ai.inpaint",
    "prompt": "replace the background with a studio backdrop",
    "negative_prompt": null,
    "seed": 12838192,
    "model": {
      "name": "model-name",
      "digest": "sha256:..."
    },
    "inputs": [
      {
        "oid": "sha256:...",
        "role": "source"
      },
      {
        "oid": "sha256:...",
        "role": "mask"
      }
    ],
    "outputs": [
      {
        "oid": "sha256:..."
      }
    ]
  },
  "capture": {
    "adapter": "photoshop-uxp",
    "adapter_version": "0.1.0",
    "fidelity": "exact-command",
    "captured_at": "...",
    "unknown_fields": []
  },
  "vendor": {
    "name": "adobe.photoshop",
    "raw_payload_oid": "sha256:..."
  }
}

标准字段用于：

pixlog recipe diff
pixlog search --model flux
pixlog lineage
pixlog reproduce

原始 payload 用于：

* 将来升级 parser
* 供应商专属 UI
* 调试
* 尽可能重放
* 保留未知字段

⸻

八、Recipe 不等于一定可复现

建议 Pixlog 在 recipe 上显示可复现等级：

等级	例子
Exact	ImageMagick、确定性 Photoshop Action、固定本地模型和完整环境
Best effort	ComfyUI 有 workflow、seed、模型 hash，但 CUDA/kernel 可能不同
Request reproducible	完整云 API request，但供应商后台模型可能升级
Provenance only	Photoshop Generative Fill 只知道发生过操作
Inferred	只有 before/after，由 AI 推测修改内容

即使 seed 相同，也还需要记录：

* 模型文件 hash
* VAE/LoRA/ControlNet hash
* custom node Git commit
* application version
* plugin/extension version
* GPU/runtime
* PyTorch/CUDA
* precision
* provider model revision
* safety或prompt rewrite
* input image精确字节

⸻

九、我建议 Pixlog 的第一批 Capture Adapters

按开发价值排序：

1. pixlog-photoshop
   UXP plugin + localhost daemon
2. pixlog-comfyui
   server extension / workflow execution observer
3. pixlog-a1111
   WebUI extension + PNG metadata importer
4. pixlog-proxy
   OpenAI / Firefly / Stability / Replicate 等 API adapter
5. pixlog-metadata
   PNG chunks / EXIF / XMP / C2PA importer
6. pixlog-browser
   仅用于没有 API 的 Web 工具，明确标记 ui-observed

最值得先实现的是：

Photoshop UXP plugin
        +
ComfyUI adapter
        +
generic AI API proxy

这三项基本覆盖：

* 人工图片编辑
* 本地 AI workflow
* 云端 AI 生成与修改

Pixlog 的核心竞争力最终不是“能够读取多少种 metadata”，而是：

无论图片来自 Photoshop、ComfyUI 还是云端 AI，Pixlog 都能把输入资产、操作事件、参数、输出和 Git commit 连接成同一张 lineage graph。