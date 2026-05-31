# Copilot Slash Commands 注入点设计 Spike

> 调研型设计文档，不含实现代码。目标：确定乾坤袋在 GitHub Copilot Slash Commands 层做低打扰、高收益提示的注入方式，并给出 PromptLint / ScopeClarifier / WorkspaceIndexHinter / PlanGuard 四个命令的设计骨架。

## 1. 注入点调研

Copilot 暴露自定义 slash command 的方式有三条，按侵入度排列：

| 注入点 | 形态 | 侵入度 | 是否适合乾坤袋 |
| --- | --- | --- | --- |
| **Prompt Files**（`.github/prompts/*.prompt.md`） | 仓库内 Markdown 文件，Copilot 自动索引，键入 `/` 即出现在补全菜单 | 低（纯文件） | ✅ 首选 |
| **Chat Participant / Extension API**（`@participant /cmd`） | 需独立 VS Code 扩展注册 participant | 高（要发扩展、走 JS runtime） | ⛔ 本期不做 |
| **Context Variables**（`${selection}` / `${file}` / `${input:...}`） | 在 prompt 体内引用，非独立命令 | — | ✅ 作为前两者的参数通道 |

**选择 Prompt Files 的理由**：与项目「Thin Plugin + Sidecar」「先规则后模型」「可降级」一致——命令体只是一段 Markdown，真正逻辑留在 sidecar 的 MCP 工具里；不引入扩展 runtime，sidecar 失败也不影响 Copilot 本体。Extension API 留给后续真正需要拦截/改写输入的场景。

**Prompt File 关键 schema**（来自 VS Code 文档实测）：

```md
---
description: 一句话说明，显示在补全菜单
name: qiankun-prompt-lint        # /后命令名，缺省取文件名
argument-hint: 粘贴你要发给 Copilot 的 prompt
agent: ask | agent | plan        # ask=只读建议，agent=可执行，plan=先出计划
tools: ['qiankun/*']             # 引用 MCP server 全部工具，<server>/* 形式
---
正文为 Markdown 指令，可用：
- 内置变量 ${selection}、${file}、${input:name:placeholder}
- 工具引用 #tool:memory-query
- 相对文件链接
```

**关键约束**：Copilot CLI 当前**不支持**自定义 prompt files（仅内置命令），该能力目前只在 VS Code 扩展生效。因此本设计的交付面 = VS Code；CLI 注入为 W5+ 议题，不在此承诺。

## 2. 交付方式：C1 注入区托管 prompt files

乾坤袋不手写 `.prompt.md`，而是由 sidecar **生成并幂等维护** `.github/prompts/` 下四个文件，复用既有 [C1 注入区协议](c1-injection-zone.md)：每个文件正文用 `<!-- QIANKUN:START/END -->` 包裹乾坤袋托管段，标记外（如用户自定义补充）保持原状。多次运行同输入得到一致输出。

每个命令体只做两件事：(1) 用 `${selection}`/`${input}` 取上下文；(2) 通过 `tools: ['qiankun/*']` 调用 sidecar MCP 工具（现有 `memory-query`、`usage-report`，按需新增）。**logic stays in sidecar**——命令体不内联规则，避免 prompt 本身膨胀成新的 token 黑洞。

## 3. 四个命令设计

| 命令 | agent | 复用的 sidecar 能力 | 高收益点 |
| --- | --- | --- | --- |
| **`/qiankun-prompt-lint`** | ask | `internal/instructions` Linter（report-only） | 发送前对 prompt/instructions 做异味体检：明文凭据、易过期信息、重复段落、缺作用域。直接省下「带着脏上下文反复对话」的 token。 |
| **`/qiankun-scope`**（ScopeClarifier） | ask | `detectIntent` + `memory-query` | 检测查询作用域模糊（无路径/模块），反向给出候选模块与 1–2 个澄清问题，减少 Copilot 因范围发散而召回过多文件。 |
| **`/qiankun-index-hint`**（WorkspaceIndexHinter） | ask | `memory-query`（hybrid retrieval） | 注入本地 Memory Index 的 top-k 高相关文件/片段，替代 Copilot 全量重扫工作区——把召回从「不稳定且昂贵」变成「确定且本地」。 |
| **`/qiankun-plan`**（PlanGuard） | plan | `memory-query` + `internal/memory/commands`（命令发现） | agent 执行大改动前先出计划，附**真实存在**的 build/test/lint 命令（红线：绝不伪造 `pnpm test`），并标出受影响文件，给一道护栏。 |

四者均以 `top_k` clamp（默认 8、上限 20）控制返回体积，与现有 MCP 工具约束一致。

## 4. 低打扰策略

- **用户发起即低打扰**：slash command 是用户主动键入触发，天然没有弹窗/打断；不存在「乾坤袋擅自插话」。这比后台提示更符合「≤2 次/人/天主动提示」红线——主动提示预算留给状态栏，slash 层是按需调用。
- **默认 report-only**：`prompt-lint`/`scope`/`index-hint` 用 `agent: ask`，只给建议不动代码；只有 `plan` 进入 `plan` 模式，仍是先计划后执行。
- **可观测**：每次命令调用经 MCP 工具落 UsageMeter，节省/overhead 进周报；任何「省了多少」都能被还原。
- **可降级**：MCP 不可用时 prompt 体里的 `#tool` 调用失败，Copilot 退回普通对话，命令不阻断主流程。

## 5. 验收口径

- 四个 prompt files 由 sidecar 幂等生成，重复运行 diff 为空（C1 幂等）。
- 每个命令的收益（注入相关文件数、拦截的异味数、避免的重扫）必须能在 UsageMeter / 周报中逐项解释。
- 乾坤袋自身 overhead（命令体 token + MCP 往返）≤ 其带来节省的 5%，否则停下来上报，不靠话术硬凑。
- 命令发现仍只解析真实 scripts/Gradle/Maven 任务，PlanGuard 不得出现伪造命令。

## 6. 非目标

本期不做：Chat Participant / Extension API 注册、对用户输入的自动拦截改写、Copilot CLI 的 slash 注入、把 Linter/检索逻辑内联进 prompt 体、在命令链路里引入 LLM 调用（先规则后模型）。这些待 prompt files 路线验证收益后再评估。
