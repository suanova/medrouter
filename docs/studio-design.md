# 工作室（Studio）设计分析

> 目标：在 MedRouter 里创建和管理三类工作负载——**Notebook**、**推理服务**、**微调**，并且做到"一键部署"。
> 本文只做可行性分析与设计取舍，不含实现。结论基于 2026-11-23 对 kind 集群和共享集群（`106.75.251.248`）的实测。

## 1. 结论

**三种都要支持，但不能同时做，也不该共用一套实现。**

| 模式 | 平台原语 | 现状 | 建议阶段 |
|---|---|---|---|
| Notebook | `DevEnvironment`（`ai.cubestack.io/v1alpha1`） | CRD 与 operator 就绪，kind 上跑通过 | **P0** |
| 推理服务 | `InferenceService` + `InferenceRuntimeProfile` | CRD 就绪，生产有 9 个 profile，但**生产还没有任何 InferenceService 实例** | **P1** |
| 微调 | **没有** | 集群里没有 Kubeflow / Ray / JobSet / Volcano；`tasks`/`taskruns` 是 Agent 巡检任务，不是训练 | **P2，且需要新增能力** |

"一键部署"的本质不是"一个按钮"，而是**把 20 个 CR 字段收敛成一个模板 + 一次选择**。三种模式的"一次选择"不同：

- Notebook：选**规格**（CPU/内存/GPU + 镜像）
- 推理：选**模型**（后端自动匹配 profile）
- 微调：选**模型 + 数据集**（后端套用训练配方）

## 2. 平台已有原语（实测）

### 2.1 DevEnvironment

```yaml
apiVersion: ai.cubestack.io/v1alpha1
kind: DevEnvironment
metadata: {name: mock-jupyter, namespace: project-a}
spec:
  type: jupyter            # jupyter | ssh | vscode
  image: harbor.isuanova.com/suanova/jupyter-minimal:latest
  running: true            # 启停开关
  resources:
    cpu: "2"
    memory: 4Gi
    gpu: {count: 1, vendor: metax, model: MXC550}   # 可选
  storage: {size: 100Gi, mountPath: /home/ubuntu, pvcRetention: retain}
  ports: [{name: jupyter, containerPort: 8888, type: http}]
  runtime:
    command: [...], args: [...], env: [...], sudo: false
  lifecycle:
    idleTimeout: 0         # 空闲自动停止（秒）
status:
  phase: {name: Running}
  endpoints: [{name: jupyter, address: https://dev.cubestack.local/ws/project-a/mock-jupyter}]
  jupyterTokenSecret: {name: mock-jupyter-jupyter-token, namespace: project-a}
  conditions: [Accepted, RouteReady, Scheduled, Ready, Progressing]
```

关键点：

- **启停不需要删除**——`spec.running=false` 即可，`status.lastActivity` 支持空闲超时。
- **入口是自动发布的**，地址在 `status.endpoints[].address`，命名空间与名字都在路径里（`/ws/<ns>/<name>`）。
- **Jupyter 有 token**，放在同命名空间的 Secret 里。要把 token 展示给用户，MedRouter 需要 `get secrets`——这是一个必须显式决定的权限边界。
- `endpoints[].address` 的主机名在 kind 上是 `dev.cubestack.local`（mock）。**生产上的实际域名需要确认**。

### 2.2 InferenceService

```yaml
apiVersion: ai.cubestack.io/v1alpha1
kind: InferenceService
metadata: {name: dd, namespace: default}
spec:
  modelRef: deepseek-v4-flash-w8a8          # ModelVersion 名
  profileRef: metax-sglang-dsv4-pd          # InferenceRuntimeProfile 名
  overrides: {}                             # profile 声明的可覆盖项
  route:
    aigatewayRouter: cubestack-ai-gateway-private   # 或 cubestack-ai-gateway
    modelName: dd
    paths: [/v1/chat/completions, /v1/responses, /v1/messages, /v1/images]
    idleTimeoutSeconds: 300
    timeoutSeconds: 0
```

### 2.3 InferenceRuntimeProfile（20 个，生产 9 个）

```json
{
  "accelerator": {"vendor": "metax", "models": ["MXC500","MXC550","MXC550-PL"]},
  "engine": {"name": "sglang|custom|...", "version": "..."},
  "modelRequirements": {"architectures": ["deepchem"], "quantization": ["fp16"]},
  "endpoint": {"portName": "http", "role": "custom"},
  "assets": [{...entrypoint configmap...}],
  "nodeSelector": "{{ overrides.nodeSelector }}",
  "overrides": [{"name": "gpuCount", "default": "..."}]
}
```

