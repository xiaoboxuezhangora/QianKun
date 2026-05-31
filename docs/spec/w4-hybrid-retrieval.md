# Hybrid Retrieval 与 Symbol Index v0 设计

## 目的

W3 的 Memory Index 已能召回（FTS5 或 keyword_index 回退）并按文件级权重粗排，但缺少“按职责理解仓库”的能力：查询「订单 service 接口」时无法优先返回真正的 service 文件，生成物（如 `*.d.ts`）也可能挤进 top-k。

本设计在召回结果之上引入两层能力：

1. **Symbol Index v0**：用正则 / AST-lite（先规则后模型，不引入 LSP）从源文件头部提取组件、路由、store、composable/hook、service/API 方法、Controller、Service、Repository、Mapper、`@*Mapping` endpoint 等符号，写入 `symbol` 表并折入召回。
2. **Hybrid Retrieval**：在召回候选上计算可解释的相关性 Rel，再以 MMR 做多样性 rerank，输出 top-k。

设计严格遵循项目原则：**先规则后模型**、**可观测**（任何排序都能由包级常量逐项还原）、**可降级**（FTS5 不可用时 keyword 回退，rerank 对召回来源透明）。

## Symbol Index v0

### 提取范围与读取约定

- 仅读取文件**头部** `HeadReadLimit = 16KB`（小文件读全量）。绝大多数符号声明位于文件头部；该读取与索引层 `content_sample` 的“尾部 8KB”语义互不影响、互为补充。
- 含 NUL 的二进制文件不提取。读取失败返回 nil，**不阻断索引**。
- 提取是纯函数（`symbols.Extract`），不触碰数据库；幂等由调用方按文件重建保证。

### 符号类别与权重

`kindWeight` 体现“文件职责”强弱，入口型权重最高、定义型较低：

| Kind | 权重 | 触发特征 |
| --- | --- | --- |
| endpoint | 85 | `@Get/Post/Put/Delete/Patch/RequestMapping(...)` |
| route | 80 | `createRouter` / `createBrowserRouter` / `RouterModule.forRoot/Child` / `<Route path>` / router 目录 |
| controller | 75 | `@RestController` / `@Controller` |
| store | 70 | `defineStore` / `createSlice` |
| api-method / service / repository | 65 | services|api 目录导出函数 / `@Injectable`、`@Service` / `@Repository`、`@Mapper`、`JpaRepository` |
| component | 60 | `.vue` SFC / `@Component` / JSX 中 PascalCase 函数 |
| module | 55 | `@NgModule` |
| composable / hook / entity | 50 | `use*` 导出（.ts 记 composable、.tsx 记 hook）/ `@Entity` |

未知类别回退 40（`WeightFor`）。

### 折入召回（关键约定）

符号名会被 tokenize、去重后折入两处召回通道，使「按符号名检索」可命中：

- **FTS 文档**：`content_sample + " " + 符号词项` 一并写入 `file_fts`。
- **keyword_index**：基础正文词项以 `from_symbol = 0` 写入；折入的符号词项以 `from_symbol = 1` 写入（`INSERT OR IGNORE`，不覆盖基础词项的 count）。

`from_symbol` 是本期新增列，通过 `ensureColumn`（`PRAGMA table_info` 探测后按需 `ALTER TABLE ADD COLUMN`）**向后兼容**迁移。

**为什么分流**：召回阶段需要符号词项参与匹配（`recallPaths` 不区分 `from_symbol`）；但 `keywordCount` 计分只统计 `from_symbol = 0` 的基础词项，避免符号信号被重复计入——符号信号已由 Rel 的 `SymbolScore` 显式计算。如此 Rel 保持可解释，不会因“符号名恰好等于正文词”而双倍加分。

## 检索打分（Rel）

`computeRel` 在召回候选上逐项累加，所有系数为包级常量（见 `internal/memory/index/hybrid.go`），便于调参与解释。

```
Rel = file.Weight
    + Σ_term ( pathHit·35 + kindHit·10 + min(contentCount,5)·12 )
    + keywordCount·4                       // 仅 from_symbol=0
    + SymbolScore(syms, terms, intent)     // 单文件封顶 60
    + intentBoost·20                       // 路径段命中查询意图，命中即加一次
    + roleBoost()                          // 当前恒为 0（dormant）
    − sizePenalty                          // >32KB 起，封顶 20
```

