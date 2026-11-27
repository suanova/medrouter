# 对话页与 DSH 运行时

「对话」把部署在集群里的 DeepSeek Harness（dsh）智能体接到 MedRouter。本文说明
数据流、用户隔离和扩缩容边界；部署参数见 Chart README 的「DSH 智能体（对话页）」。

## 数据流

```
浏览器 ──(会话 Cookie)──> MedRouter /api/chat/*
                              │  X-API-Key + X-DSH-User-Id
                              ▼
                         dsh-api (/api/v1/tasks)
                              │
                     DeepSeek Harness agent
                              │
                   <volume>/workspace/<userId>
```

- 浏览器只与 MedRouter 同源通信；dsh 不需要 Ingress，也不接收终端用户凭据。
- MedRouter 用登录会话确认用户，把 `users.id` 放进 `X-DSH-User-Id`。
- `X-API-Key` 是 MedRouter 与 dsh-api 之间的共享密钥，来自同一个 Secret。
- 产物下载由 MedRouter 代理：`/api/chat/tasks/:id/files/:key` 会携带同一身份向
  dsh 请求字节，再以 `Content-Disposition` 交给浏览器。

## 用户隔离

dsh-api 不信任调用方声明的用户，只认网关注入的身份头，并据此：

| 面 | 行为 |
|---|---|
| 工作目录 | 新任务 `cwd = <workspace>/<userId>`；显式 `cwd` 必须在该目录内 |
| 续会话 | 首轮 cwd 固定；跨用户或未知 `sessionId` 返回 `unknown_session` |
| 任务查询 / 取消 | 任务 owner 不匹配（含无身份调用）一律 404 |
| 产物下载 | 同上，且文件必须仍在该任务 cwd 内 |
| 任务列表 | 先按 owner 过滤，再取最近 50 条 |

MedRouter 侧还有两道检查：

- 提交附件时，路径必须落在 `<DSH_UPLOAD_MOUNT>/<userId>/` 且是真实普通文件；
  `path.Clean` 挡目录穿越，`Lstat` 挡符号链接与其他用户的目录。
- 上传落盘目录按用户拆分为 `<DSH_UPLOAD_ROOT>/uploads/<userId>`，文件名加随机
  前缀避免覆盖；每小时清理过期与超量文件。

对话历史（消息文本）只存在浏览器 localStorage，按用户 id 分组；服务端保存的是
dsh 的会话、工作区与产物。换浏览器看不到历史消息，但用同一账号继续任务时，dsh
仍按 `sessionId` 找到原会话。

## 多副本与 HPA

dsh-api 的任务表和运行中的 agent 句柄在进程内存里。为了扩到多副本：

1. Chart 渲染一个 headless Service `<release>-dsh-headless`。
2. MedRouter 解析它的 Pod IP，用 `fnv32(userId) % len(pods)` 选一个 Pod；提交、
   轮询、取消、下载因此固定到同一副本。
3. Pod 集合变化（扩缩容、重启）会改变哈希环：在途任务失败（内存记录消失）。
   dsh-api 也只在进程内记录 `sessionId → task`，所以换 Pod 后旧 `sessionId` 会
   返回 `unknown_session`；MedRouter 把它转成 `SESSION_EXPIRED`，页面清掉该会话
   并在下一条消息时自动开新会话。工作区文件仍在共享卷上。

因此多副本与附件上传都要求 `ReadWriteMany` 数据卷；Chart 在渲染阶段拒绝
`ReadWriteOnce` 的多副本或带上传的配置。

## 模型与预设

- DSH 只服务用户在 Studio 部署的推理服务，不使用固定上游。dsh 容器启动时向
  MedRouter 的 `/api/chat/runtime/models` 拉取全部 `running` 服务，为每个服务写一条
  pi-ai `openai-completions` 路由；请求体里的 `model` 就是服务名，私有 AI 网关的
  ext_proc 据此设置 `x-ai-eg-model` 并选中后端，客户端不需要自己加头。
- 对话页只列出调用者自己的 running 服务；提交时后端再次校验归属与状态，然后
  发送 `model=<服务名>`、`provider=mr-<服务名>`。其他用户、未运行或不存在的服务
  返回 `MODEL_NOT_AVAILABLE`。
- 推理服务集合变化时，MedRouter reconciler 计算哈希并 patch dsh Deployment 的
  pod-template annotation，触发滚动重启；重启期间在途任务失败。没有 running
  服务时页面提示去 Studio 部署。
- 模型必须经 AI 网关提供 OpenAI 兼容的 `chat/completions` 且支持 tool calling
  （vLLM/SGLang 需要在 runtime profile 中启用 tool parser），否则 agent 无法执行
  工具、生成交付物。
- 同一会话可以换模型，dsh 会在上下文里插入一条模型切换提示。
- 预设（`standard` / `ptc`）决定工具集，会话开始后不能更换；页面在第一轮之后
  锁定下拉。

## 内置技能

镜像内的 `medrouter-skills` 向 dsh 注册四个按需加载的技能：

| 技能 | 内容 |
| --- | --- |
| `slides-design` | 版式网格、字号、配色、原生图表/表格与交付前检查 |
| `medical-slides` | 研究解读、疾病/产品介绍、医学教育课件的结构与证据标注 |
| `medical-writing` | 术语、证据表述、统计口径、引用与隐私、中英双语 |
| `pharma-trials` | 试验分期/终点/分析集、安全性报告、偏倚陷阱与报告规范 |

配合官方 `office-pptx` / `office-docx` / `office-xlsx` 完成实际文件生成。技能由
模型按任务描述自动选择，不需要前端配置。

## 运行预算

- 单轮默认 20 分钟、硬上限 2 小时；`ask_user_question` 10 分钟后按“用户跳过”
  继续，避免没有人在 Web UI 前时任务永久挂起。
- 单副本并发任务默认 8 个；超出的提交由 dsh-api 拒绝。
- Office 技能默认开启渲染后 `read_image` 质检，可用 `dsh.llm.imageInput=off` 或
  `dsh.agent.imageReviewLimit` 限制耗时。
- 产物单文件上限 64 MiB，经 MedRouter 流式代理，不落网关磁盘。

## 停止与失败

- 「停止」调用 `DELETE /api/v1/tasks/:id`，dsh 取消当前轮次；页面只停止轮询，
  不会重新提交。
- `completed` / `max-tokens` / `canceled` 保留回复，页面按完成展示并标注停止；
  `failed` / `timeout` 没有可用回复，显示错误原因。
- 轮询连续 5 次网络失败或收到 404 时，该轮标记失败；网络抖动不会立刻终止任务。