**这就是"一键"的钥匙**：`ModelVersion.spec.architecture` + `.quantization` 与 `modelRequirements` 求交集，就能自动选出 profile 并预填 overrides，用户不需要理解 sglang/vllm。

### 2.4 缺失的能力

- **没有训练原语**：`kubectl get crd | grep -iE "ray|kubeflow|training|pytorch|jobset|volcano"` 为空。
- **`cubepilot-api` 不可用**：平台已有的 `cubepilot-api.cubestack-system:8080` 是可能的复用点（如果它已经封装了创建工作负载），但 Pod 处于 `Pending` 已 10 天，无法作为依赖。

## 3. "一键部署"怎么做

### 3.1 统一模型

三种模式在 MedRouter 侧统一成一个概念：**Workspace（工作台）**。

```
Workspace = 一条 MedRouter 记录 + 一个 CubeStack CR
           = {owner, kind, preset, cr_name, namespace, phase, endpoint}
```

MedRouter 的数据库负责"谁拥有它、能不能看、能不能删"，CR 负责"实际跑什么"。这比用 CR label 做权限更可靠，也避免 MedRouter 每次列表都要扫描集群。

### 3.2 一次点击的完整链路

```
用户点击「一键创建」
  │
  ├─ 1. 校验：登录状态、配额（活跃工作台数、GPU 总数）、名称合法
  ├─ 2. 套模板：preset → CR spec（只有这里需要业务知识）
  ├─ 3. 创建 CR（同步，<1s）
  ├─ 4. 写 workspaces 表（owner、cr_name、phase=Creating）
  └─ 5. 返回 id，前端轮询 /studio/workspaces/:id
          │
          └─ 后端 2s 轮询 CR status → 更新 phase/endpoint
                Ready 后展示「打开」
```

关键是**创建是同步的，就绪是异步的**。CR 创建成功不代表能用：Jupyter 要等 Pod 调度 + 拉镜像，推理要等模型加载（可能几分钟）。UI 必须把 `Accepted → Scheduled → Ready` 的 conditions 翻译成人话。

### 3.3 三种模式的模板

**Notebook**

| 预设 | 规格 | 用途 |
|---|---|---|
| 轻量 | 2C/4Gi，无 GPU | 看数据、写代码 |
| 标准 GPU | 8C/32Gi，1×MXC550 | 单卡实验 |
| 多卡 | 16C/64Gi，4×MXC550 | 训练/大模型推理 |

字段全部来自预设，用户只填名字。`lifecycle.idleTimeout` 默认 2 小时，避免 GPU 长期空转。

**推理服务**（一键的核心）

1. 用户选一个模型版本（来自 `/api/models` 目录）。
2. 后端匹配 profile：
   ```
   candidates = profiles where
       model.architecture ∈ profile.modelRequirements.architectures
     ∧ model.quantization ∈ profile.modelRequirements.quantization
   按 engine 优先级（sglang > vllm > custom）+ 加速卡可用性排序
   ```
3. 用 profile 的 `overrides[].default` 预填，只暴露 2–3 个用户可调项（副本数、GPU 数、idle timeout）。
4. `modelName` 默认 `<model>-<version>` 去重。
5. 创建后等待 `Ready`，入口是 `<aigatewayRouter>/v1/chat/completions`，`model` 字段填 `modelName`。

**微调**（需要新建能力）

没有原语，只有三条路：

| 方案 | 做法 | 代价 |
|---|---|---|
| A. 单机 Job | 训练镜像 + S3 数据集挂载 + 产物回写 S3，结束后注册成新的 `ModelVersion` | 不引入新组件；只支持单机 LoRA/QLoRA |
| B. 装训练算子 | Kubeflow Training Operator / JobSet → `PyTorchJob` | 集群级变更，需要平台团队配合；能做多机 |
| C. 暂缓 | — | 产品缺一角 |

推荐 **A**，并明确"单机、LoRA 级"的边界。产物注册成 `ModelVersion` 是产品亮点——微调完可以直接在推理页一键部署，形成闭环。

## 4. 需要拍板的六个决策

### 4.1 写 CR 还是调平台 API？

