package index

import (
	"fmt"
	"math"
	"strings"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/symbols"
)

// ============================================================================
// Hybrid Retrieval 打分与 rerank 的全部可调参数，集中为包级常量便于调参与解释。
// 任何排序结论都应能由这些常量逐项还原（可观测原则）。
// ============================================================================
const (
	// ---- Rel 词法项（沿用既有 scoreFile 语义）----
	relPathHit       = 35.0 // 查询词出现在路径
	relKindHit       = 10.0 // 查询词出现在 kind
	relContentUnit   = 12.0 // 查询词在正文每命中一次的加分
	relContentCap    = 5    // 单词正文命中计数封顶，防长文堆分
	relKeywordUnit   = 4.0  // keywordCount（仅基础词项）每命中一次加分

	// ---- Rel 符号项 ----
	relSymbolExact    = 30.0 // 查询词精确等于符号名
	relSymbolPartial  = 15.0 // 查询词为符号名子串
	relSymbolKindUnit = 0.25 // 符号 kind 命中查询意图时，按符号自身 weight 比例加成
	relSymbolCap      = 60.0 // 单文件符号项总封顶，防符号多的大文件霸榜

	// ---- Rel 意图 / role ----
	relIntentBoost = 20.0 // 文件路径段命中查询意图
	relRoleBoost   = 25.0 // 文件 role 与查询意图一致时加分（role 为空时不加成）

	// ---- Rel 体积惩罚（沿用）----
	relSizePenaltyThreshold = 32 * 1024 // 超过该字节数开始惩罚
	relSizePenaltyCap       = 20.0      // 体积惩罚封顶

	// ---- MMR 多样性 rerank ----
	mmrLambda      = 0.7 // 相关性 vs 多样性权衡，越大越偏相关
	mmrSimDir      = 0.5 // Sim 中“同目录”权重
	mmrSimKindRole = 0.3 // Sim 中“同 kind/role”权重（role 落地前仅用 kind）
	mmrSimJaccard  = 0.2 // Sim 中“关键词 Jaccard”权重

	// ---- 召回 ----
	recallMultiplier = 8 // 召回候选数 = topK * 该倍数
)

// queryIntent 表示从查询文本解析出的检索意图：希望命中哪些符号类别、路径段与文件 role。
type queryIntent struct {
	kinds    map[symbols.Kind]bool // 期望的符号类别
	pathSegs []string              // 期望出现在路径中的目录段
	roles    map[string]bool       // 期望的文件 role（与 scan.Role* 对齐）
}

// intentRule 把触发词映射到一组符号类别、路径段与文件 role（中英双语触发词）。
type intentRule struct {
	triggers []string
	kinds    []symbols.Kind
	segs     []string
	roles    []string
}

// intentRules 是意图识别规则表。命令类（build/test）已由命令发现覆盖，不在此参与文件 rerank。
var intentRules = []intentRule{
	{[]string{"路由", "route", "router", "routing"}, []symbols.Kind{symbols.KindRoute}, []string{"router", "routes"}, []string{scan.RoleRouteDefinition}},
	{[]string{"组件", "component", "页面", "page", "view", "视图"}, []symbols.Kind{symbols.KindComponent}, []string{"components", "pages", "views"}, []string{scan.RolePageView, scan.RoleComponent}},
	{[]string{"状态", "store", "状态管理", "pinia", "vuex", "redux"}, []symbols.Kind{symbols.KindStore}, []string{"store", "stores"}, []string{scan.RoleStore}},
	{[]string{"服务", "接口", "api", "service", "http", "请求"}, []symbols.Kind{symbols.KindService, symbols.KindAPIMethod}, []string{"services", "service", "api"}, []string{scan.RoleService}},
	{[]string{"controller", "控制器", "endpoint", "mapping"}, []symbols.Kind{symbols.KindController, symbols.KindEndpoint}, []string{"controller", "web"}, []string{scan.RoleController}},
	{[]string{"repository", "dao", "mapper", "持久化", "仓储"}, []symbols.Kind{symbols.KindRepository}, []string{"repository", "dao", "mapper"}, []string{scan.RoleRepository}},
	{[]string{"composable", "hook", "钩子"}, []symbols.Kind{symbols.KindComposable, symbols.KindHook}, []string{"composables", "hooks"}, nil},
}

