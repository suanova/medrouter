# MedRouter 首页设计（P0）

> 依据：`docs/medical-model-plaza-strategy.md`（定位、五支柱、产品结构 §6）
> 现有实现：`web/src/features/home/index.tsx`（目前只有 SiteHeader）、`/api/models`、`/api/datasets`
> 目标：把首页做成“按问题进入 + 证据先于模型”的准入货架入口，而不是模型列表的另一个封面。

## 1. 首页要回答的四个问题

1. **这是什么？** 医疗医药 AI 的准入货架、体检中心、配送网络。
2. **给谁用？** 医院（选型/合规/接入）、药企（尽调/集成/审计）、模型厂商（被看见/被验证/被调用）。
3. **凭什么信？** 医疗模型护照（L0–L4 证据、监管状态、交付形态）+ MedTrust 评测 + 机构/专家署名。
4. **下一步做什么？** 按场景进入 → 看护照 → 申请/评测 → SaaS API 接入。

设计原则：

- **证据先于文件**：卡片先给 `L2 · 二类 · SaaS`，再给模型名和版本。
- **按问题导航**：首页第一层不是“任务/框架”，而是“肺结节筛查 / 病理 / 病历质控 / ADMET”。
- **诚实展示缺失**：护照字段没有就显示“未声明”，不编造评分或背书。
- **一条路径到接入**：每个区块最终都指向“选型 → 核验 → 授权 → API”。

## 2. 页面结构

### 2.1 桌面线框（≥1200px）

```
┌──────────────────────────────────────────────────────────────────────────┐
│ SiteHeader（现有：Models / Datasets / Studio / 语言 / 主题 / 登录）       │
├──────────────────────────────────────────────────────────────────────────┤
│ HERO  7/5                                                                 │
│ ┌────────────────────────────────────┐  ┌──────────────────────────────┐ │
│ │ KICKER  MEDICAL AI MODEL EXCHANGE  │  │ 护照卡（精选模型）           │ │
│ │ H1      先看证据，再选模型。       │  │ [L2] [二类] [SaaS]           │ │
│ │ LEAD    面向医院与药企的…          │  │ 肺结节 CT 辅助检测           │ │
│ │ [ 搜索模型、疾病、科室或厂商    ]  │  │ 肺癌筛查 · 影像科 · v2.3     │ │
│ │ [肺结节筛查][病理][病历质控][ADMET]│  │ MedTrust 82.4（外部回顾）    │ │
│ │ [浏览模型广场] [申请接入]          │  │ 中山医院影像科 背书          │ │
│ └────────────────────────────────────┘  └──────────────────────────────┘ │
│ TRUST STRIP  9 个模型 · 8 个数据集 · L0–L4 证据分级 · API-only · 审计     │
├──────────────────────────────────────────────────────────────────────────┤
│ 按问题进入（4 张场景卡）                                                  │
│ [肺结节筛查] [病理辅助诊断] [病历生成与质控] [药企 ADMET]                 │
├──────────────────────────────────────────────────────────────────────────┤
│ 精选护照模型（3 张卡）                          [查看全部模型 →]          │
│ [护照卡] [护照卡] [护照卡]                                                │
├──────────────────────────────────────────────────────────────────────────┤
│ MedTrust 评测                                     [评测方法] [联合评测]   │
│ ┌ 场景榜/报告预览（有数据时） ─────────────────────────────────────────┐  │
│ │ 场景 | 模型 | 证据 | 综合分 | 报告类型 | →                           │  │
│ └──────────────────────────────────────────────────────────────────────┘  │
│ 无数据时：评测说明（7 维度 / 3 级数据）+ 招募 CTA                          │
├──────────────────────────────────────────────────────────────────────────┤
│ 配套可信数据（3 张数据集卡）                      [浏览数据集 →]          │
├──────────────────────────────────────────────────────────────────────────┤
│ 从选型到上线（4 步）                                                      │
│ 01 选型 → 02 核验与评测 → 03 申请与授权 → 04 API 接入                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 机构与专家（署名/背书；无数据时显示“共建机构招募中”）                     │
├──────────────────────────────────────────────────────────────────────────┤
│ 双 CTA：医院/药企「申请接入」   模型厂商「上架模型」                      │
├──────────────────────────────────────────────────────────────────────────┤
│ Footer（现有样式）：模型 / 数据集 / 评测 / 场景包 / 护照规范 / 隐私 / 协议 │
└──────────────────────────────────────────────────────────────────────────┘
```