`cubepilot-api` 不可用（Pending 10 天），所以**只能直接写 CR**。这意味着 MedRouter 需要新增写权限。后续如果平台 API 恢复，可以替换实现而不动前端。

### 4.2 用户的负载放在哪个命名空间？

| 方案 | 优点 | 缺点 |
|---|---|---|
| 单命名空间 `medrouter-workspace` + owner label | RBAC 简单（一个 Role），配额好算 | 隔离靠 MedRouter 后端；用户绕过 MedRouter 就能互相看到 |
| 每用户命名空间 `ws-<user-id>` | 强隔离、配额按人、审计清晰 | MedRouter 需要能创建 Namespace + 动态 RoleBinding；operator 的 Jupyter 路由按命名空间生成，入口 URL 会变长 |

**推荐单命名空间 + 后端强制 ownership**，理由：用户只能通过 MedRouter 访问，没有 kubectl；平台级隔离不是这一层的职责。留一个 `studio.namespacePerUser` 开关给后期。

### 4.3 配额

单命名空间方案下，必须配 `ResourceQuota`（总 CPU/内存/GPU/PVC），否则一个用户可以把节点吃满。GPU 尤其需要：`requests.nvidia.com/gpu` 或 metax 对应的资源名。

MedRouter 侧再做一层软配额：每用户活跃工作台数（默认 3）、GPU 总数（默认 4）。

### 4.4 凭据边界

要把 Jupyter token 给用户，MedRouter 需要 `get secrets`。三个选项：

1. 给 MedRouter `get secret`（限命名空间）——简单，但扩大了爆炸半径。
2. 只展示 URL，让用户在 Jupyter 里登录（token 由 operator 注入？需要确认 operator 是否支持免 token）。
3. 让 operator 在 `status` 里返回 token（需要平台团队改）。

建议先做 1，并把 Role 限定到 `resourceNames` 无法枚举（token secret 名动态），至少限定在单个命名空间内。

### 4.5 生命周期

- Notebook：`idleTimeout` 默认 2h，可配；停止用 `running=false`，不删除，保留 PVC。
- 推理：`route.idleTimeoutSeconds` 默认 300；无流量自动卸载模型。
- 删除：先删 CR，PVC 按 `pvcRetention` 决定；MedRouter 侧软删除记录保留审计。

### 4.6 谁能用

至少要 "登录用户"。建议再加一层白名单/申请制：GPU 资源稀缺，完全开放会很快耗尽。可以用 `studio.enabled` + 用户角色位实现。

## 5. MedRouter 侧的数据模型

```sql
CREATE TABLE workspaces (
  id           INTEGER PRIMARY KEY,
  owner_id     INTEGER NOT NULL,          -- users.id
  kind         TEXT NOT NULL,             -- notebook | inference | finetune
  name         TEXT NOT NULL,             -- 用户可见名，唯一 per owner
  namespace    TEXT NOT NULL,
  cr_name      TEXT NOT NULL,
  preset       TEXT NOT NULL,
  phase        TEXT NOT NULL,             -- Creating|Running|Stopped|Failed|Deleting
  reason       TEXT,                      -- 从 CR conditions 投影
  endpoint     TEXT,
  model_ref    TEXT,                      -- inference/finetune
  dataset_ref  TEXT,                      -- finetune
  spec_json    TEXT,                      -- 创建时的完整 CR spec，用于重建/复制
  created_at, updated_at, deleted_at
);
```

`phase` 是 CR conditions 的投影，不是真相来源——真相永远是 CR。

## 6. API

```
GET    /api/studio/presets                      # 模板目录（images、规格、可选 profile）
GET    /api/studio/workspaces                   # 我的工作台
POST   /api/studio/workspaces                   # {kind, name, preset, model_ref?, dataset_ref?}
GET    /api/studio/workspaces/:id               # 详情 + 实时 phase
POST   /api/studio/workspaces/:id/start
POST   /api/studio/workspaces/:id/stop
DELETE /api/studio/workspaces/:id
GET    /api/studio/workspaces/:id/endpoint      # 打开前换取一次性入口（token/URL）
```

所有接口要求登录，且只能操作 `owner_id = 当前用户` 的记录。管理员可以看全部（审计）。

## 7. RBAC 变更（chart）

现有：ClusterRole 只读 `modelversions` / `datasets` / `inferenceruntimeprofiles`。

新增（限定在 workspace 命名空间）：