// detectIntent 从原始查询文本（小写）解析意图集合。规则只“加成”不“过滤”，避免误伤召回。
func detectIntent(query string) queryIntent {
	lower := strings.ToLower(query)
	intent := queryIntent{kinds: map[symbols.Kind]bool{}, roles: map[string]bool{}}
	segSeen := map[string]bool{}
	for _, rule := range intentRules {
		hit := false
		for _, t := range rule.triggers {
			if strings.Contains(lower, strings.ToLower(t)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, k := range rule.kinds {
			intent.kinds[k] = true
		}
		for _, seg := range rule.segs {
			if !segSeen[seg] {
				segSeen[seg] = true
				intent.pathSegs = append(intent.pathSegs, seg)
			}
		}
		for _, role := range rule.roles {
			intent.roles[role] = true
		}
	}
	return intent
}

// scoredFile 是参与 rerank 的候选文件，携带相关性 Rel 与多样性所需的特征。
type scoredFile struct {
	file     indexedFile
	syms     []symbols.Symbol
	rel      float64
	relNorm  float64
	segments []string        // 路径段，用于 dirSim
	termSet  map[string]bool // 词元集合，用于 Jaccard
}

// computeRel 计算单个候选的相关性 Rel（rerank 前的相关性，QueryResult.Score 即取此值）。
func computeRel(file indexedFile, syms []symbols.Symbol, terms []string, intent queryIntent) float64 {
	lowerPath := strings.ToLower(file.Path)

	// 噪声惩罚：generated-noisy 文件且查询未显式命中其路径，直接踢出（Rel=0），
	// 满足“generated/noisy 默认不进 top-k，除非明确要求”。优先以真实 role 判定，
	// 仅当 role 缺失（旧索引行）时回退到路径启发。
	if isGeneratedNoisy(file.Role, lowerPath) && !anyTermInPath(lowerPath, terms) {
		return 0
	}

	// 词法项（与既有 scoreFile 对齐）。
	score := float64(file.Weight)
	lowerKind := strings.ToLower(file.Kind)
	lowerContent := strings.ToLower(file.ContentSample)
	for _, term := range terms {
		if strings.Contains(lowerPath, term) {
			score += relPathHit
		}
		if strings.Contains(lowerKind, term) {
			score += relKindHit
		}
		if count := strings.Count(lowerContent, term); count > 0 {
			score += math.Min(float64(count), relContentCap) * relContentUnit
		}
	}
	score += float64(file.KeywordCount) * relKeywordUnit

	// 符号项。
	score += symbolScore(syms, terms, intent)

	// 意图项：路径段命中查询意图。
	for _, seg := range intent.pathSegs {
		if strings.Contains(lowerPath, "/"+seg) || strings.HasPrefix(lowerPath, seg+"/") || strings.Contains(lowerPath, seg+"/") {
			score += relIntentBoost
			break
		}
	}

	// role 项：文件 role 与查询意图一致时加成。同 role 文件等量加分，不改变组内相对次序，
	// 只整体抬升“职责正确”的一类文件。
	score += roleBoost(file.Role, intent)

	// 体积惩罚。
	if file.SizeBytes > relSizePenaltyThreshold {
		score -= math.Min(float64(file.SizeBytes)/float64(relSizePenaltyThreshold), relSizePenaltyCap)
	}
	if score < 0 {
		return 0
	}
	return score
}

// symbolScore 计算符号项：精确/部分名命中 + 符号 kind 命中意图，单文件封顶。
func symbolScore(syms []symbols.Symbol, terms []string, intent queryIntent) float64 {
	var score float64
	for _, sym := range syms {
		name := strings.ToLower(sym.Name)
		matched := false
		for _, term := range terms {
			switch {
			case name == term:
				score += relSymbolExact
				matched = true
			case !matched && strings.Contains(name, term):
				score += relSymbolPartial
				matched = true
			}
		}
		if intent.kinds[sym.Kind] {
			score += relSymbolKindUnit * float64(sym.Weight)
		}
	}
	return math.Min(score, relSymbolCap)
}

// roleBoost 在文件 role 命中查询意图期望的 role 集合时返回 relRoleBoost，否则 0。
// role 为空（旧索引行或无法归类的源码）或意图未指定 role 时不加成。
func roleBoost(role string, intent queryIntent) float64 {
	if role == "" || len(intent.roles) == 0 {
		return 0
	}
	if intent.roles[role] {
		return relRoleBoost
	}
	return 0
}

// isGeneratedNoisy 判定文件是否为生成/噪声：优先采信扫描期分配的真实 role，
// 仅当 role 缺失（旧索引行）时回退到路径启发，保证旧库行为不变。
func isGeneratedNoisy(role, lowerPath string) bool {
	if role != "" {
		return role == scan.RoleGeneratedNoisy
	}
	base := lowerPath
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return strings.HasSuffix(base, ".d.ts")
}

func anyTermInPath(lowerPath string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(lowerPath, term) {
			return true
		}
	}
	return false
}

// rerankMMR 对候选按 MMR 选出 topK：先按归一化 Rel 贪心起步，
// 之后每轮在剩余候选中选 MMR 最高者，兼顾相关性与多样性。
func rerankMMR(cands []scoredFile, topK int) []scoredFile {
	if len(cands) == 0 {
		return cands
	}
	normalizeRel(cands)
	if topK > len(cands) {
		topK = len(cands)
	}

	selected := make([]scoredFile, 0, topK)
	used := make([]bool, len(cands))
	for len(selected) < topK {
		bestIdx := -1
		bestScore := math.Inf(-1)
		for i := range cands {
			if used[i] {
				continue
			}
			var maxSim float64
			for j := range selected {
				if sim := similarity(cands[i], selected[j]); sim > maxSim {
					maxSim = sim
				}
			}
			mmr := mmrLambda*cands[i].relNorm - (1-mmrLambda)*maxSim
			// 平局时以更高 Rel、再以路径字典序保证确定性。
			if mmr > bestScore ||
				(mmr == bestScore && bestIdx >= 0 &&
					(cands[i].rel > cands[bestIdx].rel ||
						(cands[i].rel == cands[bestIdx].rel && cands[i].file.Path < cands[bestIdx].file.Path))) {
				bestScore = mmr
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, cands[bestIdx])
	}
	return selected
}

// normalizeRel 把 Rel 线性归一化到 [0,1]，供 MMR 与多样性项同量纲比较。
func normalizeRel(cands []scoredFile) {
	min, max := math.Inf(1), math.Inf(-1)
	for _, c := range cands {
		if c.rel < min {
			min = c.rel
		}
		if c.rel > max {
			max = c.rel
		}
	}
	span := max - min
	for i := range cands {
		if span <= 0 {
			cands[i].relNorm = 1
			continue
		}
		cands[i].relNorm = (cands[i].rel - min) / span
	}
}

// similarity 计算两文件相似度，用于 MMR 多样性惩罚。
func similarity(a, b scoredFile) float64 {
	return mmrSimDir*dirSim(a.segments, b.segments) +
		mmrSimKindRole*kindRoleSim(a.file, b.file) +
		mmrSimJaccard*jaccard(a.termSet, b.termSet)
}

// dirSim 以共同前缀目录段占比衡量“同目录”程度。
func dirSim(a, b []string) float64 {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	common := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
		common++
	}
	return float64(common) / float64(maxLen)
}

// kindRoleSim：role 落地前仅比较 kind（相同记 1）。role 落地后可并入 role 维度。
func kindRoleSim(a, b indexedFile) float64 {
	if a.Kind == b.Kind {
		return 1
	}
	return 0
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// loadSymbols 批量加载候选路径的符号，避免 N+1 查询。
func (s *Store) loadSymbols(root string, paths []string) (map[string][]symbols.Symbol, error) {
	result := make(map[string][]symbols.Symbol)
	if len(paths) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(paths))
	args := make([]any, 0, len(paths)+1)
	args = append(args, root)
	for i, p := range paths {
		placeholders[i] = "?"
		args = append(args, p)
	}
	rows, err := s.db.Query(fmt.Sprintf(
		`SELECT path, symbol_name, symbol_kind, weight FROM symbol WHERE root = ? AND path IN (%s)`,
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var path, name, kind string
		var weight int
		if err := rows.Scan(&path, &name, &kind, &weight); err != nil {
			return nil, err
		}
		result[path] = append(result[path], symbols.Symbol{Name: name, Kind: symbols.Kind(kind), Weight: weight})
	}
	return result, rows.Err()
}

// pathSegments 返回路径的目录段（不含文件名）。
func pathSegments(path string) []string {
	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return nil
	}
	return parts[:len(parts)-1]
}
