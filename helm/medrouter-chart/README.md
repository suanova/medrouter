# MedRouter Helm Chart

把 MedRouter 部署到 Kubernetes。MedRouter 是单个 Go 二进制（前端已内嵌），因此本 Chart 只有一个无状态 Deployment 和 Service，不需要 Redis。

数据库有三种模式：

- **`postgres`（默认）**：Chart 内嵌 [CloudNativePG](https://cloudnative-pg.io/) operator 子 Chart，并创建 `Cluster` CR。默认 3 个 PostgreSQL 实例，流复制、自动故障切换、自签名 TLS，可水平扩容 MedRouter。
- **`external`**：连接已有的 PostgreSQL，DSN 由使用者提供。
- **`sqlite`**：数据库文件放在 PVC 的 `/data/medrouter.db`，只能单副本运行，适合开发或小规模部署。

## 前置条件

- Kubernetes 1.29+（CloudNativePG 1.30 子 Chart 的要求）
- Helm 3.8+
- 可用的 MedRouter 镜像。默认 `harbor.isuanova.com/suanova/medrouter:<appVersion>`，由 `.github/workflows/build-image-push.yaml` 构建推送；也可以自行构建并推送到私有仓库后覆盖 `image.repository` / `image.tag`。若 Harbor 项目不公开，需要配置 `imagePullSecrets`。
- `postgres` / `sqlite` 模式需要一个默认 StorageClass，或在安装时显式指定。
- 安装 CloudNativePG operator 需要 cluster 级权限（CRD、ClusterRole、Webhook）。若集群已安装 operator，设置 `database.postgres.operator.install=false`。

## 快速安装（托管 PostgreSQL，默认）

```bash
helm install medrouter ./helm/medrouter-chart \
  --namespace medrouter --create-namespace
```

这会安装：

- CloudNativePG operator（`charts/cloudnative-pg`，operator 1.30.0）；
- 名为 `<release>-postgres` 的 3 实例 PostgreSQL 集群；
- MedRouter Deployment，通过 `<release>-postgres-rw:5432` 连接主库（故障切换后 Service 自动跟随新主库）。

默认没有 Ingress，使用端口转发访问。本地端口要与 `config.PORT` 一致，否则浏览器 Origin 和 `PUBLIC_ORIGIN` 不匹配：

```bash
kubectl -n medrouter port-forward svc/medrouter 3000:80
# 打开 http://localhost:3000
```

### 部署到域名 + 子路径（生产）

对外暴露到域名、TLS 在平台 Gateway 或前置负载均衡器上终止时，这三个值必须显式设置：

```bash
helm upgrade --install medrouter ./helm/medrouter-chart \
  --namespace medrouter --create-namespace \
  --set config.PUBLIC_ORIGIN=https://www.medrouter.com \
  --set config.SESSION_COOKIE_SECURE=true
```

站点地址为 `https://www.medrouter.com`。

- `config.PUBLIC_ORIGIN` 只填浏览器地址的**协议 + 域名**，不带路径。注册、登录等写请求会校验 `Origin`，填错会返回 403 `CROSS_ORIGIN_REQUEST`。
- TLS 在 Gateway/Ingress 之外终止（例如前置负载均衡器）时，chart 推导不出 `https://`，必须像上面这样显式指定。
- `config.SESSION_COOKIE_SECURE=true` 让 session cookie 只通过 https 下发。`PUBLIC_ORIGIN` 为 https 时留空也会自动为 true，显式写出可避免以后改域名时遗漏。

查看数据库状态：

```bash
kubectl -n medrouter get cluster medrouter-postgres
kubectl -n medrouter get pods -l cnpg.io/cluster=medrouter-postgres
```

首次安装会生成 `SESSION_SECRET` 并写入 `medrouter-secret`，生成 PostgreSQL 应用密码并写入 `medrouter-postgres-app-auth` 与 `medrouter-postgres-dsn`。请备份它们。

## PostgreSQL 高可用

### 实例数与资源

```yaml
database:
  mode: postgres
  postgres:
    instances: 3          # >= 2 才有 HA；3 是推荐值
    storage: 10Gi
    storageClassName: ""  # 空表示使用默认 StorageClass
    resources:
      requests: {cpu: 500m, memory: 1Gi}
      limits: {cpu: "2", memory: 4Gi}
    auth:
      database: medrouter
      username: medrouter  # 只能是 [a-z_][a-z0-9_]*
```

`instances=1` 可以运行，但没有故障切换能力。生产环境建议 3 个实例并配合 `database.postgres.storageClassName` 选择支持跨节点访问的存储（`ReadWriteOnce` 即可，CNPG 每个实例使用独立 PVC）。

MedRouter 的 `replicaCount` 与 PostgreSQL 实例数相互独立。多个应用副本会共享同一个 `-rw` 主库 Service；应用启动时执行 `AutoMigrate`，多副本同时启动通常安全，但大版本升级时建议先把 `replicaCount` 缩到 1。

### 自动故障切换

CloudNativePG 自动检测主库故障并提升一个副本。应用只需连接 `-rw` Service，不需要知道当前主库是谁。等待初始化容器会持续重试 `psql "SELECT 1"`，直到新主库可写。

### 备份

```yaml
database:
  postgres:
    backups:
      enabled: true
      schedule: "0 0 2 * * 0"
      method: volumeSnapshot
```

- `volumeSnapshot`：需要 VolumeSnapshot CRD 和 VolumeSnapshotClass。
- `fileArchiving`：需要配置 `barmanObjectStore`（S3/兼容对象存储）。

### PgBouncer

```yaml
database:
  postgres:
    pgBouncer:
      enabled: true
      replicas: 2
```

会额外创建 `Pooler`（`<cluster>-pgbouncer:5432`）。MedRouter 自身仍直连 `-rw`；该池化服务供其它客户端使用。

### 复用已有 CloudNativePG operator

```bash
--set database.postgres.operator.install=false
```

此时 Chart 不安装 operator 子 Chart，但仍渲染 `Cluster` CR，由集群中已有的 operator 接管。若集群没有 operator，安装会因找不到 `postgresql.cnpg.io/v1` CRD 而失败。

### CRD 升级

CloudNativePG 的 CRD 位于子 Chart 的 `crds/` 目录。Helm 只在首次安装时创建 CRD，升级或卸载不会更新/删除它们。升级 operator 时如果 CRD 有变更，需要先手动应用新版 `charts/cloudnative-pg/crds/crds.yaml`。

## 使用 SQLite（开发/小规模）

```bash
helm install medrouter ./helm/medrouter-chart \
  --namespace medrouter --create-namespace \
  --set database.mode=sqlite \
  --set database.postgres.operator.install=false
```

- 数据库文件在 PVC 的 `/data/medrouter.db`；`persistence.retain=true`（默认）会加 `helm.sh/resource-policy: keep`，卸载后保留数据。
- SQLite 是单写者，`replicaCount` 必须为 1；Chart 会拒绝多副本。
- 使用 `Recreate` 更新策略，升级期间会短暂中断。
- 即使使用 SQLite，默认的 `database.postgres.operator.install=true` 仍会安装 CloudNativePG operator；用不到时请显式设为 `false`（如上）。

## 使用外部 PostgreSQL

```bash
helm upgrade --install medrouter ./helm/medrouter-chart \
  --namespace medrouter \
  --set database.mode=external \
  --set database.postgres.operator.install=false \
  --set 'database.external.dsn=postgres://medrouter:password@db.example.com:5432/medrouter?sslmode=require' \
  --set replicaCount=2
```

- DSN 只写入 Secret，不会进入 ConfigMap。
- `database.external.waitForPostgres=true`（默认）会先运行 `psql -tAc "SELECT 1"`，直到数据库可连接。
- 此模式不渲染 PVC，`persistence.*` 被忽略。
- 多副本时自动渲染 PDB，并给 Pod 添加软反亲和（可用 `podAntiAffinity=false` 关闭）。
- 应用启动时会执行 `AutoMigrate`；DSN 用户需要在其 schema 上拥有建表/改表权限。

## 使用 Ingress

```bash
helm upgrade --install medrouter ./helm/medrouter-chart \
  --namespace medrouter \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0]=models.example.com' \
  --set ingress.tls.enabled=true \
  --set ingress.tls.secretName=models-example-tls
```

`PUBLIC_ORIGIN` 会自动推导为 `https://models.example.com`，Session Cookie 自动加 `Secure`。Ingress 不做 rewrite，请求原样进入应用。

### 子路径部署

```bash
helm upgrade --install medrouter ./helm/medrouter-chart \
  --namespace medrouter \
  --set config.BASE_PATH=/medrouter \
  --set ingress.enabled=true \
  --set 'ingress.hosts[0]=example.com' \
  --set ingress.tls.enabled=true \
  --set ingress.tls.secretName=example-tls
```

此时站点地址是 `https://example.com/medrouter`。Ingress 只发布 `/medrouter` 前缀；API、Cookie Path、OAuth 回调和健康检查都会自动带前缀。

> 如果 TLS 在 Ingress 之外终止（例如在负载均衡器或 cert-manager 的 ssl-redirect 上），自动推导出的 `PUBLIC_ORIGIN` 可能是 `http://`。此时必须显式设置 `config.PUBLIC_ORIGIN=https://...`，否则登录会被同源校验拒绝（403 `CROSS_ORIGIN_REQUEST`）。

## 使用 Gateway（CubeStack HTTPRoute，默认开启）

在 CubeStack 环境里，chart 走平台 Gateway（Envoy Gateway 的 `cubestack-gateway`）而非每应用一个 Ingress。因此 chart 默认会创建一条 HTTPRoute，把 `${BASE_PATH}` 发布到平台路径：

```yaml
# values.yaml（默认值）
gateway:
  enabled: true              # 默认 true；ingress 默认 false
  parent:
    name: cubestack-gateway  # 平台 Gateway 名
    namespace: envoy-gateway-system
  hostnames: []              # 空 = 该 Gateway 的所有 hostname
  rootRedirect:
    enabled: true            # "/" -> "/${BASE_PATH}/"
    statusCode: 302
```

- HTTPRoute 挂在 `.Values.gateway.parent`（默认 `cubestack-gateway` / `envoy-gateway-system`），即 `envoy-gateway` 组件提供的平台共享 Gateway。已配置 `config.BASE_PATH=/medrouter` 时，站点地址为 `https://<平台>/medrouter`。
- 与 Ingress 一样不做 rewrite：应用按 `BASE_PATH` 运行，路径原样到达后端 Service。
- `backendRefs` 指向 chart 自渲染的 Service（名为 release name），端口为 `.Values.service.port`。
- `gateway.hostnames` 非空时，`config.PUBLIC_ORIGIN` 留空会自动推导为 `https://<第一个 hostname>`（平台 Gateway 终止 TLS）。如果 Gateway 监听的是纯 HTTP（本地端口转发、测试环境），必须显式设置 `config.PUBLIC_ORIGIN`（https 场景同时设置 `config.SESSION_COOKIE_SECURE=true`），否则登录会被同源校验拒绝（403 `CROSS_ORIGIN_REQUEST`）。
- 该 HTTPRoute 需要 Gateway API CRD（由 `envoy-gateway` 组件提供）。目标集群没有 Gateway API 时，用 `--set gateway.enabled=false` 关闭；此环境改用上面的 Ingress。

### 访问 `/` 自动跳到应用

`config.BASE_PATH=/medrouter` 时应用只在 `/medrouter` 下提供服务，直接打开 `https://<平台>/` 没有路由可匹配。`gateway.rootRedirect`（默认开启）会渲染一条精确匹配 `/` 的 302 规则：

```yaml
- matches:
    - path: {type: Exact, value: /}
  filters:
    - type: RequestRedirect
      requestRedirect:
        path: {type: ReplaceFullPath, replaceFullPath: /medrouter/}
        statusCode: 302
```

目标是带斜杠的规范地址 `/medrouter/`：chart 本来就会把 `/medrouter` 重定向到 `/medrouter/`，直接跳终点少一跳。`config.BASE_PATH` 为空时应用自己就服务 `/`，这条规则不渲染；`--set gateway.rootRedirect.enabled=false` 可关闭。

> ⚠️ 共享 Gateway 注意：不带 `hostnames` 的 HTTPRoute 会匹配父 Gateway 的**所有** hostname。如果平台控制台（或其他应用）也在同一个 Gateway 的 `/` 上，这条规则会把它一起重定向。用 `gateway.hostnames`（例如 `--set 'gateway.hostnames[0]=medrouter.example.com'`）把路由限定到 MedRouter 自己的域名，或者关掉 `rootRedirect`。

### 能不能跳到 `/medrouter` 但地址栏不显示 `medrouter`？

用**重定向**做不到：重定向的定义就是把浏览器地址换成目标地址，地址栏必然出现 `/medrouter`。而且 `/medrouter` 不是纯网关层的修饰，它就是应用的 `BASE_PATH`——前端的 `<base href>`、`/api` 前缀、Cookie Path、OAuth 回调都绑定它，只要 `BASE_PATH=/medrouter`，页面内跳转同样会带出这个前缀。

不想要前缀，只有一种干净做法：**把应用部署在根路径**，即 `config.BASE_PATH=""`。此时 HTTPRoute 发布 `PathPrefix: /`，地址就是 `https://<平台>/`，不需要配置这条重定向。前提是该 Gateway 的 `/` 归 MedRouter；如果必须和其他应用共用同一个域名，路径前缀就是区分应用的路由键，无法隐藏。

（网关侧的 `URLRewrite` 可以让浏览器停在 `/` 而把请求改写到 `/medrouter`，但应用的静态资源、前端路由都以 `BASE_PATH` 为准，两边不一致会出现白屏或 404，chart 不提供这种半改写方案。）

## 邮箱验证与 OAuth

默认关闭邮箱验证，新装环境不需要 SMTP。开启时必须同时配置 SMTP，否则 Chart 在渲染阶段就会失败：

```bash
--set config.EMAIL_VERIFICATION_ENABLED=true \
--set config.SMTP_HOST=smtp.example.com \
--set config.SMTP_PORT=587 \
--set config.SMTP_TLS_MODE=starttls \
--set config.SMTP_USERNAME=no-reply@example.com \
--set secrets.SMTP_PASSWORD=...
```

调试模式（验证码写日志而不是发邮件）在生产环境需要显式放行：

```bash
--set config.EMAIL_DEBUG_MODE=true \
--set config.ALLOW_EMAIL_DEBUG_IN_PRODUCTION=true
```

OAuth 回调地址统一为：

```
${PUBLIC_ORIGIN}${API_PREFIX}/api/oauth/<provider>/callback
```

`config.API_PREFIX` 默认 `/medrouter`，因此默认回调为 `${PUBLIC_ORIGIN}/medrouter/api/oauth/<provider>/callback`。

| 提供商 | 配置项 |
|---|---|
| GitHub | `config.GITHUB_CLIENT_ID` + `secrets.GITHUB_CLIENT_SECRET` |
| Google | `config.GOOGLE_CLIENT_ID` + `secrets.GOOGLE_CLIENT_SECRET` |
| 通用 OIDC | `config.OIDC_ISSUER_URL` / `OIDC_CLIENT_ID` / `OIDC_DISPLAY_NAME` / `OIDC_SCOPES` + `secrets.OIDC_CLIENT_SECRET` |

只有 client ID 和 secret 都配置后，提供商才会出现在登录页。未配置 secret 时对应环境变量不存在，应用会按未启用处理。

## Secret 管理

默认 `secret.create=true`：Chart 创建 `<release>-secret`，空的 `SESSION_SECRET` 会自动生成 48 位随机值；升级时通过 `lookup` 复用已有值，不会因为重新渲染而把所有人登出。其他凭据（SMTP、OAuth、初始管理员密码）不会自动生成。

托管 PostgreSQL 模式还会创建以下两个 Chart 管理的 Secret（无论 `secret.create` 是否为 true）：

| Secret | 内容 | 说明 |
|---|---|---|
| `<cluster>-app-auth` | `username` / `password` | CloudNativePG `bootstrap.initdb.secret`；operator 用它设置数据库 owner 密码 |
| `<release>-postgres-dsn` | `SQL_DSN` | 应用实际使用的连接串，指向 `<cluster>-rw`，`sslmode=require` |

PostgreSQL owner 密码来自 `secrets.POSTGRES_PASSWORD`；为空时生成 24 位随机值，并通过 `lookup` 在升级时复用。密码不能包含空格或 URI 特殊字符（`@ : / ? # [ ] %`），否则 Chart 在渲染阶段报错。

使用外部 Secret 管理（External Secrets、Sealed Secrets、Vault 等）时：

```yaml
secret:
  create: false
  existingSecret: medrouter-credentials
```

该 Secret 至少需要：

| Key | 何时必需 |
|---|---|
| `SESSION_SECRET` | 总是 |
| `SQL_DSN` | `database.mode=external` |
| `SMTP_PASSWORD` | 使用 SMTP 时 |
| `GITHUB_CLIENT_SECRET` / `GOOGLE_CLIENT_SECRET` / `OIDC_CLIENT_SECRET` | 对应 OAuth 提供商 |
| `INITIAL_ADMIN_PASSWORD` | 使用初始化管理员时 |
| `DSH_API_KEY` / `DSH_DEEPSEEK_API_KEY` | `dsh.enabled=true` 时（两个都需要） |

其余 key 可省略。Deployment 对可选 key 使用 `optional: true`；DSH 的两个 key 是必需的，缺失时 Pod 会停在 `CreateContainerConfigError`。托管 PostgreSQL 模式下 `SQL_DSN` 由 Chart 自己生成，不需要放进外部 Secret。`secret.create=true` 时 `DSH_API_KEY` 自动生成，`DSH_DEEPSEEK_API_KEY` 必须显式提供。

## 模型库、数据集库与 RBAC

`/models` 页面直接读取 CubeStack 的 `ModelVersion` 与 `InferenceRuntimeProfile`（`ai.cubestack.io/v1alpha1`），`/datasets` 页面读取 `Dataset`。这些 CRD 是集群级资源，因此 Chart 默认会：

- 创建一个 ServiceAccount（`serviceAccount.create=true`），并让 Pod 挂载其 token；
- 为模型库创建一个只读 ClusterRole / ClusterRoleBinding，仅允许 `list` 上述两种模型资源；
- 为数据集库创建独立的只读 ClusterRole / ClusterRoleBinding，仅允许 `list` `datasets`；
- 通过 ConfigMap 注入 `MODEL_LIBRARY_*` 与 `DATASET_LIBRARY_*`（`ENABLED`、`CACHE_TTL_SECONDS`、`REQUEST_TIMEOUT_SECONDS`）。

两个库的开关与 RBAC 相互独立：关闭数据集库不会影响 `/api/models`，反之亦然。两个 ClusterRoleBinding 都带 `medrouter.io/catalog: models|datasets` 标签，便于按目录分别审计或删除。

```bash
# 默认：创建 SA + 两套 RBAC
helm install medrouter ./helm/medrouter-chart -n medrouter --create-namespace

# 已有 CubeStack 只读 Role，自己绑定：关闭 Chart 的 RBAC
helm install medrouter ./helm/medrouter-chart -n medrouter \
  --set modelLibrary.rbac.create=false \
  --set datasetLibrary.rbac.create=false

# 不使用数据集库：不渲染 Dataset ClusterRole，页面显示 DISABLED
helm install medrouter ./helm/medrouter-chart -n medrouter \
  --set datasetLibrary.enabled=false
```

没有安装 CubeStack CRD 或缺少 RBAC 时，MedRouter 仍能正常启动，`/api/models` / `/api/datasets` 返回 HTTP 200 且 `available:false`，页面按原因显示 `CRD_MISSING` / `FORBIDDEN` 等不可用状态。

## 文件浏览与在线预览（S3）

`/models` 和 `/datasets` 详情页的 **Files** tab 会按 `ModelVersion` / `Dataset` 的 `spec.storage.s3.uri` 列出对象存储里的文件，并支持"打开"（预览）和下载。列表是公开的，**预览和下载需要登录**。

### 前置条件

1. 版本必须是 `storage.strategy: S3` 且 `spec.storage.s3.uri` 非空（如 `s3://medrouter/datasets/pubmedqa/pqa-1.0`）；HostPath / PVC 版本会显示"不可在线浏览"。
2. 在 **release 命名空间**准备一个只读凭据 Secret：

   ```bash
   kubectl -n medrouter create secret generic medrouter-s3-credentials \
     --from-literal=S3_ACCESS_KEY_ID=... \
     --from-literal=S3_SECRET_ACCESS_KEY=...
     # 可选：--from-literal=S3_SESSION_TOKEN=...
   ```

   键名由 `datasetFiles.credentials.accessKeyKey` 等决定，默认就是上面三个。**凭据不能省**：预签名必须有密钥，匿名凭据只能浏览和预览，下载会返回 `S3_UNAVAILABLE`。

3. 凭据至少需要 `list`（浏览）和 `get`（下载/预览）。US3 的令牌支持"授权文件前缀"，可以只授权需要的 bucket/前缀。

### 最少需要设置的值

```yaml
datasetFiles:
  enabled: true
  endpoint: "https://s3-cn-sh2.ufileos.com"     # 集群访问对象存储的端点
  credentials:
    existingSecret: medrouter-s3-credentials
```

`pathStyle` 在设置 `endpoint` 时默认为 `true`，`region` 默认 `us-east-1`。S3 兼容存储通常要改成实际 region，例如 US3 的 `cn-sh2`——region 参与 SigV4 签名，写错会得到 `SignatureDoesNotMatch`。

### 端点：内网与外网

浏览器必须能直接访问签名后的 URL，所以签名所用的主机名和集群访问用的主机名往往是两个：

```yaml
datasetFiles:
  enabled: true
  endpoint: "http://internal.s3-cn-sh2.ufileos.com"   # 集群 → 对象存储：列表、元数据、预览读取
  publicEndpoint: "https://s3-cn-sh2.ufileos.com"     # 浏览器 → 对象存储：仅用于签名下载 URL
  region: cn-sh2
  pathStyle: true
  credentials:
    existingSecret: medrouter-s3-credentials
```

- `endpoint` 只在服务端使用，可以是内网地址；`publicEndpoint` 只参与签名，必须是用户浏览器可达的公网或 CDN 域名。留空则回落到 `endpoint`（单端点部署）。
- `endpoint` 填内网地址却漏配 `publicEndpoint` 时，下载 URL 会指向内网主机：浏览器打不开，服务端日志却一切正常。

### 对象存储不可公网访问时

```yaml
datasetFiles:
  enabled: true
  endpoint: "http://10.66.2.1:30880"
  downloadMode: proxy        # 字节经 MedRouter 同源转发，不再签发直连 URL
```

`proxy` 模式下预览和下载都走同一个 content 端点，代价是 Pod 出带宽。预览和下载共用 `preview.concurrency` 个并发槽位，不要用它承载几十 GB 文件的并发下载。

### 大小上限

| 值 | 默认 | 说明 |
|---|---|---|
| `datasetFiles.preview.enabled` | `true` | 关闭后"打开"按钮消失，接口返回 `PREVIEW_DISABLED` |
| `datasetFiles.preview.textWindowBytes` | `262144` | 文本预览一次读取的窗口；成本与文件大小无关，上限 1 MiB |
| `datasetFiles.preview.imageMaxBytes` | `10485760` | 可预览图片的大小上限 |
| `datasetFiles.preview.pdfMaxBytes` | `33554432` | 可预览 PDF 的大小上限 |
| `datasetFiles.preview.concurrency` | `16` | 同时进行的预览流数量 |
| `datasetFiles.download.maxBytes` | `53687091200` | 单文件下载上限（50 GiB）；`0` 不限 |
| `datasetFiles.download.dailyQuotaBytes` | `107374182400` | 单用户 24h 下载配额（100 GiB）；`0` 不限 |
| `datasetFiles.download.largeThresholdBytes` | `2147483648` | 超过此大小使用更长的下载 URL TTL |
| `datasetFiles.download.largeTTLSeconds` | `3600` | 大文件的下载 URL TTL |

超过 `download.maxBytes` 返回 `FILE_TOO_LARGE`（413），超过配额返回 `DOWNLOAD_QUOTA_EXCEEDED`（429）。配额是进程内内存计数：重启清零，多副本不共享，与 `RateLimiter` 同样的取舍。

### 验证

```bash
# 列表不需要登录；available:true 且有 entries 说明端点与凭据都对
curl -s "https://<site>/medrouter/api/datasets/<name>/files?ref=<ref>" | jq '.data.available, .data.reason'

# 预览和下载需要会话
curl -s -b cookie.txt "https://<site>/medrouter/api/datasets/<name>/files/preview?path=README.md" | jq '.data.kind, .data.previewable'
```

常见 `reason`：`S3_DISABLED`（未启用）、`S3_URI_MISSING`（版本没声明 URI）、`S3_AUTH`（凭据被拒）、`S3_UNAVAILABLE`（端点不可达或签名失败）、`PREVIEW_UNSUPPORTED_FORMAT`（二进制格式）。

### 排查

- **列表报 `S3_AUTH`**：凭据的 bucket 归属不对。US3 要求 bucket 的创建账户与密钥拥有者一致。
- **下载 URL 主机不对**：检查是否漏配 `publicEndpoint`。
- **`SignatureDoesNotMatch`**：`region` 与端点不匹配。
- **配了内网域名却一直 401 / 返回 JSON 错误**：确认它是 S3 协议端点。US3 的内网 S3 域名形如 `internal.s3-<region>.<domain>`；`<bucket>.internal-<region>-01.<domain>` 是 UFile 原生 API，不认 SigV4，不能填进 `endpoint`。

## DSH 智能体（对话页）

`dsh.enabled=true` 会在集群里运行 DeepSeek Harness 运行时（镜像来自本仓库
`images/dsh`），MedRouter 页面顶部出现「对话」入口。浏览器只与 MedRouter 通信：
MedRouter 校验登录会话，把每个请求连同用户 id 转发给 dsh-api；dsh 按用户隔离
工作目录、会话归属、任务查询与产物下载。

### 构建镜像

```bash
docker build -t harbor.isuanova.com/suanova/medrouter-dsh:latest images/dsh
docker push harbor.isuanova.com/suanova/medrouter-dsh:latest
```

镜像内置 dsh 0.2.2-rc.1、dsh-api 插件、Office 技能（python-pptx / python-docx /
openpyxl）与 `medrouter-skills`（演示文稿设计、医学课件、医学写作、临床试验口径），
可以生成 PPT/DOCX/XLSX 并把文件交还给页面下载。技能由模型按任务自动加载，无需
额外配置。

### 安装

```bash
helm upgrade --install medrouter ./helm/medrouter-chart -n medrouter --create-namespace \
  --set dsh.enabled=true \
  --set secrets.DSH_DEEPSEEK_API_KEY=sk-... \
  --set dsh.persistence.storageClassName=<支持 RWX 的 StorageClass>
```

- `secrets.DSH_DEEPSEEK_API_KEY` 只在 dsh Pod 内使用，MedRouter 与浏览器都拿不到。
- `secrets.DSH_API_KEY` 留空时自动生成，MedRouter 与 dsh-api 共用，跨升级保持稳定。
- `dsh.image.repository` / `tag` 指向你推送的镜像；默认 `latest`，生产环境建议固定到 `sha-*` 或版本 tag。

### 调度：只跑在 master

默认 `dsh.scheduling.masterOnly=true`：

- `nodeSelector: {"node-role.kubernetes.io/control-plane": ""}`
- tolerations：`node-role.kubernetes.io/control-plane` 与
  `node-role.kubernetes.io/master` 的 `NoSchedule`

节点标签不是 control-plane 形态时，覆盖 `dsh.scheduling.nodeSelector`；
允许 dsh 跑在任意节点时设 `dsh.scheduling.masterOnly=false`。

### HPA 与多副本

```yaml
dsh:
  autoscaling:
    enabled: true
    minReplicas: 1
    maxReplicas: 3
    targetCPUUtilizationPercentage: 70
```

dsh-api 的任务表与运行中的 agent 在**进程内存**里，会话与工作区在数据卷上。

- MedRouter 通过 headless Service `<release>-dsh-headless` 解析 Pod IP，按用户 id
  哈希固定到同一个 Pod，因此同一账号的提交、轮询、下载都会落到同一副本。
- 缩容或 Pod 重启会让**在途**任务失败（任务记录随进程消失），旧 `sessionId`
  也会失效（dsh-api 只在进程内记录会话）；页面收到 `SESSION_EXPIRED` 后会清掉
  该会话，下一条消息自动开新会话。工作区文件仍在共享卷上。
- 多副本必须使用支持 `ReadWriteMany` 的存储类；Chart 会在渲染阶段拒绝只有
  `ReadWriteOnce` 的多副本配置。
- HPA 需要集群安装 metrics-server；CPU 只是粗略信号，agent 大部分时间在等模型。

### 数据卷与附件

一个 PVC（默认 `<release>-dsh-data`，20Gi，`ReadWriteMany`）同时挂载给两个 Pod：

| Pod | 挂载点 | 内容 |
|---|---|---|
| dsh | `/data` | `DSH_HOME`、`workspace/<userId>`、`uploads/<userId>` |
| MedRouter | `/data/dsh` | 只写 `uploads/<userId>` |

- 附件上传需要 `dsh.uploads.enabled=true`（默认）与 RWX 存储；没有共享存储时设
  `dsh.uploads.enabled=false`，对话与产物下载仍可用。
- 上传扩展名白名单、单文件 25 MiB、每轮最多 10 个；每小时清理超过
  `retentionDays` 或每用户超过 `maxFilesPerUser` 的文件。
- 提交时后端校验附件路径必须落在当前用户目录且文件真实存在，拒绝目录穿越、
  符号链接与其他用户的路径。
- PVC 默认带 `helm.sh/resource-policy: keep`，`helm uninstall` 不会删掉会话、
  工作区与附件；确认不要数据后再手动删除。

### 模型端点与预算

| 值 | 默认 | 说明 |
|---|---|---|
| `dsh.llm.baseURL` | 空 | DeepSeek 官方端点；可指向任意 OpenAI 兼容网关 |
| `dsh.llm.models` | `deepseek-flash` / `deepseek-v4-pro` | 对话页模型下拉的目录；id 必须与 `overlay.yml` 的模型一致，第一项为默认 |
| `dsh.llm.imageInput` | `on` | 设为 `off` 关闭 Office 渲染后看图质检（更快） |
| `dsh.agent.defaultPreset` | `standard` | 默认预设；包含 bash、文件编辑与 Office 技能 |
| `dsh.agent.defaultTimeoutSeconds` | `1200` | 单轮预算 |
| `dsh.agent.maxTimeoutSeconds` | `7200` | 单轮硬上限 |
| `dsh.agent.questionTimeoutSeconds` | `600` | `ask_user_question` 等待人工的时间，超时按跳过继续 |
| `dsh.agent.imageReviewLimit` | `0` | 单轮 `read_image` 次数上限，0 不限制 |
| `dsh.agent.maxConcurrent` | `8` | 每个副本并发任务数 |

### 对话页会话边界

- 对话历史保存在**浏览器 localStorage**（按用户 id 分组），服务端只保存 dsh 的
  会话与工作区；换浏览器看不到历史消息，但输入会话 id 仍可续接。
- 模型下拉来自 `dsh.llm.models`；同一会话内可以换模型（dsh 会插入一条模型切换
  提示）。预设（工具集）在会话开始后锁定，换预设需新建对话。
- 「停止」只是请求 dsh 中止当前轮次并停止本地轮询，不会重新提交任务。
- 产物通过 MedRouter 同源代理下载（`/api/chat/tasks/:id/files/:key`），dsh 不需要
  暴露到集群外。

## 常用配置


| 值 | 默认 | 说明 |
|---|---|---|
| `image.repository` / `image.tag` | `harbor.isuanova.com/suanova/medrouter` / Chart appVersion | 镜像地址 |
| `replicaCount` | `1` | SQLite 模式必须为 1 |
| `serviceAccount.create` | `true` | 为模型库/数据集库创建 ServiceAccount |
| `serviceAccount.name` | 空（使用 fullname） | 指定已有 ServiceAccount |
| `modelLibrary.enabled` | `true` | 是否启用模型库与 token 挂载 |
| `modelLibrary.rbac.create` | `true` | 是否创建模型库只读 ClusterRole / Binding |
| `modelLibrary.cacheTTLSeconds` | `30` | 模型目录快照缓存时间 |
| `modelLibrary.requestTimeoutSeconds` | `5` | 读取 Kubernetes API 的超时 |
| `datasetLibrary.enabled` | `true` | 是否启用数据集库与 token 挂载 |
| `datasetLibrary.rbac.create` | `true` | 是否创建数据集库只读 ClusterRole / Binding |
| `datasetLibrary.cacheTTLSeconds` | `30` | 数据集目录快照缓存时间 |
| `datasetLibrary.requestTimeoutSeconds` | `5` | 读取 Dataset CRD 的超时 |
| `database.mode` | `postgres` | `postgres` / `external` / `sqlite` |
| `database.postgres.operator.install` | `true` | 是否安装内嵌的 CloudNativePG operator |
| `database.postgres.instances` | `3` | PostgreSQL 实例数（>=2 才有 HA） |
| `database.postgres.storage` | `10Gi` | 每个实例的 PVC 容量 |
| `database.postgres.image` | `ghcr.io/cloudnative-pg/postgresql:16.14-system-trixie` | PostgreSQL 镜像 |
| `database.postgres.retain` | `true` | Cluster 加 `helm.sh/resource-policy: keep` |
| `database.external.dsn` | 空 | 外部 PostgreSQL DSN（写入 Secret） |
| `secrets.POSTGRES_PASSWORD` | 空（自动生成） | 托管 PostgreSQL owner 密码 |
| `persistence.enabled` | `true` | SQLite 数据卷；关闭则使用 `emptyDir`（仅供测试） |
| `persistence.size` | `1Gi` | SQLite PVC 容量 |
| `persistence.retain` | `true` | SQLite PVC 卸载后保留 |
| `config.BASE_PATH` | 空 | 子路径前缀 |
| `config.API_PREFIX` | `/medrouter` | API 前缀；实际挂载在 `${API_PREFIX}/api`（默认 `/medrouter/api`）。`/` 表示直接使用 `/api`。设置 `BASE_PATH` 时必须在其之下 |
| `config.PUBLIC_ORIGIN` | 自动 | 浏览器访问源；空=按 Ingress host 或 `gateway.hostnames[0]` 推导，都没有则 `localhost:<PORT>` |
| `config.SESSION_COOKIE_SECURE` | 自动 | 空=按 `PUBLIC_ORIGIN` 是否 https 决定 |
| `config.TRUSTED_PROXIES` | 空 | 反向代理 CIDR，逗号分隔，用于取得真实客户端 IP |
| `config.REGISTER_ENABLED` | `"true"` | 是否开放注册 |
| `config.INITIAL_ADMIN_USERNAME` | 空 | 首次启动创建 root 账号的用户名 |
| `datasetFiles.enabled` | `false` | 启用模型/数据集文件浏览、预览与下载（需配合 `endpoint` 与凭据 Secret） |
| `datasetFiles.endpoint` | 空 | 集群访问对象存储的端点，如 `http://internal.s3-cn-sh2.ufileos.com` |
| `datasetFiles.publicEndpoint` | 空 | 仅用于签名下载 URL 的公网/CDN 域名；空则回落到 `endpoint` |
| `datasetFiles.region` | `us-east-1` | SigV4 region，需与端点一致（US3 如 `cn-sh2`） |
| `datasetFiles.pathStyle` | 设置 `endpoint` 时 `true` | 路径风格寻址 |
| `datasetFiles.credentials.existingSecret` | 空 | 存放 `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` 的 Secret 名 |
| `datasetFiles.downloadMode` | `presign` | `presign` 浏览器直连对象存储；`proxy` 字节经 MedRouter 转发 |
| `datasetFiles.preview.*` / `download.*` | 见上文 | 预览窗口与大小上限、下载上限与配额 |
| `dsh.enabled` | `false` | 运行 DSH 智能体并启用「对话」页 |
| `dsh.image.repository` / `tag` | `harbor.isuanova.com/suanova/medrouter-dsh` / `latest` | DSH 运行时镜像；生产建议固定 sha 或版本 tag |
| `dsh.scheduling.masterOnly` | `true` | 只调度到 master/control-plane 并容忍其污点 |
| `dsh.replicaCount` | `1` | 关闭 HPA 时的副本数 |
| `dsh.autoscaling.enabled` | `false` | 渲染 HPA（需要 metrics-server） |
| `dsh.autoscaling.minReplicas` / `maxReplicas` | `1` / `3` | HPA 范围；多副本需要 RWX 存储 |
| `dsh.persistence.accessModes` | `[ReadWriteMany]` | 多副本或附件上传必须包含 RWX |
| `dsh.persistence.size` / `storageClassName` | `20Gi` / 空 | DSH 数据卷 |
| `dsh.uploads.enabled` | `true` | 对话附件上传（需要共享卷） |
| `dsh.agent.defaultPreset` | `standard` | 默认预设 |
| `dsh.llm.models` | 两个 DeepSeek 模型 | 对话页可选模型；第一项为默认 |
| `secrets.DSH_DEEPSEEK_API_KEY` | 空（`dsh.enabled=true` 时必填） | dsh Pod 使用的模型密钥 |
| `secrets.DSH_API_KEY` | 空（自动生成） | MedRouter 与 dsh-api 的共享密钥 |
| `ingress.enabled` | `false` | 是否创建 Ingress |
| `gateway.enabled` | `true` | 是否创建 Gateway API HTTPRoute（CubeStack 平台） |
| `gateway.parent.name` | `cubestack-gateway` | HTTPRoute 挂载的平台 Gateway 名 |
| `gateway.parent.namespace` | `envoy-gateway-system` | 该 Gateway 所在命名空间 |
| `gateway.hostnames` | `[]` | HTTPRoute 绑定的 hostname；空 = 父 Gateway 的所有 hostname |
| `gateway.rootRedirect.enabled` | `true` | 是否把 `/` 重定向到 `${BASE_PATH}/`（`BASE_PATH` 为空时不渲染） |
| `service.port` | `80` | Service 端口 |
| `pdb.enabled` / `minAvailable` | `true` / `1` | 仅在应用副本数 > 1 时渲染 |
| `resources` | 100m/128Mi → 1/512Mi | MedRouter 容器资源 |
| `cloudnative-pg.image.tag` | `1.30.0` | 内嵌 operator 镜像版本 |
| `envVars` | `[]` | 追加环境变量，例如 `[{name: DB_MAX_OPEN_CONNS, value: "20"}]` |
| `extraVolumes` / `extraVolumeMounts` | `[]` | 追加卷和挂载 |

完整列表和注释见 [`values.yaml`](values.yaml)。

## 健康检查

探针请求 `${BASE_PATH}/healthz`，该接口会执行数据库 `SELECT 1`。启动探针允许最多 5 分钟（`failureThreshold: 60 × 5s`）；托管 PostgreSQL 首次 bootstrap 的时间由 `wait-for-postgres` 初始化容器承担，应用容器在数据库可写后才启动。

## 校验规则

Chart 在渲染阶段会拒绝以下配置：

- `database.mode` 不是 `postgres` / `external` / `sqlite`
- SQLite 模式下 `replicaCount > 1`
- 外部 PostgreSQL 模式 + `secret.create=true` 但 DSN 为空
- 托管 PostgreSQL 的 `auth.username` / `auth.database` 不是合法 PostgreSQL 标识符
- 托管 PostgreSQL 的 `instances < 1`、`storage` 为空
- `secrets.POSTGRES_PASSWORD` 含 URI 特殊字符
- 开启备份但 `method` 非法，或 `fileArchiving` 缺少 `barmanObjectStore`
- 开启 PgBouncer 但 `replicas < 1`
- `secret.create=false` 但没有 `secret.existingSecret`
- `config.BASE_PATH` 非法（含 `..`、空段或非法字符）
- `config.PORT` 非法，布尔配置不是 `true`/`false`
- 开启邮箱验证但没有 SMTP，或生产环境未放行 debug 模式
- 创建 Ingress 但没有 host，或启用 TLS 没有 `secretName`
- `config.extra` 覆盖 Chart 管理的保留键
- 开启模型库或数据集库 RBAC 但没有可用的 ServiceAccount
- `modelLibrary.*` 或 `datasetLibrary.*` 的 `cacheTTLSeconds` / `requestTimeoutSeconds` 小于 1
- `dsh.enabled=true` 但镜像仓库为空、`secrets.DSH_DEEPSEEK_API_KEY` 为空（`secret.create=true` 时）
- `dsh.autoscaling.enabled=true` 但没有任何指标，或 `maxReplicas < minReplicas`
- dsh 多副本或 `dsh.uploads.enabled=true` 但数据卷不包含 `ReadWriteMany`
- `dsh.uploads.enabled=true` 但未启用持久化
- `dsh.agent` 的超时非正或 `maxTimeoutSeconds < defaultTimeoutSeconds`，或 `maxConcurrent < 1`
- `dsh.service.port` 非法、`dsh.uploads.mount` 不是绝对路径

## 升级与卸载

```bash
helm upgrade medrouter ./helm/medrouter-chart -n medrouter
helm uninstall medrouter -n medrouter
```

- SQLite 模式使用 `Recreate` 更新策略，旧 Pod 释放 RWO 卷后新 Pod 才启动，升级期间会短暂中断。
- PostgreSQL 模式使用 `RollingUpdate`（`maxUnavailable=0`），多副本可平滑滚动。
- `database.postgres.retain=true` 时 `helm uninstall` 不会删除 CloudNativePG `Cluster` 及其 PVC。重新安装前如果需要彻底清理：

  ```bash
  kubectl -n medrouter delete cluster medrouter-postgres
  ```

- `persistence.retain=true` 时 SQLite PVC 同样会保留；需要清理时手动删除：

  ```bash
  kubectl -n medrouter delete pvc medrouter-data
  ```

- 自动生成的 `SESSION_SECRET` 在卸载后随 Secret 一起删除；如果数据库保留下来而 Secret 丢失，可以启动，但旧会话全部失效。

## CI 验证

[`.github/workflows/kind-postgres.yaml`](../../.github/workflows/kind-postgres.yaml) 会在 GitHub Actions 中：

- 运行 `helm lint`，渲染托管 PostgreSQL、SQLite、外部 DSN、外部 Secret、备份/PgBouncer 场景，并断言不安全配置会被拒绝；
- 用仓库根目录的 `Dockerfile` 构建镜像并加载到 KinD；
- 以默认的 `database.mode=postgres` 安装 Chart，由 Chart 自带的 CloudNativePG operator 创建 3 实例 PostgreSQL 集群；
- 通过端口转发访问 `/healthz`、`/api/status`，完成注册 → 登录 → 当前用户流程；
- 直接查询 PostgreSQL 主库的 `users` / `user_sessions` 表，确认数据写入的是 CNPG 集群而不是 SQLite；
- 删除主库 Pod 触发自动故障切换，验证新主库产生、集群恢复到 3 个 ready 实例且 MedRouter 继续服务。

镜像发布由另外两个工作流负责，均推送到 `harbor.isuanova.com/suanova/medrouter`：

- [`build-image-pr.yaml`](../../.github/workflows/build-image-pr.yaml)：PR 构建并推送 `pr-<number>` / `sha-<short>`；
- [`build-image-push.yaml`](../../.github/workflows/build-image-push.yaml)：`main`/`master` 与 `v*` 标签构建多架构镜像，推送分支名、`sha-<short>`、语义化版本，默认分支另有 `latest`。

两者使用 `vars.CI_BOT_NAME` 与 `secrets.CI_BOT_PASSWORD` 登录 Harbor；fork/Dependabot PR 没有 secret 时只构建不推送。

## 故障排查

| 现象 | 排查方向 |
|---|---|
| 登录返回 403 `CROSS_ORIGIN_REQUEST` | `PUBLIC_ORIGIN` 与浏览器实际地址（scheme + host + port）不一致 |
| 登录后立即掉线 | `SESSION_COOKIE_SECURE=true` 但通过 http 访问，浏览器丢弃 Cookie |
| Pod 一直 CrashLoop，日志提示数据库不可写 | SQLite 模式的 PVC 权限或 `storageClassName`；容器用户需要对 `/data` 有写权限 |
| Pod 卡在 `wait-for-postgres` | DSN 错误、网络策略、`sslmode` 与证书不匹配；托管模式检查 Cluster 是否 Ready |
| `Cluster` 一直不 Ready | `kubectl -n <ns> describe cluster <name>`、查看 `cnpg.io/cluster` Pod 日志、StorageClass 是否可动态供给 |
| 安装报 `no matches for kind "Cluster"` | 未安装 CRD：保留 `database.postgres.operator.install=true`，或先安装 CloudNativePG operator |
| 健康检查 404 | `config.BASE_PATH` 与探针路径不一致（应使用 Chart 自动生成的路径） |
| 升级后用户全部登出 | `SESSION_SECRET` 被替换（检查是否手工改过 Secret） |
