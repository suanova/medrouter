# medrouter-skills (bundled)

MedRouter 自带的 dsh 技能包，为「对话」补充医药 / 临床与演示文稿方向的专门工作流。
技能不是工具：它们是一组按需加载的说明，模型在任务匹配时通过 `skill` 工具读取。

| 技能 | 用途 |
| --- | --- |
| `slides-design` | 16:9 演示文稿的版式、字号、配色、图表与表格规范，以及交付前的视觉检查清单 |
| `medical-slides` | 临床研究解读、疾病/产品介绍、医学教育课件的结构、证据标注与合规措辞 |
| `medical-writing` | 医学术语、证据表述、统计口径、引用格式、隐私与中英双语写作 |
| `pharma-trials` | 试验分期、终点、分析集、安全性报告、偏倚陷阱与报告规范 |

## 安装方式

镜像构建时装入 profile 种子，运行时由 `overlay.yml` 挂载：

```sh
dsh plugin --profile web add /opt/dsh-plugins/medrouter-skills
```

`overlay.yml` 中的挂载行：

```yaml
- insert:
    - id: medrouter-skills
      name: medrouter-skills
```

## 实现说明

- 与 dsh-api 一样是零依赖的 Cordis 插件：`inject: ['skills']`，
  通过 `ctx.skills.registerProvider` 注册，从 `assets/<skill>/SKILL.md`
  读取带 frontmatter 的说明。
- 不 import `@deepseek-ai/dsh-skill`：本包在 profile 的 `node_modules` 之外，
  直接 import 无法解析；`rank` 使用与 `BUNDLED_SKILL_RANK` 相同的 600。
- 每个 `SKILL.md` 的 frontmatter 只要求 `description`；技能名取自目录名。
- 新增技能：在 `assets/<name>/SKILL.md` 写说明，并把 `<name>` 加进
  `lib/index.js` 的 `SKILL_NAMES`（列表是显式声明的，避免把临时目录当技能）。
