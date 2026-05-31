# QianKun / 乾坤袋

乾坤袋是面向 GitHub Copilot 与 AI 编程助手的本地上下文治理层。它不替代 Copilot，不自建完整 AI runtime，而是在 Copilot 外侧提供缓存、压缩、Memory Index、指令治理和 UsageMeter，让开发者尽量保持原有使用习惯，同时让组织看清 token 成本、缓存命中和节省效果。

本文基于 Notion 文档《乾坤袋 · 中文友好精简版（背景 · 原理 · 开发方案）》及其子文档整理。当前仓库如尚未包含对应代码目录，请把本文视为工程落地说明和验收口径。

## 为什么做

GitHub 官方文档说明：从 2026-06-01 起，Copilot 将从 request-based billing 迁移到 usage-based billing，Copilot 交互会按 input tokens、output tokens、cached tokens 等消耗折算为 GitHub AI Credits。项目文档中的成本假设已与 GitHub Docs / GitHub Blog 的公开说明一致。

当前 AI IDE / Copilot 工作流的主要问题：

- 上下文过载：整仓扫描、整文件粘贴、lockfile、历史文档、AI 工具缓存和构建产物挤占真正有用的业务源码上下文。
- 重复上下文浪费：仓库结构、工具结果、扫描摘要和长 instructions 在多轮会话中反复进入上下文。
- 召回不稳定：简单文件扫描不能理解 Vue、Angular、React、Spring Boot 等项目结构，容易把文档、配置或锁文件排到业务文件前面。
- 指令不可观测：`AGENTS.md`、`CLAUDE.md`、Copilot instructions、README、技能文档可能重复、过长、缺少作用域或混入动态信息。
- 节省效果不可验证：只说“节省 token”不够，需要 UsageMeter 能解释节省、缓存、开销和净收益。

## 产品目标

对普通开发者：继续正常使用 Copilot，只是更少粘贴、更少等待、更少被无关上下文误导。

对组织管理者：看得见用量、解释得清节省、管得住高风险和高成本场景。

目标指标：

| 指标 | 目标 |
| --- | --- |
| 组织月 token 节省 | 基础 >= 30%，目标 45% |
| Phase 1 灯塔用户节省 | >= 20% |
| 乾坤袋自身消耗 | <= 节省的 5% |
| 首次安装到可用 | <= 5 分钟 |
| 每日主动打扰 | <= 2 次 / 人 / 天 |

## 总体架构

乾坤袋采用 Thin Plugin + Sidecar 的形态：

- JetBrains / IntelliJ IDEA 插件优先：状态栏、自检入口、安装引导、低频提示、个人看板入口。
- 本地 sidecar `qiankun-mcpd` 承载核心逻辑：Memory Index、UsageMeter、tool cache、payload minimizer、instructions lint、health check。
- W1 的 Tool Result Cache 首版使用 in-memory + JSON 文件持久化；SQLite 仍保留给后续 UsageMeter / Memory / Cache 演进。
- MCP 只暴露最小工具集，避免工具描述和工具返回成为新的 token 黑洞。
- VS Code Extension 与 CLI 作为后续入口复用同一个 sidecar。

```mermaid
flowchart LR
  IDEA["IntelliJ IDEA Plugin\n状态栏 / 自检 / 低频提示"] --> SC["qiankun-mcpd Sidecar"]
  CLI["CLI / Git Hook"] --> SC
  VS["未来 VS Code Extension"] --> SC
  SC --> TC["JSON Tool Cache\nW1"]
  SC --> DB["SQLite W3+\nUsageMeter / Memory / Cache"]
  SC --> MCP["MCP Tools"]
  MCP --> CP["GitHub Copilot"]
```

五个核心能力面：

| 能力面 | 作用 | 关键模块 |
| --- | --- | --- |
| Memory & Cache | 减少重复扫描和重复上下文 | Memory Index、Tool Result Cache、C1/C2/C3、C4、C5 |
| Instructions | 给项目规则减肥并检查风险 | InstructionsLinter、路径级 instructions、重复检测 |
| MCP Tools | 控制工具数量、描述和返回体积 | usage-report、config-query、protocol-probe、payload minimizer |
| Skills & Chat Modes | 只在高收益场景轻提示 | PromptLint、ScopeClarifier、WorkspaceIndexHinter、PlanGuard |
| UsageMeter | 证明是否真的节省 | token 估算、cacheable ratio、saved vs overhead、weekly report |

