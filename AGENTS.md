# AGENTS.md

本文件给当前项目中的 AI Agent 使用。目标是让 Agent 在实现乾坤袋时保持上下文节省、低打扰、可观测和工程可验证。

## 项目定位

乾坤袋是 Copilot 外侧的本地上下文治理层，不替代 Copilot，不自建完整 AI runtime。

核心目标：

- 减少重复、噪声、过期或无关上下文进入模型。
- 提升 Vue、Angular、React、Spring Boot 项目中的关键文件召回质量。
- 让 UsageMeter 证明 token 节省、缓存收益、sidecar overhead 和净收益。
- 以 JetBrains / IntelliJ IDEA 插件首发，核心逻辑放在本地 `qiankun-mcpd` sidecar。

## 技术方向

优先采用以下工程形态：

- Go sidecar：`cmd/qiankun-mcpd` + `internal/*`。
- SQLite：UsageMeter、Memory、Cache 的本地存储。
- JetBrains 插件：`idea-plugin/`，只做状态、安装引导、自检和低频提示。
- MCP：Phase 1/2 只暴露最小工具集，优先 `memory-query`、`usage-report`、`protocol-probe`。
- 后续 VS Code / CLI 入口必须复用 sidecar，不复制核心逻辑。

## Agent 构建建议

当前项目不应一开始构建“全自动强模型 Agent”。推荐按 L0 到 L3 递进：

| 层级 | 角色 | 实现建议 |
| --- | --- | --- |
| L0 | 规则 / 缓存 | ignore、role 识别、tool cache、payload minimizer、UsageMeter，尽量 0 token |
| L1 | 主 Agent | 只做规划、关键推理、最终验收，使用最少必要上下文 |
| L2 | 调度器 | Phase 2 后再引入 `contextgate.think_with` 或等价调度入口 |
| L3 | Worker | 本地或便宜模型做分类、摘要、候选文件筛选，结果必须被 L1 验收 |

落地顺序：

1. 先做 deterministic 能力：ignore、framework profile、role 分类、command discovery、UsageMeter 字段。
2. 再暴露窄 MCP tools：工具名短、描述短、schema 稳定、返回体可压缩。
3. 再做 PromptLint、ScopeClarifier、WorkspaceIndexHinter、PlanGuard 等温和干预。
4. 最后才做 Orchestrator-Worker，且必须记录 worker overhead / saved ratio。

红线：

- 不让 Agent 自动接管所有任务。
- 不默认把整仓、整日志、整 lockfile 塞进上下文。
- 不缓存最终 patch、鉴权结论、数据库迁移脚本等高风险产物作为可复用答案。
- 不为了节省 token 删除关键事实；压缩摘要必须带 `source_refs`。

## 工作流程

每次改动前：

- 先阅读 `README.md`，确认当前阶段和验收口径。
- 再检查实际仓库结构，不要假设 Notion 中的目录一定已经存在。
- 说明影响面，优先最小可行修改。

实现时：

- public CLI 输出 schema 在同阶段内保持兼容。
- `memory-scan` 必须解释 skipped 文件，输出 `skipped_summary`。
- `memory-query` 排序必须综合关键词、路径、framework role、symbol、文件大小惩罚和 noisy 降权。
- command discovery 只能来自真实配置，不伪造默认 test 命令。
- UsageMeter 必须区分 estimated、cache avoided、sent context、adjusted saved、ignored tokens。

交付前：

- 文档变更至少自检语义一致性。
- Go sidecar 行为变更运行 `go test ./...`。
- 跨模块或 CLI 行为变更运行 `make smoke`。
- JetBrains 插件变更运行 `make idea-plugin`。

如命令尚未在仓库中实现，在最终说明中明确“未运行，原因是工程尚未落地该命令”。

## Memory Index 要求

默认排除或强降权：

- `node_modules`、`dist`、`build`、`target`、`coverage`
- `.git`、`.idea`、`.gradle`、`tmp`、`logs`
- `*.lock`、`pnpm-lock.yaml`、`package-lock.json`、`yarn.lock`
- `.claude/settings.local.json`、`.opencode/skills/**`
- 大型二进制、图片、字体、apk、zip、jar、class、map

支持项目级 `.contextgateignore`，并尽量尊重 `.gitignore`。

框架 profile：

- Vue：`vue` 依赖、`vite.config.*`、`.vue` 文件。
- Angular：`@angular/core`、`angular.json`、`project.json`、`nx.json`。
- React：`react` / `react-dom`、`next.config.*`、Vite React plugin、`.tsx` / `.jsx`。
- Spring Boot：`spring-boot-starter`、`src/main/java`、`application*.yml/yaml/properties`。

角色分类至少覆盖：

- app entry、route definition、page/view、reusable component
- state/store、service/api client、domain model/dto
- backend controller、backend service、backend repository/mapper
- configuration、build/test config、documentation、generated/noisy

## InstructionsLinter 要求

检查范围：

- `AGENTS.md`
- `.github/copilot-instructions.md`
- `CLAUDE.md`
- `GEMINI.md`
- README、技能文档和项目规则文档

检查项：

- 文件或总量过长。
- 多文件重复表达。
- 缺少路径作用域。
- 混入时间戳、当前 sprint、临时路径、随机 ID 等动态信息。
- 包含 secret、token、cookie、密钥、PII。

策略：

- 初期 report-only。
- W4 升级到 warning。
- blocking 仅用于高置信安全问题或严重上下文污染。

## 输出风格

- 默认中文。
- 结论先行，避免长篇背景复述。
- 问题用 `SECURITY`、`TYPE`、`ARCH`、`QUALITY`、`PERF`、`DONE` 标记严重性。
- 提到真实文件时使用绝对路径。
- 不确定时给出假设和验证方式，不编造仓库状态。

## 验证命令

目标命令：

```bash
make smoke
go test ./...
make idea-plugin
```

真实项目 Memory Index 验收样例：

```bash
CG=/Users/wangbo/own/QianKun/bin/qiankun-mcpd
ROOT=/Users/wangbo/APMIS/gdsrm/frontend/aims-pda-vue

"$CG" memory-scan --root "$ROOT" --format json
"$CG" memory-query --root "$ROOT" --query "Vant 移动端页面 路由 组件" --top-k 8
"$CG" weekly-report --format markdown --instructions-root "$ROOT"
"$CG" usage-report
```