其中 `SymbolScore`：

```
SymbolScore = Σ_sym ( 精确名命中·30 | 部分名命中·15 ) + Σ_sym[kind∈intent] ( sym.Weight·0.25 )
            ，整体 min(·, 60)
```

### 噪声治理

`isGeneratedNoisy` 以路径规则临时判定生成/噪声文件（`components.d.ts`、`auto-imports.d.ts`、`env.d.ts`、`shims-vue.d.ts` 及任意 `*.d.ts`）。命中且查询未显式触及其路径时 `Rel = 0`，直接踢出候选——满足「生成物默认不进 top-k，除非 query 明确要求」。

> 这是 role 落地前的过渡方案。role 工作包（W4 独立）落地后，generated-noisy 应由 role 判定取代路径启发式。

### role 休眠（dormant）

`relRoleBoost = 25` 常量已就位，但 `roleBoost()` 当前恒返回 0。职责信号目前完全由 `symbol_kind` 承载（`SymbolScore` 的 kind∈intent 项）。role 落地后只需在 `roleBoost()` 内读取文件 role 与意图比较，**Rel 公式其余部分无需改动**。

## 意图识别

`detectIntent` 从查询文本（小写）匹配 `intentRules` 触发词（中英双语），产出期望的 `symbol kinds` 与 `pathSegs`。规则**只加成、不过滤**，避免误伤召回。当前规则覆盖：路由、组件/页面、store/状态、service/接口、controller/endpoint、repository/dao/mapper、composable/hook。命令类（build/test）已由命令发现覆盖，不在此参与文件 rerank。

## MMR 多样性 rerank

`rerankMMR` 先将候选 Rel 线性归一化到 [0,1]，再贪心选取：

```
MMR(d) = λ·relNorm(d) − (1−λ)·max_{s∈Selected} Sim(d, s)        λ = 0.7
Sim(a, b) = 0.5·dirSim + 0.3·kindRoleSim + 0.2·keywordJaccard
```

- `dirSim`：共同前缀目录段占比，衡量“同目录”聚集度。
- `kindRoleSim`：role 落地前仅比较 kind（相同记 1）。
- `keywordJaccard`：两文件词元集合的 Jaccard。

平局时以更高 Rel、再以路径字典序决断，保证**结果确定性**。λ=0.7 偏相关，仅在相关性接近时用多样性打破同目录扎堆。

## 降级前提

- FTS5 运行时探测不可用时，`queryFTS` 短路，`queryKeywordFallback` 用 `recallPaths` 取候选集、`keywordCounts`（`from_symbol=0`）取计数。
- `hybridRank` 对召回来源**透明**：无论 FTS 还是 keyword 回退，Rel 计算与 MMR rerank 行为一致，降级不阻断、不改变排序语义。
- 符号加载 `loadSymbols` 批量 `IN` 查询，避免 N+1。

## 延迟预算

- 索引期：每文件多一次 ≤16KB 头部读取 + 正则提取 + 符号行 delete-then-insert，随文件数线性增长，仍在 `SyncScan` 单事务内。
- 查询期：召回候选数 = `topK · recallMultiplier(8)`，符号一次性批量加载，Rel 为 O(候选·词项)，MMR 为 O(topK·候选)。候选规模小，**不引入 UsageMeter p95 回归**。

## 验收

- **命中率闸门**：四套框架 fixtures（`testdata/retrieval-fixtures/{vue,angular,react,spring}`）各配 query→期望命中集合，Top-5 命中率 ≥ 80%（`hitRateGate`）。
- **降级一致性**：同一闸门在 `fts5Enabled=false`（keyword 回退）下复测通过，证明 rerank 对召回来源透明。
- **红线**：若 symbol+intent+path+NoisePenalty 在**不依赖 role** 的前提下达不到 80%，必须停下来上报真实命中率与瓶颈分析，**禁止靠调系数硬凑**（可观测 / 先证明不亏 优先于过线）。
- 当前实测：FTS5 与 keyword 回退两路均通过，总体 Top-5 命中率 95.5%（21/22）。

## 非目标

W4 本期不实现：完整 framework profile、role 分类与 `roleBoost` 激活、symbol index 深化（跨文件引用、调用图）、语义/向量召回、LSP 接入。`symbol` 表、`from_symbol` 列、`relRoleBoost` 常量为这些能力预留接口，但当前不承诺其行为。
