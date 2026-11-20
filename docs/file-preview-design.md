# 文件"打开"（在线预览）设计与大小上限

> 现状：模型/数据集详情有"文件"tab，能按目录浏览、能下载（登录后拿 presigned URL 直连对象存储）。
> 目标：给文件行增加"打开"，在不暴露 bucket/endpoint/凭据、不把整份文件读进内存的前提下预览内容。
> 关联：`docs/model-files-design.md`（文件列表与 S3 接入）、`internal/service/datasetfiles.go`、`web/src/features/files/file-browser-panel.tsx`。

## 1. 结论（先给数）

### 1.1 能打开的文件上限

| 类别 | 判定 | 文件大小上限 | 单次实际传输 | 依据 |
|---|---|---|---|---|
| 文本 / 代码 / 配置 / JSON / JSONL / CSV / Markdown / 日志 | `.json .jsonl .txt .md .csv .tsv .yaml .yml .toml .ini .cfg .conf .log .py .go .sh .js .ts .java .c .cpp .rs .sql .proto .xml .html` | **不设上限**（成本与文件大小无关） | **256 KiB** | 用 `Range: bytes=0-262143` 只取头部；2 GB 的 JSONL 和 2 KB 的 config.json 成本相同 |
| 图片 | `.png .jpg .jpeg .webp .gif .bmp` | **10 MiB** | 全量 | 浏览器必须整图解码；4K PNG ≈ 8 MB，超此量级走下载更合适 |
| PDF | `.pdf` | **32 MiB** | 全量（Range 透传，支持跳页） | 论文 / 数据集说明文档量级；再大内存和首屏都不划算 |
| 其余二进制 | `.safetensors .bin .gguf .pt .pth .onnx .parquet .arrow .npy .npz .zip .tar .gz .h5 .mp4 .wav` | **不可打开** | 0 | 无浏览器原生渲染能力，不假装能看 |
| SVG / HTML / XML | — | 按**文本**预览 | 256 KiB | 见 §7.2：绝不内联渲染，否则是同源 XSS |

### 1.2 下载上限

| 项 | 值 | 说明 |
|---|---|---|
| 单文件 | **50 GiB** | 覆盖 70B 级 safetensors 分片（2–5 GiB）和 GGUF 量化单文件（70B Q4_K_M ≈ 40 GiB）；挡住误配出的 TB 级对象。`0` = 不限 |
| 单用户 24h 累计 | **100 GiB** | presign 模式下服务器不背带宽，但平台不能被当免费 CDN 批量拖库 |
| URL 签发速率 | 10 次/分钟/用户 | 现有的 `common/ratelimit.go` 复用 |
| 大文件 presign TTL | 文件 > 2 GiB 时 **60 分钟**（默认 10 分钟） | 默认 TTL 撑不住大文件断点续传：签名只在请求发起时校验，续传的 Range 请求过期就 403 |
| 未登录 | 不允许 | 保持现状（`RequireAuth`） |
| 目录 | 403/400 | 现状已按文件路径校验 |

**一句话**：打开按"传输窗口"限，不按文件大小限；下载按"单文件 + 每日配额"双重限。

## 2. 为什么这么定

1. **打开的成本必须与文件大小解耦。** 这是整个设计的支点。文本预览走 Range GET，一次最多读 256 KiB，所以一个 10 GB 的 JSONL 和一个 1 KB 的 config.json 对 Pod 的内存、对 S3 的出流量、对首屏时间的开销完全一样。给文本设"文件大小上限"反而会挡住最需要预览的场景（数据集首行长什么样）。
2. **必须整份传输的格式才配大小上限。** 图片和 PDF 是浏览器渲染器的硬要求，只能给一个"值不值得从对象存储拉一遍"的阈值。
3. **下载是产品策略而不是技术约束。** presign 模式下字节直接从 RGW 到用户，不经过 MedRouter。所以 50 GiB 的意义是防误操作和防滥用，不是防 OOM，做成可配置。
4. **诚实的降级。** 超过上限、格式不支持，都要明确告诉用户"为什么不能打开、去看下载"，而不是给一个空面板。

## 3. 架构