```yaml
kind: Role
rules:
  - apiGroups: ["ai.cubestack.io"]
    resources: ["devenvironments", "inferenceservices"]
    verbs: ["get", "list", "watch", "create", "patch", "update", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: []        # 注意：Jupyter token 名是动态的，无法用 resourceNames
    verbs: ["get"]
```

**不要给集群级写权限**。推理服务如果最终要跨命名空间部署，再单独评估。

## 8. 风险

1. **生产从未跑过 DevEnvironment/InferenceService**（`kubectl get` 都是空的）。第一次部署就是在生产验证 operator 行为，需要预留排查时间。
2. **入口域名未确认**：kind 上 `status.endpoints` 是 `https://dev.cubestack.local/ws/...`；生产需要确认 Gateway 是否正确发布与 TLS。
3. **镜像目录**：目前只知道 `jupyter-minimal:latest`。Notebook 的"规格选择"需要一组经过验证的镜像，否则用户会选到跑不起来的。
4. **GPU 资源紧张**：需要配额 + 白名单，否则第一批用户就会互相抢占。
5. **推理服务的 status 为空**：生产上两个 InferenceService 都没有 status，说明可能从未真正调度过。`Ready` 语义和耗时需要实测。
6. **微调没有原语**：这是三种里唯一需要"新增平台能力"的，风险最高。

## 9. 分期建议

| 阶段 | 内容 | 依赖 | 预估 |
|---|---|---|---|
| P0 | Notebook：预设 → DevEnvironment → 状态轮询 → 入口 | 无新平台能力 | 前端 3 页 + 后端 CRUD + chart RBAC |
| P1 | 推理：模型选择 → profile 自动匹配 → InferenceService → 网关入口 | 确认 gateway 与 profile 生效 | 匹配算法 + 表单收敛 |
| P2 | 微调：单机 Job + LoRA 配方 → 产物注册 ModelVersion | 训练镜像、S3 读写凭据、模型注册流程 | 最大，需单独设计 |

**不建议把三种塞进一个"通用表单"**：字段差异大（GPU、镜像、路由、数据集），通用表单最后会变成"所有字段都暴露"，反而失去一键的意义。共用的只是 shell：列表、创建入口、状态卡、启停删。

## 10. 待确认

1. 生产上 DevEnvironment 的入口域名是什么？`dev.cubestack.local` 显然不是。
2. Jupyter token 是否必须由 MedRouter 读 Secret？operator 能否改成在 status 里下发？
3. 推理服务 `Ready` 后，调用入口是 `aigatewayRouter` 的哪个域名？`modelName` 是否全局唯一？
4. 微调：先做单机 Job 是否可接受（不支持多机）？训练镜像谁来提供？
5. GPU 配额的具体数值与资源名（metax 的 device plugin 资源键）。
6. 工作室是否对所有登录用户开放，还是申请制？

## 14. 微调的数据集兼容性（已实现）

用户点模型卡片的「微调」时，只能看到与该模型任务匹配的数据集。规则有三级来源，优先级从高到低：

1. **`ModelVersion.spec.meta`**：`finetune`（方法，如 `lora` / `none`）与 `tasks`（逗号分隔）。
2. **`ModelVersion.metadata.annotations`**：`medrouter.io/finetune`、`medrouter.io/finetune-tasks`。
   现有集群的 `ModelVersion.spec` 被 ValidatingAdmissionPolicy 锁成不可变（`spec is immutable`），
   所以存量记录只能靠 annotation 表达；新记录的 spec.meta 会被优先读取。
3. **架构兜底表**：只有明确有训练配方的文本架构（mistral / llama / qwen / bert / distilbert / deberta）
   才会被推断为可微调，其余一律报 `NO_RECIPE`，不猜。

数据集侧用 `Dataset.spec.meta.task`（可写），缺失时按格式兜底（Parquet/JSONL → text-qa，
SDF/TSV/CSV → molecule，FASTA → sequence，mmCIF/PDB → structure，H5AD → scrna）。

任务词汇表固定为七个值：

```
text-qa | text-classification | sequence | structure | molecule | scrna | vqa
```

接口：`GET /api/studio/finetune/targets?model=<spec.model>`。返回兼容列表与**不兼容列表及原因**
（`TASK_MISMATCH` / `TASK_UNKNOWN`），UI 默认折叠后者但可展开——数据集"消失"比"被排除"更让人困惑。

启动路径必须重新校验同一规则，UI 过滤只是便利，不是边界。
