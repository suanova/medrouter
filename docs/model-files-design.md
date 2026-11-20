# 模型文件列表与 S3 接入设计

> 现状：模型详情只有“版本 + 存储摘要”（`storage.strategy`、是否配置了 URI/凭据），不列出文件。
> 目标：在模型详情里按版本浏览文件（目录、大小、修改时间、下载），文件数据后续从 S3 获取。
> 依据：`docs/medical-model-plaza-strategy.md`（版本/文件是 table stakes）、`model-collection` 的 S3 约定。

## 1. 现有约束（已确认）

- `ModelVersion.spec.storage.strategy` 四选一：`HostPath` / `Dynamic` / `Static` / `S3`。
- S3 块只有两项：`s3.uri`（可选，`s3://bucket/prefix`）和 `s3.credentialsRef.name`。
- `uri` **可以省略**，此时具体路径由 `InferenceService.overrides.modelPath` 按部署提供（例如 `metax-vllm-qwen3.8-27b-s3`）。
- S3 端点不在 CR 上，是部署级参数（profile 的 `s3Endpoint`，示例 `http://10.66.2.1:30880`，Ceph RGW）。
- 凭据 Secret 约定为 `cubestack-system/cubestack-s3-credentials`，`credentials` key 是 AWS shared credentials 文件；engine 挂载为 `/var/run/cubestack/model-credentials`。
- 同一个 `spec.model + spec.version` 可以有多个 `ModelVersion` 注册（例如 cephfs 与 S3 各一份），`ref` 不同，`storage` 是唯一事实来源。
- 当前 API 的 `StorageSummary` **故意不暴露** bucket、URI、路径和凭据名。

## 2. 设计原则

1. **按版本/ref 浏览**：文件属于某个 `ModelVersion`，不是整个模型；UI 先选版本（ref），再列目录。
2. **只对能解析的存储提供列表**：`HostPath`/PVC 不通过 Web 后端直接读节点文件；首期只支持 `S3` 且有明确 `uri` 的 ref。
3. **后端代理 S3，浏览器不接触凭据**：凭据只在服务端；列表返回相对路径；下载用短期 presigned URL（或同源流式代理，二选一）。
4. **分层列目录，不一次性拉全量**：`delimiter="/"` + 分页；缓存 + singleflight；大目录可控。
5. **诚实降级**：S3 未配置、URI 未声明、权限不足、超时都返回明确 reason，UI 给可操作提示，不显示空列表。
6. **公开与受控分开**：公开模型允许匿名浏览；受控/需申请模型先鉴权/授权，下载留审计。

## 3. 架构

```
Browser
  ├─ GET  /api/models/:name/versions/:ref/files?prefix=&limit=&cursor=
  └─ GET  /api/models/:name/versions/:ref/files/download?path=
        │
        ▼
MedRouter API
  ├─ 校验 ref 属于该 model（ModelVersion）
  ├─ 从 spec.storage 解析 S3 {bucket, prefix}
  ├─ S3 凭据：读取 cubestack-s3-credentials（或平台只读凭据）
  ├─ ListObjectsV2(bucket, prefix+subprefix, delimiter="/", max-keys=...)
  ├─ 结果去掉根 prefix，返回相对 name/type/size/etag/last_modified
  └─ 下载：校验 path → 生成 5–15 分钟 presigned GET URL
        │
        ▼
S3 / Ceph RGW（模型 bucket）
```

## 4. API 契约

### 4.1 列表

```
GET /api/models/:name/versions/:ref/files
    ?prefix=subdir/          # 相对该版本根目录，缺省根
    &limit=200               # 默认 200，上限 1000
    &cursor=<opaque>         # 上一页 next_cursor
```

响应（HTTP 200；不可用时 `available:false` + `reason`，沿用现有 catalog 状态风格）：

```json
{
  "data": {
    "available": true,
    "reason": "",
    "storage": "S3",
    "prefix": "subdir/",
    "entries": [
      {"name":"config.json","type":"file","size":1234,"etag":"...","last_modified":"2026-11-01T..."},
      {"name":"tokenizer/","type":"dir"}
    ],
    "truncated": true,
    "next_cursor": "opaque",
    "generated_at": "..."
  }
}
```