```
Browser
  ├─ GET /api/models|datasets/:name/files                  (已有，列表，公开)
  ├─ GET /api/models|datasets/:name/files/preview?ref=&path=   (新) → JSON
  ├─ GET /api/models|datasets/:name/files/content?ref=&path=   (新) → 二进制流，Range
  └─ GET /api/models|datasets/:name/files/download?ref=&path=  (已有) → presigned URL
        │
        ▼
MedRouter API  (复用 FilesService 的 resolver / 路径校验 / singleflight)
  ├─ Head(object) → size / etag        (60s 缓存)
  ├─ 按扩展名分类 → text | image | pdf | unsupported
  ├─ text:  GetObject + Range 0-262143 → ≤256 KiB → JSON
  ├─ image/pdf: GetObject → 流式拷贝到响应（不缓冲）
  └─ download: Head 时顺带做 50 GiB + 配额校验 → presign
        │
        ▼
S3 / Ceph RGW
```

两条新路由都放在 `RequireAuth` 组内（与 `/download` 同级）：预览会真实传输内容，等价于下载，匿名只保留列表能力。

## 4. API 契约

### 4.1 预览元信息 + 文本内容

```
GET /api/{models|datasets}/:name/files/preview?ref=<ref>&path=<relative/path>
```

永远是 JSON，`code:0` 包裹，`data` 形如：

```json
{
  "kind": "text",
  "previewable": true,
  "size": 104857600,
  "content_type": "application/json",
  "encoding": "utf-8",
  "content": "……前 256 KiB……",
  "truncated": true,
  "window_bytes": 262144
}
```

```json
{
  "kind": "image",
  "previewable": true,
  "size": 3145728,
  "content_type": "image/png",
  "content_url": "/medrouter/api/models/qwen3/files/content?ref=v1&path=preview.png"
}
```

```json
{
  "kind": "pdf",
  "previewable": true,
  "size": 8388608,
  "content_type": "application/pdf",
  "content_url": "/medrouter/api/datasets/xxx/files/content?ref=v2&path=README.pdf"
}
```

不可预览：

```json
{
  "kind": "unsupported",
  "previewable": false,
  "reason": "PREVIEW_UNSUPPORTED_FORMAT",
  "size": 5153960755
}
```

新增 reason（与现有 `S3_*` 风格一致，前端负责文案）：

| reason | 含义 | retryable |
|---|---|---|
| `PREVIEW_UNSUPPORTED_FORMAT` | 二进制格式没有浏览器渲染器 | false |
| `PREVIEW_TOO_LARGE` | 超过该类别大小上限 | false |
| `PREVIEW_DISABLED` | 部署关闭了预览 | false |
| `FILE_TOO_LARGE` | 下载超过 50 GiB 单文件上限 | false |
| `DOWNLOAD_QUOTA_EXCEEDED` | 超过 24h 配额 | true（带 reset_at） |

### 4.2 二进制内容

```
GET /api/{models|datasets}/:name/files/content?ref=<ref>&path=<relative/path>
```

- 仅 `kind in (image, pdf)`；其他返回 415。
- 透传 `Range` / `If-None-Match`，响应带 `Accept-Ranges: bytes`、对象的 `ETag`、`Content-Length`。PDF 靠这个才能跳页和流式渲染。
- 响应头：
  - `Content-Type`: 只允许白名单 `image/png|jpeg|gif|webp|bmp`、`application/pdf`，其余一律 `application/octet-stream`；
  - `Content-Disposition: inline; filename*=UTF-8''<basename>`；
  - `X-Content-Type-Options: nosniff`（已有）；
  - `Cache-Control: private, max-age=300`。
- **不做** 302 到 presigned URL：那会暴露 RGW 地址和 bucket，与现有"URI 不出服务端"的约定冲突。

### 4.3 下载的增量校验

在现有 `FilesService.Download` 里，`Head` 已经拿到 `Size`——**加大小和配额校验零额外成本**：

```go
obj, err := s.store.Head(ctx, bucket, key)
if maxBytes > 0 && obj.Size > maxBytes { return FILE_TOO_LARGE }
if !quota.Allow(user, obj.Size)        { return DOWNLOAD_QUOTA_EXCEEDED }
ttl := s.presignTTL
if obj.Size > s.largeFileThreshold { ttl = s.largeFileTTL }
```

配额按"签发即扣减、失败不回滚"（简单、防刷）；审计日志记录 `user/model|dataset/ref/path/size/result`。

## 5. 下载通道：直连对象存储，还是从 MedRouter 转发

### 5.1 现状是直连