## 工程结构

```text
.
├── cmd/
│   └── qiankun-mcpd/
│       └── main.go
├── internal/
├── idea-plugin/
├── scripts/
├── testdata/
├── Makefile
├── README.md
└── AGENTS.md
```

当前 W4 已在 W1/W2/W3 基础上新增 stdio MCP server、hybrid retrieval + MMR、Symbol Index v0、命令发现、framework profile 与真实 role 分配、InstructionsLinter warning 以及周报 Saved vs Overhead 净收益视图。`internal/compaction` 仍只是占位包；semantic cache、symbol 深化/LSP、复杂 Agent 编排、IDEA 插件产品化和企业驾驶舱仍属于未来工作。

CLI 状态：

| 命令 | 用途 | 当前状态 |
| --- | --- | --- |
| `qiankun-mcpd --version` | 输出 sidecar 当前版本 | 已实现，当前版本 `0.4.0-w4` |
| `qiankun-mcpd --health` | 输出 sidecar readiness | 保持顶层 `status=ready`，包含 `toolcache` 和 `usage` 子对象 |
| `qiankun-mcpd memory-scan --root <path> --format json` | 扫描仓库，输出文件分档、权重、profile/role、token 估算和 skipped summary | 已实现；同时非阻断同步 SQLite Memory Index 和 UsageMeter |
| `qiankun-mcpd memory-query --root <path> --query "<text>" --top-k 8` | 查询相关文件上下文（hybrid retrieval + MMR + 命令发现） | 已实现，自动扫描/增量更新索引 |
| `qiankun-mcpd usage-report` | 输出本地 UsageMeter 汇总 | 已实现 |
| `qiankun-mcpd weekly-report --format markdown --instructions-root <path>` | 输出 token、cache、Memory、Saved vs Overhead、Instructions 周报 | 已实现，支持 `--output <file>` |
| `qiankun-mcpd mcp` | 以 stdio 启动最小 MCP server，暴露 `memory-query`/`usage-report` | W4 已实现，端到端 Go 测试覆盖 |

安装方式：

一键安装（无需 Go 环境，自动识别 macOS / Linux 平台，从 GitHub Release 下载预编译二进制并校验 SHA-256）：

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/install.sh | bash
```

可选环境变量：`QIANKUN_VERSION`（指定版本 tag，默认最新 release）、`QIANKUN_INSTALL_DIR`（指定安装目录，默认 `/usr/local/bin` 或 `~/.local/bin`）、`QIANKUN_GH_PROXY`（GitHub 下载镜像前缀，国内网络用）。Windows 用户请从 [Releases](https://github.com/xiaoboxuezhangora/QianKun/releases) 手动下载 `.exe`。

> 国内网络若出现 `curl: (28) ... timeout`（连不上 `raw.githubusercontent.com` / `github.com`），可经 jsDelivr 取脚本并用镜像下载二进制：
>
> ```bash
> export QIANKUN_GH_PROXY=https://ghproxy.com/   # 换成任一可用的 GitHub 镜像
> curl -fsSL https://cdn.jsdelivr.net/gh/xiaoboxuezhangora/QianKun@main/scripts/install.sh | bash
> ```
>
> 脚本已内置连接超时与重试，连不通会快速报错而非长时间卡住；版本号在 GitHub API 不可达时自动回退到 jsDelivr 查询。公共镜像时好时坏，建议用自己可用的代理/镜像。

有 Go 环境时也可源码安装：

```bash
go install github.com/xiaoboxuezhangora/QianKun/cmd/qiankun-mcpd@latest
```

该命令要求远端 GitHub 仓库路径与 `go.mod` 的 module path 保持一致，即 `github.com/xiaoboxuezhangora/QianKun`。

卸载（默认只删二进制并保留数据目录，加 `--purge` 才一并删除 `${QIANKUN_HOME:-~/.qiankun}`）：

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/uninstall.sh | bash
# 连同数据目录一起删除：
curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/uninstall.sh | bash -s -- --purge
```

### 预编译产物（手动安装）