### 2.2 移动端（<720px）

顺序：Hero 文案 → 搜索 → 场景 chips（横向滚动）→ CTA → 护照卡 → trust strip → 场景卡（单列）→ 精选模型（单列）→ 评测 → 数据集 → 步骤 → CTA → footer。

- 搜索框与 CTA 全宽，按钮高度 ≥44px。
- 场景 chips 横向滚动，不做换行挤压。
- 护照卡的关键信息（证据/监管/交付）在 2 行内，评分与背书可折行。

## 3. 区块规格

### 3.1 Hero

**目的**：5 秒内说清定位与下一步。

**布局**：`grid-template-columns: minmax(0, 7fr) minmax(320px, 5fr)`，间距 48–64px；Hero 下方 24px 是 trust strip。

**文案**

| 位置 | 中文 | English |
|---|---|---|
| Kicker | `MEDICAL AI MODEL EXCHANGE` | 同左 |
| H1 | 先看证据，再选模型。 | Evidence first. Then the model. |
| Lead | 面向医院与药企的医疗医药 AI 准入货架。每份模型护照可机读、可核验；评测与版本绑定；通过统一 SaaS API 接入业务系统。 | The vetted marketplace for medical AI. Every model passport is machine-readable and verifiable, evaluations are bound to versions, and delivery is a multi-tenant SaaS API. |
| 搜索 placeholder | 搜索模型、疾病、科室或厂商 | Search models, diseases, departments or vendors |
| 主 CTA | 浏览模型广场 | Browse models |
| 次 CTA | 申请接入 | Request access |
| 场景 chips | 肺结节筛查 · 病理辅助诊断 · 病历质控 · 药物 ADMET | Lung nodule screening · Pathology · Chart QA · ADMET |

**交互**

- 搜索提交 → `/models?q=...`。
- 场景 chips → `/models?department=[...]&task=[...]`（使用新的多选数组参数）。
- 主 CTA → `/models`；次 CTA → `/sign-up`（P0），P1 换成线索表单。

**护照卡（Hero 右侧）**

选 `featured` 且证据等级最高的模型；没有 featured 时取最近更新。字段来源见 §6。

```
[L2] [监管状态未声明] [SaaS]
肺结节 CT 辅助检测
肺癌筛查 · 影像科 · v2.3
MedTrust —（评测待接入）
发布者：OpenMedLab · 1 个版本 · 3 个可用运行时
```

- 监管状态、MedTrust 分数、背书在数据缺失时显示为“未声明 / 评测待接入”，不隐藏、不编造。
- 评分与背书有数据时按 `MedTrust 82.4（外部回顾）`、`中山医院影像科 背书` 展示。
- 整卡可点击 → `/models/:id`。

**Trust strip**

`{models} 个模型 · {datasets} 个数据集 · L0–L4 证据分级 · API-only 交付 · 调用可审计`

数字来自 API；API 不可用时不显示数字，只保留能力说明。

### 3.2 按问题进入（场景导航）

**目的**：医院/药企带着问题来，不带着模型名来。

**布局**：4 列；每卡高 180–200px。

**场景配置（P0 静态配置 + 实时计数）**