`FilesService.Download` 只做三件事：`Head` 校验对象存在 → `PresignGet` 签发短时 URL → 前端 `window.location.assign(url)`。**字节不经 MedRouter**。README 第 319 行已写明这条前提：`S3_ENDPOINT` 必须对最终用户可达。

### 5.2 前提在 US3 上很容易不成立

`internal/s3/client.go` 只有**一个** `Endpoint`，它同时服务于两件诉求相反的事：

| 用途 | 谁访问 | 需要 |
|---|---|---|
| `List` / `Head` / `GetRange` | MedRouter Pod | 内网端点足够，最好只走内网 |
| `PresignGet` 签出来的 URL | **用户的浏览器** | 必须公网可达 |

US3 把这两者分成了两个域名（见 US3《AWS S3 协议支持说明》）：

```
外网:  protocol://s3-<region_code>.<custom_domain>
内网:  protocol://internal.s3-<region_code>.<custom_domain>
```

所以只填一个 `S3_ENDPOINT` 时，要么服务端绕公网、要么用户拿到一个 `http://internal.s3-xxx/...` 的死链接（表现为下载一直失败/超时，且报错发生在浏览器侧，服务端日志干净）。

US3 的另外几条约束，选直连前必须接受：

- **只支持 SigV4**，query 内签名（`X-Amz-Credential`）是支持的 → presign 本身没问题，`client_test.go:166` 已按 US3 风格覆盖。
- **令牌可设 IP 白/黑名单**（《令牌管理》）。一旦给下载用的令牌配了白名单，用户自己的 IP 不在名单里，所有 presign 直连一律 403。这是直连方案的硬约束。
- **ETag 与 AWS 语义不同，且不支持 MD5 校验** → 不要按 MD5 用 ETag（预览的 `If-None-Match` 只能用"同对象同值"，不能当内容哈希）；若 `GetObject` 报 checksum 类错误，把 SDK 的 `ResponseChecksumValidation` 设为 `when_required`。
- **ACL 只有 private / public-read / public-read-write**，没有 `authenticated-read` → 不存在"登录即可读"的对象级权限，可读性只能靠签名表达。

### 5.3 三个选项

| | A. 纯直连（现状） | B. 双端点直连（推荐） | C. 同源代理 |
|---|---|---|---|
| 签约端点 | 单一 `S3_ENDPOINT` | 列表走内网，签名走 `S3_PUBLIC_ENDPOINT` | 不签名，`GetObject` + 流拷贝 |
| 用户流量 | RGW/CDN → 用户 | RGW/CDN → 用户 | Pod → 用户 |
| Pod 带宽 | 0 | 0 | 全量 |
| 暴露 bucket/endpoint | 是（URL 里天然可见） | 是 | 否 |
| 令牌 IP 白名单 | 冲突 | 冲突 | 兼容 |
| 审计精度 | 只知"签发了" | 只知"签发了" | 字节级、可中断 |
| 断点续传 | 依赖 TTL | 依赖 TTL（大文件 60 min） | 原生 Range，最稳 |
| CDN 加速 | 可用 | 可用 | 用不了 |
| 改动量 | 0 | ~20 行（第二个 presigner） | 复用预览的流式端点 |

### 5.4 推荐

**下载走 B（双端点直连），预览走 C（同源代理），把 C 保留成下载的可配置兜底。**

理由：

1. 预览**没有选择**。要同源（不能把 RGW 端点暴露给前端、`X-Frame-Options`/CSP 约束）、要限 256 KiB 窗口、要按类别卡上限，只能在服务端做。所以 `.../files/content` 这条流式端点反正要建。
2. 下载既然这条流式端点已经有了，C 是"顺手"就能开的模式，成本只是在 `FilesService.Download` 里加一个分支。做成 `S3_DOWNLOAD_MODE=presign|proxy`，默认 `presign`，内网部署或令牌设了 IP 白名单时切 `proxy`。
3. 大模型文件（2–40 GiB）应尽量走直连 + 绑定 CDN 域名。US3《域名管理》本身就推荐生产环境绑自定义域名走 CDN。签名用的主机名换成 CDN 域名即可，presign 依然成立——**这是 C 做不到的**，Pod 出带宽在几十 GB × 多用户时是不可接受的。
4. 双端点实现很轻：`s3.New` 增加 `PublicEndpoint`，内部多建一个只用于 `NewPresignClient` 的 core client，`PresignGet` 走它；`List`/`Head`/`GetRange` 继续走内网端点。Chart 加 `datasetFiles.publicEndpoint`。

