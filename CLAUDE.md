# CLAUDE.md

本文件为 Claude Code 提供 QianKun（乾坤袋）项目的工作指引。内容整理自仓库 README、Notion 项目文档与当前代码。

## 项目定位

乾坤袋是面向 **GitHub Copilot 与 AI 编程助手的本地上下文治理层**。它**不替代 Copilot、不自建 AI runtime**，而是在 Copilot 外侧提供缓存、压缩、Memory Index、指令治理和 UsageMeter，让开发者保持原有使用习惯，同时让组织看清 token 成本、缓存命中和节省效果。

**为什么做**：从 2026-06-01 起 GitHub Copilot 迁移到 usage-based billing（按 input/output/cached tokens 折算 AI Credits）。当前 AI IDE 工作流存在上下文过载、重复上下文浪费、召回不稳定、指令不可观测、节省效果不可验证等问题。

**核心设计原则**（改动不得违背）：

- **本地优先**：默认不上传源码和敏感上下文。
- **低打扰**：默认静默优化，只在高置信、高收益时提示（≤2 次/人/天）。
- **可观测**：任何节省结论都必须能被 UsageMeter 解释。
- **可降级**：sidecar 失败不能影响 Copilot 本体。
- **先规则后模型**：能用 ignore、cache、schema、排序解决的，不先调 LLM。
- **先证明不亏**：乾坤袋自身 overhead 必须低于节省量并被持续报告（目标 ≤ 节省的 5%）。

## 系统架构

形态为 **Thin Plugin + Sidecar**：IDEA 插件（状态栏/自检/低频提示）为前端，本地常驻 sidecar `qiankun-mcpd` 承载核心逻辑。CLI、Git Hook、未来的 VS Code Extension 复用同一 sidecar。MCP 只暴露最小工具集，避免工具描述/返回成为新的 token 黑洞。

## 当前阶段

仓库处于 **Phase W3**，版本 `0.3.0-w3`。已落地能力按周期划分：

- **W1**：`internal/toolcache`（线程安全 KV，in-memory + JSON 持久化、TTL、LRU 驱逐）；`internal/injection`（解析 `<!-- QIANKUN:START/END -->` C1 注入区，读取 `AGENTS.md`/`CLAUDE.md`）。
- **W2**：`internal/memory/tokens`（保守 token 估算）；`internal/memory/scan`（仓库遍历 Walker，输出文件分档/权重/token/跳过原因，默认跳过噪声目录、lockfile、AI 工具历史，轻量支持 `.gitignore`/`.contextgateignore`）。
- **W3**：`internal/memory/index`（SQLite 持久化 Memory Index + 关键词/FTS5 检索）；`internal/usage`（UsageMeter，SQLite）；`internal/instructions`（InstructionsLinter v0，report-only）；`internal/weekly`（周报聚合）。
- `internal/compaction` 仅为占位包，**不介入会话流**。

**尚未实现（W4+）**：完整 framework profile、symbol index 深化、hybrid rerank、semantic cache、MCP server 端到端、IDEA 插件产品化、企业驾驶舱。不要在文档或代码中宣称已具备这些能力。

## 代码结构

- `cmd/qiankun-mcpd/main.go` — CLI 入口，子命令通过 `args[0]` 分发。
- 命令：`--version`、`--health`、`memory-scan`、`memory-query`、`usage-report`、`weekly-report`。
- `internal/<domain>/` — 业务逻辑分层，见上文各 package。
- 默认数据目录 `${QIANKUN_HOME:-~/.qiankun}`：`db/memory.sqlite`、`db/usage.sqlite`、`cache/`、`logs/`。

## 构建与验证

```bash
make build        # go build -o bin/qiankun-mcpd ./cmd/qiankun-mcpd
make test         # go test ./...
make smoke        # test + build + 端到端冒烟（health/memory-scan/query/usage/weekly）
go test -race ./...
make idea-plugin  # W3 仅占位输出
```

技术栈：Go 1.22，SQLite 使用 `modernc.org/sqlite`（**纯 Go，无需 CGO**，便于 sidecar 分发）。FTS5 在运行时探测，不可用时降级到 `keyword_index` + Go 侧评分，不阻断查询。

## 约定

- Module path 为 `github.com/xiaoboxuezhangora/QianKun`，必须与远端仓库一致以支持 `go install`。
- 代码注释与文档使用**中文**，与现有风格保持一致。
- 各 CLI 命令以非阻断方式同步 Memory Index / UsageMeter：写入失败只在 stderr/health 体现，不影响主输出。
- 索引/usage 表结构变更应保持向后兼容；`memory-scan` 的 JSON schema 不应破坏性变更。
- 噪声治理是核心价值：lockfile、生成物、`node_modules`/`dist`/`build`/`.idea` 等默认不进 top-k，除非 query 明确要求。命令发现只能从真实 scripts/Gradle task 解析，不能伪造（如 `pnpm test`）。