| 场景 | 过滤条件（AND，维度内 OR） | 一句话 |
|---|---|---|
| 肺结节筛查 | department ∈ {影像, 呼吸}；task ∈ {检测, 分割} | 从 CT 影像到结构化报告，按证据等级选型。 |
| 病理辅助诊断 | department ∈ {病理}；modality ∈ {病理WSI} | WSI 模型、染色归一化与报告结构化。 |
| 病历生成与质控 | modality ∈ {EMR文本}；task ∈ {分类, 检测(NER)} | 脱敏、幻觉评测、租户隔离后再上线。 |
| 药企 ADMET | department ∈ {药学}；task ∈ {药物虚拟筛选} | 性质预测、毒性与 scaffold split 评测。 |

**卡片内容**

```
[icon] 肺结节筛查
从 CT 影像到结构化报告…
3 个模型 · 最高证据 L1 · 2 份评测
→ 进入场景
```

- 计数：客户端对 `/api/models` 按过滤条件计算；最高证据取该场景内最高等级。
- 评测数暂无数据时省略，不写 0。
- 点击 → `/models?department=["影像","呼吸"]&task=["检测","分割"]`。

### 3.3 精选护照模型

**目的**：先展示“证据 + 场景”，再展示模型文件。

**布局**：3 张卡；桌面上每卡 1/3，平板 2 列，手机 1 列。

**卡片信息层级**

```
[L2] [二类] [SaaS]                       ← 证据 / 监管 / 交付
肺结节 CT 辅助检测                        ← 模型名
肺癌筛查 · 影像科 · v2.3                  ← 预期用途 · 科室 · 版本
MedTrust 82.4（外部回顾）                 ← 评测（有则显示）
中山医院影像科 背书                       ← 背书（有则显示）
OpenMedLab · 1 版本 · 3 运行时 · 2 天前更新
```

- 模型描述用 `modelDescription()` 截断为两行。
- 证据徽章复用 `.ml-evidence-tag` 的 L0–L3 颜色。
- 监管状态字段在护照 schema 落地前显示“未声明”；交付形态由 `distributions` 推导：含 `API` → `SaaS`，否则 `下载`。
- 选 3 个：`featured` 优先 → 证据等级 L3 > L2 > L1 > L0 → `updated_at` 降序。

**状态**

- 加载：3 个卡片骨架（复用 `CatalogLoading` 的节奏）。
- 失败：紧凑提示“模型目录暂不可用 · 重试” + “去模型广场”，不阻塞 Hero/场景/步骤。
- 列表为空：隐藏整段。

### 3.4 MedTrust 评测

**目的**：把“评测是商品”放在首屏之下最显眼的位置。

**有数据时（P1，`EvaluationReport` 元数据）**：3–5 行榜单

```
场景            模型              证据   综合分   报告类型      →
肺结节筛查      肺结节 CT 检测     L2     82.4    外部回顾      →
病理分型        病理 WSI 分型      L2     79.1    多中心        →
ADMET           毒性预测           L1     74.6    公开基准      →
```

**无数据时（P0 当前）**：两栏说明 + CTA

- 左：`MedTrust 评测` 标题 + 一句“评测与模型版本绑定；默认只接收脱敏数据。” + 7 个维度（有效性/安全性/公平性/稳健性/效率/合规/人机协作）以 mono 小标签排列。
- 右：`申请联合评测` + `了解评测方法` 两个按钮；注明“首批灯塔报告接入中”。
- 不伪造分数，不用“暂无数据”四个字占满整段。

### 3.5 配套可信数据

**目的**：医疗模型离不开数据；数据集本身也是准入信息。

**布局**：3 张卡。

```
[公开] [已去标识] [伦理：已声明]
AlphaFold 预测蛋白结构库
mmCIF / PDB · v1 · 8.2 万条
publisher · 2 天前更新
```

- 字段：`access`、`deidentification`、`ethics`、`format`、`record_count`、`latest_version`、`updated_at`。
- 缺失字段显示“未声明”，不显示空白。
- CTA：`浏览数据集 → /datasets`。

### 3.6 从选型到上线

**目的**：说明 SaaS 边界与交付路径，降低采购/信息科的顾虑。