- `name` 是相对构建（不含 bucket/根 prefix）；目录以 `/` 结尾，`type:"dir"`。
- **不返回** `s3.uri`、bucket、endpoint、凭据名。
- `reason`：`S3_DISABLED` / `S3_URI_MISSING` / `UNSUPPORTED_STORAGE` / `S3_AUTH` / `S3_UNAVAILABLE` / `TIMEOUT` / `FORBIDDEN` / `NOT_FOUND`。
- `retryable` 与 `message` 同现有 catalog 响应，前端复用 `UnavailableState` 风格。
- 校验：`prefix` 必须是相对路径，拒绝 `..`、绝对路径、反斜杠、控制字符；`prefix` 必须以 `/` 结尾或规范化为目录。
- `ref` 必须是该 model 下的 `ModelVersion.metadata.name`；不同注册（S3 vs PVC）分别浏览。

### 4.2 下载

```
GET /api/models/:name/versions/:ref/files/download?path=config.json
302 → https://<s3-endpoint>/<bucket>/<key>?X-Amz-...
```

- 仅对文件；目录 400。
- `path` 做与列表相同的校验，并确认位于该版本根 prefix 下（防越权）。
- presigned TTL 5–15 分钟；不缓存 302。
- 受控模型：先走鉴权/授权；审计记录 user、model、ref、path、size、时间、结果。
- 若不能暴露 S3 端点（私有网络），改为同源流式代理（支持 Range、Content-Length、ETag），代价是带宽经过 Pod；两种模式做成配置。

## 5. S3 配置与凭据

新增配置（后端 + chart）：

| 配置 | 说明 |
|---|---|
| `S3_ENABLED` | 开关；关闭时列表返回 `S3_DISABLED` |
| `S3_ENDPOINT` | RGW/S3-compatible 端点，例如 `http://10.66.2.1:30880` |
| `S3_REGION` | 签名 region，RGW 可用 `us-east-1` |
| `S3_PATH_STYLE` | RGW 通常 `true` |
| `S3_USE_TLS` | 端点是否 TLS |
| `S3_CREDENTIALS_SECRET` | 默认 `cubestack-s3-credentials`；后端读取 `credentials` key（AWS shared credentials 文件） |
| `S3_LIST_CACHE_TTL` | 列表缓存，默认 15–30s |
| `S3_TIMEOUT` | 单次 ListObjects，默认 5s |
| `S3_PRESIGN_TTL` | 下载 URL 有效期，默认 10m |
| `S3_DOWNLOAD_MODE` | `presign`（默认）或 `proxy` |

**凭据获取方式**（推荐按顺序）：

1. **复用 engine 的 Secret**：`cubestack-s3-credentials`（`cubestack-system`，key `credentials`）。给 MedRouter SA 一个 **resourceName 限定**的 Role：`get` on `secrets`，`resourceNames: [cubestack-s3-credentials]`；后端解析 AWS credentials 文件并在内存缓存（TTL/失败重试）。不让前端接触。
2. 若平台希望更小权限：单独创建一个 **只读** S3 凭据（list/get 指定 bucket），放到 medrouter 命名空间的 Secret 并挂载/读取。engine 的凭据可能有写权限，不建议直接给 Web 后端。
3. 云上环境可用 IAM Role/IRSA，无需静态凭据。

**URI 解析**：

- 用 `spec.storage.s3.uri`（`s3://bucket/prefix`）。`prefix` 为空表示 bucket 根，存在越权/范围过大风险：要求至少一个非空 prefix，或在配置中显式允许根。
- `uri` 缺失时（当前 model-collection 的 S3 示例就是靠 `modelPath` 覆盖）：该 ref 显示“文件位置未声明”，不猜。建议发布流水线给广场可见的 S3 版本补上 `s3.uri`，或新增注解 `medrouter.io/s3-uri` 作为覆盖（后端优先 CR，其次注解）。
- 同一 model/version 的多个注册：按 ref 分别解析；UI 版本选择器标注存储形态（S3 / PVC）。