无 Go 环境也可从 [Releases](https://github.com/xiaoboxuezhangora/QianKun/releases) 手动下载对应平台二进制。所有产物为纯 Go、无需 CGO 的静态可执行文件，拷贝即用。

| 平台 | 产物文件 |
| --- | --- |
| macOS Apple Silicon | `qiankun-mcpd-<version>-darwin-arm64` |
| macOS Intel | `qiankun-mcpd-<version>-darwin-amd64` |
| Linux x86_64 | `qiankun-mcpd-<version>-linux-amd64` |
| Linux arm64 | `qiankun-mcpd-<version>-linux-arm64` |
| Windows x86_64 | `qiankun-mcpd-<version>-windows-amd64.exe` |

安装与校验（以 macOS Apple Silicon 为例）：

```bash
# 1. 校验 SHA-256（Release 附带 checksums.txt）
shasum -a 256 -c checksums.txt
# 2. 赋可执行权限并放入 PATH
chmod +x qiankun-mcpd-0.4.0-w4-darwin-arm64
sudo mv qiankun-mcpd-0.4.0-w4-darwin-arm64 /usr/local/bin/qiankun-mcpd
# 3. macOS 首次运行解除 Gatekeeper 隔离（二进制未签名）
xattr -d com.apple.quarantine /usr/local/bin/qiankun-mcpd 2>/dev/null || true
# 4. 验证
qiankun-mcpd --version   # 0.4.0-w4
```

如需在有 Go 的机器上自行交叉编译（一台机器即可全平台出包，无需目标平台工具链）：

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o dist/qiankun-mcpd-darwin-arm64 ./cmd/qiankun-mcpd
# 将 GOOS/GOARCH 替换为 linux/amd64、linux/arm64、windows/amd64 等即可。
```

目标验证命令：

```bash
make smoke
go test ./...
go test -race ./...
make idea-plugin
```

## 当前阶段判断

当前仓库处于 **Phase W4 / 框架感知召回、MCP server 与净收益周报已落地** 状态。在 W1–W3 之上，W4 落地 stdio MCP server、hybrid retrieval + MMR、Symbol Index v0、命令发现、framework profile 与真实 role 分配、InstructionsLinter warning 与周报 Saved vs Overhead；`internal/compaction` 仍是占位文档，不介入长会话流。本仓库当前不能宣称具备 semantic cache、symbol index 深化/LSP、IDEA 插件真实产品化或企业驾驶舱。

W1 已落地：

- Go module：`github.com/xiaoboxuezhangora/QianKun`。
- 目录骨架：`cmd/qiankun-mcpd/`、`internal/`、`idea-plugin/`、`scripts/`、`testdata/`。
- `internal/toolcache` 提供线程安全 KV store，支持 in-memory + JSON 文件持久化、TTL 过期、LRU 容量驱逐、Stats 和 Close。
- Toolcache key 格式为 `<tool>:<arg-hash>:<schema-ver>`，参数 hash 基于 JSON 稳定序列化，schema version 为空时使用默认 `v1`。
- `internal/injection` 支持解析 `<!-- QIANKUN:START -->` / `<!-- QIANKUN:END -->` C1 注入区，并提供项目根目录 `AGENTS.md` / `CLAUDE.md` 的文件级读取入口。
- `docs/spec/c1-injection-zone.md` 记录注入区协议、幂等原则、错误处理和 W1 非目标。
- `qiankun-mcpd --version` 输出 `0.4.0-w4`。
- `qiankun-mcpd --health` 输出稳定 JSON，顶层保留 `{"status":"ready"}` 语义，并包含 `toolcache` 与 `usage` 子对象。
- 未识别参数返回非 0，并输出简短 usage。
- `scripts/bootstrap.sh` 可重复创建 `~/.qiankun/bin`、`~/.qiankun/cache`、`~/.qiankun/db`、`~/.qiankun/logs`。
- `make idea-plugin` 仍为占位，输出 IDEA 插件占位说明，退出码为 0。

W2 已落地：

- `internal/memory/tokens` 提供 `EstimateTextTokens`，对英文、中文/日韩、混合文本和空文本做保守估算。
- `internal/memory/scan` 提供标准库仓库遍历 Walker，输出相对路径、文件分档、权重、token 估算、跳过状态和跳过原因。
- 默认跳过高噪声目录：`node_modules`、`dist`、`build`、`target`、`coverage`、`.git`、`.idea`、`.gradle`、`tmp`、`logs`。
- 默认识别并跳过 lockfile：`*.lock`、`pnpm-lock.yaml`、`package-lock.json`、`yarn.lock`。
- 默认识别并跳过 AI 工具缓存/历史：`.claude/settings.local.json`、`.opencode/skills/**`、`.ai-docs/**`。
- 对超过 64KB 的文本文件只读取尾部 8KB 作为 sample；二进制按扩展名或 NUL sample 识别后不进入索引。
- 支持 `--include` / `--exclude` 基础 glob 过滤，为后续规则演进保留兼容入口。
- `skipped_summary` 按原因聚合数量，并保留最多 3 个代表路径。
- 对 `android/**` 下 Gradle、Manifest 和配置类文件做低权重保守索引，避免误跳过 Capacitor / Android 关键配置；W2 不做完整 Android / Capacitor 语义召回。
- 轻量支持根目录 `.gitignore` 和 `.contextgateignore`：支持注释、空行、目录规则、基础 glob 和 `**`；暂不支持 `!` 反选和完整 Git ignore 语义。
- `internal/compaction` 仅是占位 package，明确 W2 不做真实会话压缩。

W3 已落地：

- 新增 `internal/memory/index`，使用 SQLite 持久化 `memory-scan` 的 indexed 文件结果。
- 默认 Memory DB 路径为 `${QIANKUN_HOME:-~/.qiankun}/db/memory.sqlite`。
- `file_summary` 保存 `root`、`path`、`kind`、`weight`、`token_estimate`、`size_bytes`、`content_sample`、`file_hash`、`mtime_unix_nano`、`updated_at`。
- `keyword_index` 用于 FTS5 不可用时的关键词检索降级。
- `recent_change` 记录最小 upsert/delete 事件；`symbol` 表自 W4 起承载 Symbol Index v0 提取的符号（W3 仅建表）。
- `memory-scan` 会在保持原 JSON schema 的同时非阻断同步 SQLite Memory Index。
- `memory-query` 会自动先扫描并基于 mtime + file hash 做增量更新；skipped/noisy/binary 文件不进入 FTS 或关键词索引。
- SQLite driver 选择 `modernc.org/sqlite`：纯 Go、无需 CGO，便于本地 sidecar 分发；FTS5 通过运行时 `CREATE VIRTUAL TABLE ... fts5` 探测。
- 如当前 driver/SQLite 构建不支持 FTS5，代码会把 `fts5_enabled=false` 写入 meta，并使用 `keyword_index` + Go 侧评分降级检索；该降级不阻断 `memory-query`。
- 新增 `internal/usage`，默认 Usage DB 路径为 `${QIANKUN_HOME:-~/.qiankun}/db/usage.sqlite`。
- UsageMeter 事件模型包含 `call`、`cache_hit`、`cache_miss`、`latency`、`token_estimate`，并聚合 calls、cache、tokens、saved tokens 和 p95 latency。
- `memory-scan` / `memory-query` 会记录最小 usage 事件；UsageMeter 写入失败只在 stderr/health 中体现，不阻断主流程。
- 新增 `internal/instructions`，InstructionsLinter 检查指令文件过长、重复段落、缺少作用域、动态信息和明显 secret/token/cookie/private key 模式，并升级为 warning 级别输出。
- 新增 `internal/weekly` 和 `weekly-report`，聚合 Memory Index、UsageMeter、Instructions findings 和 known gaps。

W4 已落地：

- 新增 `internal/mcp` 与 `cmd/qiankun-mcpd/mcp.go`：以 stdio 常驻方式提供最小 MCP server，仅暴露 `memory-query` 与 `usage-report` 两个工具；stdout 为 JSON-RPC 帧通道，诊断只走 stderr，`top_k` 越界 clamp 到 [1,20]；由 `cmd/qiankun-mcpd/mcp_e2e_test.go` 以真实子进程端到端验收 initialize / tools/list / tools/call。
- `internal/memory/index` 升级为 hybrid retrieval：在召回候选上计算可解释的 Rel（路径/kind/正文/keyword/符号/意图/role 逐项加分 + 体积惩罚），再以 MMR 做多样性 rerank；FTS5 与 keyword 回退两路排序语义一致。
- 新增 `internal/memory/symbols`：Symbol Index v0，用正则/AST-lite 从文件头部提取组件、路由、store、composable/hook、service/API 方法、Controller、Service、Repository、Mapper、`@*Mapping` endpoint 等符号，写入 `symbol` 表并折入召回。
- 新增 `internal/memory/commands`：命令发现仅从真实 `package.json` scripts、Maven/Gradle task 解析，不伪造 `pnpm test` 等命令；作为查询上下文随 `memory-query` 返回。
- `internal/memory/scan` 落地 framework profile（Vue/Angular/React/Spring，monorepo 可多 profile 共存）与真实 role 分配（app-entry/route-definition/page-view/component/store/service/controller/repository/configuration/documentation/generated-noisy），role 接入检索 `roleBoost` 与噪声治理；`memory-scan` JSON schema 保持向后兼容（`profile`/`role` 字段已存在，仅填入真实值）。
- 四套框架 fixtures（vue/angular/react/spring）Top-5 命中率闸门 ≥80%，FTS5 与 keyword 回退两路实测总体 95.5%（21/22）。
- 周报新增 `Saved vs Overhead` 净收益视图，把节省与乾坤袋自身 overhead 并排呈现。

W2 使用示例：

```bash
./bin/qiankun-mcpd memory-scan --root testdata/memory-scan-fixture --format json
./bin/qiankun-mcpd memory-scan testdata/memory-scan-fixture
./bin/qiankun-mcpd memory-scan --root . --format json --include 'src/**' --exclude 'dist/**'
```

W3 使用示例：

```bash
./bin/qiankun-mcpd memory-query --root testdata/memory-scan-fixture --query "Vue router component" --top-k 5
./bin/qiankun-mcpd usage-report
./bin/qiankun-mcpd weekly-report --format markdown --instructions-root .
./bin/qiankun-mcpd weekly-report --format markdown --instructions-root . --output /tmp/qiankun-weekly.md
```

W2 JSON 输出结构：

```json
{
  "root": "/absolute/project",
  "generated_at": "2026-05-28T00:00:00Z",
  "totals": {
    "files_seen": 8,
    "files_indexed": 4,
    "files_skipped": 4,
    "directories_skipped": 3,
    "estimated_tokens": 123
  },
  "skipped_summary": {
    "build_artifact": {
      "count": 1,
      "representative_paths": ["dist"]
    }
  },
  "files": [
    {
      "path": "src/main.ts",
      "size_bytes": 96,
      "kind": "source",
      "weight": 90,
      "token_estimate": 24,
      "skipped": false,
      "profile": "vue",
      "role": "app-entry"
    }
  ]
}
```

W3 `memory-query` JSON 输出结构：

```json
{
  "root": "/absolute/project",
  "query": "Vue router component",
  "top_k": 5,
  "results": [
    {
      "path": "src/App.vue",
      "kind": "source",
      "weight": 90,
      "score": 120,
      "token_estimate": 16,
      "snippet": "<template>..."
    }
  ]
}
```

W3 `usage-report` JSON 输出结构：

```json
{
  "db_path": "/Users/name/.qiankun/db/usage.sqlite",
  "generated_at": "2026-05-29T00:00:00Z",
  "total_calls": 2,
  "cache_hits": 1,
  "cache_misses": 1,
  "estimated_tokens": 120,
  "estimated_saved_tokens": 300,
  "cache_avoided_tokens": 300,
  "sent_context_tokens_estimate": 120,
  "adjusted_saved_tokens": 80,
  "ignored_tokens_estimate": 220,
  "p95_latency_ms": 12
}
```

W3 验证方式：

```bash
go test ./...
go test -race ./...
make smoke
./bin/qiankun-mcpd memory-scan --root testdata/memory-scan-fixture --format json
./bin/qiankun-mcpd memory-query --root testdata/memory-scan-fixture --query "Vue router component" --top-k 5
./bin/qiankun-mcpd usage-report
./bin/qiankun-mcpd weekly-report --format markdown --instructions-root .
```

W5+ 仍未实现：

- symbol index 深化和 LSP/framework symbol 提取（W4 仅 v0 正则/AST-lite）。
- semantic cache。
- 真正 IDEA 插件产品化，包括状态栏、自检、安装引导和用户交互。
- 企业驾驶舱。

## 能力面进展

W4 已按五个能力面推进，下表记录退出标志的达成情况：

| 分线 | 核心动作 | 退出标志 | 状态 |
| --- | --- | --- | --- |
| Memory & Cache | 框架感知召回，噪声目录降权 | Vue / Angular / React / Spring Boot 查询 Top-5 命中率 >= 80% | W4 已达成（四框架整体 95.5%） |
| Instructions | Linter 从 report-only 升级到 warning | 真实项目至少消除 3 类指令异味 | W4 已落地 warning 分级 |
| UsageMeter | `saved vs overhead` 并排视图、基线对照 | weekly-report 输出净收益数字 | W4 已落地 |
| MCP Tools | MCP server 暴露 `memory-query` / `usage-report` | 外部 MCP client 端到端调通 | W4 已落地 stdio server |
| Skills | 调研 Copilot Slash Commands 注入点 | 产出 1 页设计 spike | W4 已产出 spike（见 `docs/spec/`） |

## Memory Index v1 要点

Memory Index 的目标不是“尽量多扫文件”，而是让 AI 助手优先看到工程师会先看的上下文。

P0 能力：

- 默认忽略 `node_modules`、`dist`、`build`、`target`、`coverage`、`.git`、`.idea`、`.gradle`、lockfile、大型二进制、生成文件、AI 工具历史目录等噪声。
- 支持 `.contextgateignore`，并尽量尊重 `.gitignore`。
- W4 已落地完整 Vue、Angular、React、Spring Boot framework profile 检测，可在 monorepo 中按 marker 目录最长前缀共存。
- W4 已给索引文件分配真实 role：app entry、route definition、page/view、component、store、service、controller、repository、configuration、documentation、generated/noisy；无法归类的源码留空。
- lockfile / generated / noisy 默认不进入 top-k，除非 query 明确要求。
- 命令发现只能从真实 scripts、Maven/Gradle task 或框架配置解析，不能凭 package manager 伪造 `pnpm test`。
- UsageMeter 区分 `estimated_saved_tokens`、`cache_avoided_tokens`、`sent_context_tokens_estimate`、`adjusted_saved_tokens`、`ignored_tokens_estimate`。

W4 已落地能力：

- Symbol Index v0：提取组件、路由、store、service/API 方法、Controller、Service、Repository、Mapper、RequestMapping 等轻量符号。
- Hybrid Retrieval：先召回，再按 role、query 意图、文件大小惩罚和 MMR 多样性 rerank。

## 真实项目验收样例

以下命令可用于手工冒烟；W4 已落地 framework profile、完整 role 识别与 command discovery，Vue/Android 的高质量 Top-K 命中率可一并验收。

```bash
CG=/Users/wangbo/own/QianKun/bin/qiankun-mcpd
ROOT=/Users/wangbo/APMIS/gdsrm/frontend/aims-pda-vue

"$CG" memory-scan --root "$ROOT" --format json
"$CG" memory-query --root "$ROOT" --query "Vue Vite 构建 type-check lint pnpm 脚本" --top-k 8
"$CG" memory-query --root "$ROOT" --query "Vant 移动端页面 路由 组件" --top-k 8
"$CG" memory-query --root "$ROOT" --query "Capacitor Android 打包 sync-android build-release-apk" --top-k 8
"$CG" weekly-report --format markdown --instructions-root "$ROOT" --output /tmp/qiankun-aims-pda-weekly.md
"$CG" usage-report
```

验收判断：

- `memory-scan` 不把 `pnpm-lock.yaml` 作为核心索引文件。
- Vue 查询命中 `src` 下页面、组件、router、store、api 文件。
- `package.json` 没有 test 脚本时，不返回 `pnpm test`。
- build、type-check、lint 从真实 scripts 解析。
- Android / Capacitor 查询仍能命中 `capacitor.config`、Android Gradle 和 package scripts。
- weekly-report 展示 adjusted、sent context、ignored tokens 等保守指标。
- `skipped_summary` 能解释跳过了哪些噪声。

## 设计原则

- 本地优先：默认不上传源码和敏感上下文。
- 低打扰：默认静默优化，只在高置信、高收益、低打扰时提示。
- 可观测：任何节省结论都必须能被 UsageMeter 解释。
- 可降级：sidecar 失败不能影响 Copilot 本体。
- 先规则后模型：能用 ignore、cache、schema、排序解决的，不先调用 LLM。
- 先证明不亏：乾坤袋自身 overhead 必须低于节省量，并持续被报告。

## 资料来源

- Notion: 乾坤袋 · 中文友好精简版（背景 · 原理 · 开发方案）
- Notion: 乾坤袋 · 搭建文档（项目背景、目标与基础框架）
- Notion: 增强 Memory Index 能力方案
- GitHub Docs: https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing
- GitHub Blog: https://github.blog/news-insights/company-news/github-copilot-is-moving-to-usage-based-billing/
