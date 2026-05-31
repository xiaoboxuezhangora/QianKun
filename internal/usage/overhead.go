package usage

// OverheadBudgetRatio 是设计原则规定的乾坤袋自身 overhead 上限：
// “乾坤袋自身 overhead 必须低于节省量并被持续报告（目标 ≤ 节省的 5%）”。
const OverheadBudgetRatio = 0.05

// SavedVsOverhead 给出“节省 vs 开销”并排视图所需的派生指标。
// 所有字段都可由 Report 现有 UsageMeter 字段直接解释，不引入无法追溯的估算：
//   - GrossSavedTokens / OverheadTokens / CacheAvoidedTokens /
//     IgnoredTokens / AdjustedSavedTokens 均为对应字段的原值；
//   - NetSavedTokens / BaselineTokens / OverheadRatio / ReductionRatio
//     仅由上述字段做加减、除法得到，可逐项回溯。
type SavedVsOverhead struct {
	GrossSavedTokens    int     // = estimated_saved_tokens，乾坤袋估算节省总量（缓存/重复上下文避免）
	OverheadTokens      int     // = sent_context_tokens_estimate，乾坤袋自身注入/发送上下文的开销
	CacheAvoidedTokens  int     // = cache_avoided_tokens，其中由缓存命中避免的部分
	IgnoredTokens       int     // = ignored_tokens_estimate，噪声治理额外过滤的 token（单列参考，不计入净收益）
	AdjustedSavedTokens int     // = adjusted_saved_tokens，UsageMeter 逐事件记录的调整后节省，用于交叉校验
	NetSavedTokens      int     // = GrossSavedTokens - OverheadTokens，净收益
	BaselineTokens      int     // = GrossSavedTokens + OverheadTokens，无乾坤袋时该工作负载预计发送的上下文
	OverheadRatio       float64 // = OverheadTokens / GrossSavedTokens，开销占节省比例（saved=0 时为 0）
	ReductionRatio      float64 // = GrossSavedTokens / BaselineTokens，相对基线降幅（baseline=0 时为 0）
	WithinBudget        bool    // OverheadRatio 是否 ≤ OverheadBudgetRatio
}

// SavedVsOverhead 从报表派生“节省 vs 开销”并排视图与基线对照。
func (r Report) SavedVsOverhead() SavedVsOverhead {
	s := SavedVsOverhead{
		GrossSavedTokens:    r.EstimatedSavedTokens,
		OverheadTokens:      r.SentContextTokensEstimate,
		CacheAvoidedTokens:  r.CacheAvoidedTokens,
		IgnoredTokens:       r.IgnoredTokensEstimate,
		AdjustedSavedTokens: r.AdjustedSavedTokens,
	}
	s.NetSavedTokens = s.GrossSavedTokens - s.OverheadTokens
	s.BaselineTokens = s.GrossSavedTokens + s.OverheadTokens
	if s.BaselineTokens > 0 {
		s.ReductionRatio = float64(s.GrossSavedTokens) / float64(s.BaselineTokens)
	}
	if s.GrossSavedTokens > 0 {
		s.OverheadRatio = float64(s.OverheadTokens) / float64(s.GrossSavedTokens)
		s.WithinBudget = s.OverheadRatio <= OverheadBudgetRatio
	} else {
		// 无节省时除零无意义：完全无活动（overhead=0）视为达标；
		// 若有 overhead 却无节省，则违反“overhead 低于节省量”，判为超预算并告警。
		s.WithinBudget = s.OverheadTokens == 0
	}
	return s
}