## 6. 缓存与性能

- 列表缓存：key `(bucket, rootPrefix, subPrefix, cursor)`，TTL 15–30s；singleflight 合并并发请求；错误不长期缓存。
- 目录级分页：`delimiter="/"`，首屏只列一层；用户进入子目录再请求。
- 大目录：`limit` 上限、`truncated` + cursor；不在内存保存全量文件树。
- 前端 React Query：`staleTime` 30s；复用 catalog 重试策略；进入目录时保持上一页骨架，避免闪烁。
- 观测：list 延迟/错误率/缓存命中率；S3 超时与鉴权失败分开计数；下载审计单独记录。

## 7. 前端 UI

在模型详情加 **“文件” tab**（`DetailTabs` 增加一项），内容：

```
版本: [v2.3 (S3) ▾]        存储: S3 · 可浏览
面包屑: / tokenizer/
[目录] tokenizer/           —
[文件] config.json          1.2 MB   2026-11-01   下载
[文件] model.safetensors    4.8 GB   2026-11-01   下载
```

- 版本下拉只列可浏览的 ref（S3 且有 uri）；其他版本标注“PVC/HostPath，暂不支持在线浏览”。
- 目录行可点击进入；面包屑可回退；文件行有大小、修改时间、下载按钮。
- 大列表分页/“加载更多”；当前目录内客户端过滤。
- 状态：加载骨架、空目录、S3 不可用（含 reason + 重试）、未声明位置、无权限（申请访问 CTA）。
- 受控模型：文件列表可展示，下载按钮替换为“申请访问”；已授权用户正常下载。
- 下载：`window.location`/新标签打开 presigned URL；对 proxy 模式走同源流。
- i18n zh/en；移动端表格转卡片列表。
- 不展示 bucket/endpoint/凭据；只展示相对路径。

## 8. 安全

- 凭据：服务端读取，永不进 DTO/日志；资源名限定的 RBAC；优先只读凭据。
- 越权：bucket/root prefix 只来自 CR；用户 `prefix`/`path` 做规范化并限制在根内；拒绝 `..`、绝对路径、空段。
- 模型可见性：公开模型匿名列表；受控/需申请模型鉴权 + 授权；下载审计。
- Presigned：仅 GET、短 TTL、单对象；不签目录；不在日志里打印完整 URL。
- SSRF：endpoint 来自平台配置，不接受用户输入。
- 限流：列表按用户/IP 限流；下载按用户/IP 计量（复用现有中间件）。

## 9. 实施分期

**P0（可先做，不依赖 S3 联调）**
- 后端：`GET .../files` 契约 + `StorageSummary` 扩展（`browsable` 布尔、`storage` 策略）+ 对 `HostPath/PVC/无 URI` 的明确 reason；S3 客户端接口 + fake 测试。
- 前端：“文件” tab、版本选择、目录/文件列表、空/错误/不可用状态；下载按钮先隐藏或置灰。
- Chart：`s3.*` 配置骨架与文档（默认 disabled）。

**P1（S3 联调）**
- 接 RGW：ListObjectsV2、分页、缓存、singleflight、错误映射；凭据读取与 RBAC。
- 公开模型列表可用；presigned 下载。

**P2**
- 受控模型的授权与审计；proxy 下载模式；大目录优化；manifest 索引（发布时生成，读多写少时比实时 list 更稳）；文件预览/校验和。

## 10. 待确认

1. S3 是 Ceph RGW（`10.66.2.1:30880`）吗？全平台共用同一个 endpoint，还是每个模型可能不同？
2. 凭据用 engine 的 `cubestack-s3-credentials`，还是为广场单独建只读凭据？
3. 大部分 S3 注册有没有明确的 `s3.uri`？没有的话，是否接受“文件位置未声明”，还是先补 URI/注解？
4. 下载是否必须首期支持？presigned 直连 RGW 可以接受吗（会暴露 endpoint/bucket）？
5. 文件规模：单版本几十个文件还是上万？决定实时 list + 缓存是否足够，还是需要发布时 manifest。
6. 受控模型的文件可见性：列表公开、下载申请？还是列表也需要授权？
