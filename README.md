# QianKun / 乾坤袋

面向 GitHub Copilot 与 AI 编程助手的**本地上下文治理层**。它不替代 Copilot、不自建 AI runtime，而是在 Copilot 外侧提供缓存、压缩、Memory Index、指令治理和 UsageMeter，让开发者保持原有使用习惯，同时让组织看清 token 成本、缓存命中和节省效果。

当前版本 `0.4.0-w4`（Phase W4）。

## 背景

从 2026-06-01 起 GitHub Copilot 迁移到 usage-based billing，按 input / output / cached tokens 折算 AI Credits。当前 AI IDE 工作流存在几类浪费：

- **上下文过载**：整仓扫描、整文件粘贴、lockfile、构建产物挤占真正有用的业务源码。
- **重复上下文**：仓库结构、工具结果、长 instructions 在多轮会话中反复进入上下文。
- **召回不稳定**：简单文件扫描读不懂 Vue / Angular / React / Spring 项目结构，常把文档、配置、锁文件排到业务文件前面。
- **指令不可观测、节省不可验证**：只说"节省 token"不够，需要能解释节省、缓存、开销与净收益。

设计原则：本地优先、低打扰、可观测、可降级、先规则后模型、先证明不亏（自身 overhead ≤ 节省的 5%）。

## 安装

### 一键安装（可访问 GitHub 时）

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/install.sh | bash
```

无需 Go，自动识别 macOS / Linux 平台，从 GitHub Release 下载预编译二进制并校验 SHA-256。可选环境变量 `QIANKUN_VERSION` / `QIANKUN_INSTALL_DIR` / `QIANKUN_GH_PROXY`。

> 国内网络若 `curl` 超时（连不上 `raw.githubusercontent.com`），用本机代理后再执行原始命令最稳；或经 jsDelivr 取脚本 + 镜像下载二进制：
> ```bash
> export QIANKUN_GH_PROXY=https://ghproxy.net/   # 备选 https://gh-proxy.com/
> curl -fsSL https://cdn.jsdelivr.net/gh/xiaoboxuezhangora/QianKun@main/scripts/install.sh | bash
> ```

### 内网 GitLab 离线安装（无需 Go、无需联网）

预编译产物随 `dist-internal` 分支分发，clone 后直接安装：

```bash
git clone -b dist-internal http://oscoe.neusoft.com:8000/b.w_neu/qiankun.git
cd qiankun
bash scripts/install-local.sh      # 自动选平台二进制、校验、装入 PATH
```

Windows 用户直接取 `prebuilt/qiankun-mcpd-*-windows-amd64.exe` 使用。

### 源码安装（有 Go 1.22+）

```bash
go install github.com/xiaoboxuezhangora/QianKun/cmd/qiankun-mcpd@latest
# 或本地构建
make build      # 产物 bin/qiankun-mcpd
```

SQLite 使用纯 Go 的 `modernc.org/sqlite`，**无需 CGO**，交叉编译与分发简单。

### 卸载

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoboxuezhangora/QianKun/main/scripts/uninstall.sh | bash
# 加 --purge 同时删除数据目录 ${QIANKUN_HOME:-~/.qiankun}
```

## 主要节省 token 的方式

| 机制 | 怎么省 |
| --- | --- |
| **Memory Index（框架感知召回）** | 识别 Vue / Angular / React / Spring profile 与真实 role，优先把工程师会先看的业务源码喂给助手；lockfile、生成物、`node_modules`/`dist` 等噪声默认不进 top-k，减少无效上下文。 |
| **Tool Result Cache** | 线程安全 KV（内存 + JSON 持久化、TTL、LRU），缓存重复的工具结果，避免同样的上下文反复进入会话。 |
| **Hybrid Retrieval + MMR** | 召回后按可解释的 Rel（路径/正文/符号/意图/role 加分 + 体积惩罚）排序，再用 MMR 做多样性 rerank，少而准地给上下文。 |
| **Symbol Index v0 + 命令发现** | 提取组件/路由/store/service/Controller 等符号折入召回；命令只从真实 scripts / Maven / Gradle task 解析，不伪造（如不会编出 `pnpm test`）。 |
| **Instructions Linter** | 检查 `AGENTS.md` / `CLAUDE.md` 等指令文件过长、重复、缺作用域、混入动态信息或泄露 secret，给指令"减肥"，降低每轮固定开销。 |
| **UsageMeter（Saved vs Overhead）** | 区分 `estimated_saved` / `cache_avoided` / `sent_context` / `adjusted_saved` / `ignored` tokens，把节省与乾坤袋自身 overhead 并排呈现，证明净收益为正。 |

## CLI 命令

| 命令 | 用途 |
| --- | --- |
| `qiankun-mcpd --version` / `--health` | 版本 / readiness 自检 |
| `qiankun-mcpd memory-scan --root <path> --format json` | 扫描仓库，输出分档/权重/profile/role/token 估算 |
| `qiankun-mcpd memory-query --root <path> --query "<text>" --top-k 8` | 框架感知召回相关文件（hybrid + MMR + 命令发现） |
| `qiankun-mcpd usage-report` | 本地 UsageMeter 汇总 |
| `qiankun-mcpd weekly-report --format markdown --instructions-root <path>` | token / cache / Memory / Saved vs Overhead / Instructions 周报 |
| `qiankun-mcpd mcp` | 以 stdio 启动最小 MCP server，暴露 `memory-query` / `usage-report` |

## 构建与验证

```bash
make build      # 构建
make test       # go test ./...
make smoke      # test + build + 端到端冒烟
go test -race ./...
```

## 当前能力边界

W1–W4 已落地：toolcache、injection、token 估算与仓库扫描、SQLite Memory Index、UsageMeter、Instructions Linter、周报、stdio MCP server、hybrid retrieval + MMR、Symbol Index v0、命令发现、framework profile 与真实 role 分配。

尚未实现（不要据此宣称已具备）：semantic / 向量召回、symbol index 深化（跨文件引用、调用图、LSP）、IDEA 插件产品化、企业驾驶舱；`internal/compaction` 仍为占位包，不介入会话流。