**布局**：4 步横向（移动端纵向），mono 数字 `01–04`。

1. **选型**：按疾病/科室/任务/证据等级筛选，先看护照。
2. **核验与评测**：报告与版本绑定；公开基准、医院脱敏数据、药企专有数据三级。
3. **申请与授权**：公开 / 受邀 / 机构内 / 商业分级授权；数据使用协议。
4. **API 接入**：多租户、路由、版本锁定、调用审计；不交付私有化。

底部一行小字：`平台不做诊断、不发监管证书、不替代医师或药企的责任；默认不接收原始患者数据。`

### 3.7 机构与专家

**目的**：医疗采购看署名与背书。

- **有数据时**：机构 logo 行（灰度、统一高度）+ 1 张专家引言卡（姓名 / 职务 / 机构 / 利益冲突声明链接）。
- **P0 无数据时**：一条低调的“共建机构与专家评审团招募中” + `联系我们`，不放占位 logo。

### 3.8 双 CTA

**布局**：2 张等高卡片（蓝底/描边各一）。

- 医院 / 药企：`申请接入`，副文案“预约演示，评估场景与合规要求”。
- 模型厂商：`上架模型`，副文案“提交护照字段，进入核验与评测流程”。

P0 链接到 `/sign-up`；P1 换成线索表单/工单。

### 3.9 Footer

沿用现有 `ml-foot` 样式；新增链接：模型广场 / 数据集 / 评测 / 场景包 / 护照规范 / 关于 / 隐私政策 / 用户协议。保留 `© 2026 MedRouter` 与数据来源说明。

## 4. 视觉系统

沿用现有 tokens（`web/src/styles/theme.css`），不引入新色板。

- **字体**：Bricolage Grotesque（正文/标题）+ JetBrains Mono（kicker、数字、徽章）。
- **主色**：`--primary`（oklch 0.58 0.18 255）；橙色仅用于品牌标记与必要的强调，不铺大面积。
- **证据色**：复用 `.ml-evidence-tag`：L0 红、L1 琥珀、L2 绿、L3 teal。L4 若落地，用品牌蓝加深一档。
- **容器**：`max-width: 1360px`、`padding-inline: 28px`（与 SiteHeader 一致）。
- **栅格**：12 列；Hero 7/5；场景 4 列；模型/数据集 3 列；间距 16–24px。
- **圆角/边框**：12px 圆角、1px 边框、浅阴影（仅浮层卡片）。
- **字号**：H1 `clamp(2.6rem, 5vw, 4.4rem)`；Lead `1.05–1.15rem`；区块标题 `1.6–2rem`；mono 标签 `10.5–12px`，字距 `.09em`。
- **留白**：区块上下 72–96px（桌面）/ 48px（移动）。
- **动效**：首屏 fade-up 200–300ms，尊重 `prefers-reduced-motion`；不做视差。
- **暗色**：直接使用 `.dark` tokens，护照卡与评测表需检查对比度。

## 5. 响应式

| 断点 | Hero | 场景 | 模型/数据集 | 步骤 |
|---|---|---|---|---|
| ≥1200 | 7/5 | 4 列 | 3 列 | 4 列 |
| 900–1199 | 6/6 或上下 | 2×2 | 2–3 列 | 2×2 |
| 720–899 | 上下 | 2 列 | 2 列 | 2×2 |
| <720 | 单列 | 横向滚动 chips / 单列卡 | 单列 | 纵向 |

## 6. 数据与状态

### 6.1 数据来源（现有）

- `GET /api/models` → `total`、`version_total`、`profile_total`、`models[]`（含 `evidence`、`license`、`departments`、`modalities`、`tasks`、`distributions`、`featured`、`latest_version`、`compatible_profile_count`、`updated_at`）。
- `GET /api/datasets` → `total`、`datasets[]`（含 `access`、`deidentification`、`ethics`、`format`、`record_count`、`latest_version`、`updated_at`）。
- 两个请求复用新加的 catalog 重试策略（3 次，1s/2s/4s 退避）。