```go
// s3.Options
PublicEndpoint string // 仅用于签名；空则回落到 Endpoint

core     := newCore(opts.Endpoint)          // List/Head/GetRange
presignC := newCore(firstNonEmpty(opts.PublicEndpoint, opts.Endpoint))
presign  := awss3.NewPresignClient(presignC)
```

### 5.5 选 C 时额外要付的代价

- `main.go:116` 的 `WriteTimeout: 60s` 会掐断慢速大文件，必须按 §6.2 的坑 2 单独放宽。
- 并发下载会吃满 Pod 出带宽，需要按用户限并发（建议 3）并给 `/files/content` 单列指标。
- Pod 重启发起的下载中断，用户要自己重试；直连不会。

## 6. 后端实现要点

### 6.1 复用现有拼装

`FilesService` 已经解决了三件难事，预览直接复用，不要另写一份：

- `StorageResolver.ResolveVersionStorage` → 版本 → S3 bucket/prefix；
- `NormalizeFilePath` → 防越权（拒绝 `..`、绝对路径、反斜杠、控制字符）；
- `listPage` 的缓存 + singleflight 思路 → 抄给 `Head`，避免每次预览多打一次 S3。

新增接口方法（`ObjectStore` 增加一个）：

```go
type ObjectStore interface {
    List(...)
    Head(...)
    PresignGet(...)
    GetRange(ctx context.Context, bucket, key string, off, length int64) (io.ReadCloser, int64, error) // 新增
}
```

`GetRange` 用 aws-sdk 的 `GetObjectInput{Range: aws.String("bytes=0-262143")}`，返回 body 由调用方 `io.CopyN` 限流。

### 6.2 三个现有代码里的坑（必须一起改，否则一上线就断）

1. **`http.Client{Timeout: 5s}` 会掐断流。** `main.go:163` 给 S3 客户端的是 `&http.Client{Timeout: cfg.S3Timeout}`——`Timeout` 是**整个请求含读 body** 的时限。用来做 List/Head 没问题，用来做 32 MiB PDF 的流式传输，5 秒一到 body 直接读出错。预览要用**第二个** HTTP client：`Timeout: 0`、`ResponseHeaderTimeout: 5s`、`Transport` 里设 `IdleConnTimeout`。
2. **`WriteTimeout: 60s` 会掐断慢速下载。** `main.go:116` 的 60 秒写超时对流式响应是硬上限，用户在弱网下拉 PDF 会被截断。用 Go 1.27 的 `http.ResponseController` 在这条路由上 `SetWriteDeadline(流上限)`（例如 300s），或整条 `/files/content` 单独挂一个 `TimeoutHandler`。别直接调大全局，`/status` 那些接口的慢连接保护要留着。
3. **`X-Frame-Options: DENY` 会挡住 PDF 内嵌。** `middleware.SecurityHeaders` 对所有响应设了 `DENY`。两个方案，推荐后者：
   - 给 `/files/content` 覆盖成 `SAMEORIGIN`；
   - **PDF 直接用 `window.open(content_url, '_blank')` 顶层打开**——XFO 不约束顶层导航，而且不引入 pdf.js 这种重依赖。选这个。

### 6.3 内存与并发

- 文本：`io.LimitReader(body, 262144)`，永不读第 262145 个字节；`truncated = size > len(content)`。
- 图片/PDF：`io.Copy` 直出，**不** `io.ReadAll`。
- 全局信号量限制同时进行的预览流（建议 16），超了返回 429 而不是把 Pod 的 goroutine 打满。
- 分类只看扩展名：`ListObjectsV2` 不返回 `Content-Type`，RGW 上传的对象也常年没设置。对象的 `Content-Type` 只在前缀白名单命中但扩展名未知时作为兜底，**永不**用它决定"能不能内联"。

### 6.4 UTF-8 / 编码

`utf8.Valid` 判定；不合法时不要硬塞进 JSON 字符串（会产生替换字符），返回 `encoding:"binary"` + `kind:"unsupported"`，让用户下载。GBK 等中文编码的 `README.txt` 会落到这个分支——可接受，P2 再加 `golang.org/x/text/encoding` 探测。

## 7. 前端