### 6.2 缺失字段处理

| 字段 | 现状 | 首页表现 |
|---|---|---|
| 监管状态（R0–R3） | 无 | `监管状态未声明` |
| MedTrust 综合分 | 无 | `评测待接入` |
| 机构/专家背书 | 无 | 不显示该行；区块显示“招募中” |
| 场景包/评测报告 | 无 | 场景卡用过滤条件；评测区用说明 + CTA |

### 6.3 页面状态

- **加载**：Hero 立即可见；模型/数据集区显示骨架，避免布局跳动。
- **API 失败**：Hero、场景、步骤、CTA 仍然可用；精选模型区显示紧凑错误 + 重试；数字从 trust strip 移除。
- **stale**：复用 `NoticeBar` 的 warning 样式。
- **空列表**：隐藏对应区块，不显示空卡片。

### 6.4 国际化

所有文案进 `web/src/i18n/locales/{zh,en}.json` 的 `home.*`：

```
home.hero.kicker / title / lead / search / cta.browse / cta.access
home.hero.chips.lung / pathology / chart / admet
home.trust.models / datasets / evidence / delivery / audit
home.scenes.title / items.<id>.title / description / enter
home.models.title / subtitle / viewAll
home.evaluations.title / lead / dimensions.* / cta.*
home.datasets.title / viewAll / access.public|request|restricted / fields.*
home.delivery.title / steps.<n>.title|text / disclaimer
home.orgs.title / recruiting / contact
home.cta.hospital.title|text|button / vendor.title|text|button
home.footer.links.*
```

## 7. 实现拆解

### 7.1 文件

```
web/src/features/home/
  home-page.tsx          # 组装区块，替换现有 HomePage
  home.css               # 首页局部样式（复用 ml-* tokens/类）
  scenes.ts              # 场景配置：id、过滤条件、链接
  select.ts              # 精选模型、证据排序、场景计数（纯函数 + 单测）
  sections/
    hero.tsx
    scene-grid.tsx
    featured-models.tsx
    evaluations.tsx
    dataset-strip.tsx
    delivery-steps.tsx
    org-band.tsx
    cta-band.tsx
```

- 路由：`web/src/routes/index.tsx` 的 `HomePage` 改为引用新页面。
- 复用：`SiteHeader`、`ModelCard`（新增护照变体或新组件）、`CatalogLoading`、`UnavailableState`、`NoticeBar`、`formatRelativeTime`、`initials`。
- 新增纯函数测试：`select.test.ts`（featured/evidence 排序、场景计数、缺失字段）。

### 7.2 验收标准（P0）

- [ ] Hero 在 API 失败时仍完整可用；模型区显示可重试错误。
- [ ] 场景卡计数与 `/models?filters` 的结果一致（多选 OR/AND 语义）。
- [ ] 护照卡只显示真实字段，缺失显示“未声明/评测待接入”，不出现假分数/假背书。
- [ ] 中英文完整，暗色模式对比度达标。
- [ ] 首页首屏不新增图片请求；静态资源请求数与现有模型页持平或更少。
- [ ] 移动端 360px 宽度无横向溢出。

### 7.3 分期

- **P0（本次可实现）**：Hero、场景导航、精选模型、数据集、交付步骤、双 CTA、Footer；评测区说明态；机构区招募态。
- **P1**：`ModelPassport` 字段、`EvaluationReport` 榜单、机构/专家数据、场景包页面、线索表单、首页埋点。

## 8. 待确认

1. 四个场景是否就是 P0 的正式场景集？是否需要加“影像报告生成”“临床试验招募”？
2. 评测区在无数据时保留说明 + 招募 CTA（推荐），还是先隐藏？
3. 次 CTA「申请接入」在 P0 指向 `/sign-up` 是否可接受，还是先接一个线索表单？
4. 首页是否需要展示“最新上架”时间线（P1）？