改动集中在 `web/src/features/files/`（模型和数据集共用，`model-files-panel.tsx` / `dataset-files-panel.tsx` 只是薄壳）：

- `types.ts` 增加 `FilePreview`、`FilePreviewParams`；`FileEntry` 不变（列表已经带 `size`，**点开之前就能判断是否可预览，不用多一次请求**）。
- `file-browser-panel.tsx` 文件行加"打开"按钮：
  - `type === 'dir'`：现有行为（进入目录）。
  - text → 弹窗，`<pre>` 等宽展示 + 复制 + 顶部横幅"已截断，仅显示前 256 KiB（共 4.2 GB）"。
  - image → 弹窗 `<img>`。
  - pdf → `window.open(content_url, '_blank')`。
  - unsupported → 按钮置灰 + `title` 说明"该格式不支持在线预览，请下载"。
  - 未登录 → 与 `startDownload` 一致跳登录（`currentSitePath()` 已处理 BASE_PATH）。
- `<pre>` 里**只放文本节点**，不要 `dangerouslySetInnerHTML`——文本类返回的本来就是 JSON 字符串，天然不走 HTML 解析。
- i18n 在 `web/src/i18n/locales/*` 的 `models.files` / `datasets.files` 下补 `open`、`previewTruncated`、`previewUnsupported`、`previewTooLarge`、`contentFailed`。

## 8. 安全

1. **文本永远以 JSON 字符串返回，不以文档形式返回。** 这样"预览一个 `.html`"不可能在站点源上执行脚本——这是整个预览功能最重要的一条不变量。
2. **SVG 不进图片白名单**，走文本预览。SVG 内联渲染等于允许上传者在你站点源上跑 JS（`<svg><script>`），配合同源 cookie 就是账号接管。
3. **对象的 `Content-Type` 不可信。** 只按扩展名映射到固定白名单，其余 `application/octet-stream`。
4. 路径校验全部复用 `NormalizeFilePath`，不新写解析逻辑。
5. 不暴露 bucket / endpoint / 凭据名；`content_url` 是同源相对路径。
6. 预览与下载一样受 `RequireAuth` + 限流 + 审计覆盖。
7. 配额和大小校验必须在**签发 URL / 开始推流之前**，不能只在 UI 上拦。

## 9. 配置项

| env / values | 默认 | 说明 |
|---|---|---|
| `S3_PREVIEW_ENABLED` | `true` | 预览总开关；关掉返回 `PREVIEW_DISABLED` |
| `S3_PREVIEW_TEXT_WINDOW_BYTES` | `262144` | 文本一次读取窗口；上限 1 MiB |
| `S3_PREVIEW_IMAGE_MAX_BYTES` | `10485760` | 图片全量上限（10 MiB） |
| `S3_PREVIEW_PDF_MAX_BYTES` | `33554432` | PDF 全量上限（32 MiB） |
| `S3_PREVIEW_CONCURRENCY` | `16` | 同时进行的预览流数量 |
| `S3_DOWNLOAD_MAX_BYTES` | `53687091200` | 单文件上限（50 GiB）；`0` = 不限 |
| `S3_DOWNLOAD_DAILY_QUOTA_BYTES` | `107374182400` | 单用户 24h 配额（100 GiB）；`0` = 不限 |
| `S3_DOWNLOAD_LARGE_THRESHOLD_BYTES` | `2147483648` | 超过此值用大文件 TTL |
| `S3_DOWNLOAD_LARGE_TTL_SECONDS` | `3600` | 大文件 presign TTL |
| `S3_STREAM_TIMEOUT_SECONDS` | `300` | 单条预览流的写超时 |
| `S3_PUBLIC_ENDPOINT` | 空（回落到 `S3_ENDPOINT`） | 仅用于签发下载 URL 的公网/CDN 域名，见 §5.4 |
| `S3_DOWNLOAD_MODE` | `presign` | `presign`（直连）或 `proxy`（同源流式转发） |
| `S3_DOWNLOAD_CONCURRENCY` | `3` | `proxy` 模式下单用户并发下载数 |

chart 里都挂到现有 `datasetFiles` 块下（这个名字现在同时服务数据集和模型，P3 可考虑改名 `files`）。

## 10. 观测

- 指标：`preview_requests_total{kind,result}`、`preview_bytes_total`、`preview_window_truncated_total`、`download_requests_total{result}`、`download_bytes_bytes`（按 size 估算，presign 模式只能估）。
- 审计：`user, kind, ref, path, bytes, result, ua`。
- 告警：预览 5xx 率、`S3_TIMEOUT` 比例、单用户触发配额的次数。

## 11. 分期

**P0（不依赖真实 S3，全用 fake 可测）**
- `GetRange` 接口 + 实现；`FilesService.Preview` 分类与文本窗口；`/files/preview`、`/files/content` 路由（auth）。
- 下载：`Head` 后的大小校验 + 错误码。
- 前端：打开按钮、文本弹窗、不支持置灰、登录跳转。
- 第二个 HTTP client + `ResponseController` 写超时（§5.2 的坑 1、2）。

**P1**
- 图片/PDF（含 Range 透传、新标签打开）。
- 配额（24h 滑动窗口，复用现有 rate limit 存储风格）。
- i18n、审计、指标。

**P2**
- **结构化预览**（这是模型平台真正的差异化，且改动很小）：
  - `.safetensors`：读前 8 字节得到 header 长度，再读该长度（通常 < 64 KiB）的 JSON，展示 tensor 名/ dtype / shape / 字节数。用户不用下载 5 GB 就知道里面是什么。
  - `.npy`：读 128 字节魔数头。
  - `.jsonl` / 大 CSV：`offset` 参数 + Range，做"看下一段"分页，让 10 GB 数据集可逐段翻。
- GBK 等编码探测；Parquet footer 元数据；预览链接分享（带 HMAC 短时 token，替代裸 cookie）。

## 12. 待确认

1. **下载通道选直连还是代理？** 推荐"双端点直连为主 + `proxy` 兜底"（§5.4）。需要确认：部署里 `S3_ENDPOINT` 现在填的是 US3 的内网域名还是外网域名；下载令牌会不会配 IP 白名单；有没有绑 CDN 域名的计划。
2. **下载上限 50 GiB / 每日 100 GiB 是否符合平台预期？** 两个数都是拍的，改成可配置了，但默认值需要你们对一下真实的模型文件分布（有没有单文件 > 50 GiB 的 GGUF 或整包 tar）。
2. **预览是否允许匿名？** 当前实现与下载一致：preview/content 都在 `RequireAuth` 之后。如果希望公开模型的文件能匿名预览，需要给这两条路由挂一个可选鉴权中间件——注意这等于把内容出口放开给公网。
3. **是否需要审计用户下载内容？** 现在下载没有审计记录，预览如果加审计、下载不加，日志会不平衡。
4. **SVG 是否真的按文本处理？** 我的建议是。如果产品上必须显示 SVG 图标，正确做法是单独用一个无 cookie 的沙箱域名，不能走同源。

## 13. 实现状态

已落地（P0 + 部分 P1）：

| 能力 | 位置 |
|---|---|
| `GetRange`（Range 读取 + `Content-Range` 解析总大小） | `internal/s3/client.go` |
| 双端点签名（列表走内网、签名走 `S3_PUBLIC_ENDPOINT`） | `s3.Options.PublicEndpoint` |
| 独立的流式 HTTP client（无总 `Timeout`） | `main.go:newStreamHTTPClient` |
| `Preview` / `Content` + 扩展名分类 + 大小上限 | `internal/service/datasetfiles.go` |
| 下载大小上限、每日配额、大文件 TTL、`proxy` 模式 | 同上 |
| 共享的预览并发限制与字节配额 | `service.Limiter`、`common.ByteQuota` |
| 四个新路由（models/datasets × preview/content） | `internal/router/router.go` |
| 响应体写超时放宽（`ResponseController`） | `internal/controller/files.go` |
| 前端"打开"按钮、预览弹窗、PDF iframe、i18n | `web/src/features/files/*` |
| Chart / README 配置项 | `helm/medrouter-chart`、`README.md` |

尚未做：

- **审计**（用户、ref、path、字节数）——下载目前也没有，属于同一批工作。
- **结构化预览**（safetensors header、npy、JSONL 分段）——P2，见 §11。
- **按用户区分预览并发**——当前是进程级总并发；`proxy` 下载也会占用预览槽位，16 个并发大文件下载会把预览饿死。
- **配额持久化**——进程重启清零，多副本不共享（与现有 `RateLimiter` 同样的取舍）。
- **`proxy` 模式下的中断续传**——浏览器发起的 Range 已透传，但没有客户端重试逻辑。
