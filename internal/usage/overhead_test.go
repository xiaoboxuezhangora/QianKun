package usage

import "testing"

func TestSavedVsOverheadDerivation(t *testing.T) {
	// 节省 1000、overhead 40：净收益 960，占比 4% ≤ 5%，应判为达标。
	m := Report{
		EstimatedSavedTokens:      1000,
		SentContextTokensEstimate: 40,
		CacheAvoidedTokens:        1000,
		IgnoredTokensEstimate:     200,
		AdjustedSavedTokens:       960,
	}.SavedVsOverhead()

	if m.NetSavedTokens != 960 {
		t.Fatalf("net saved = %d, want 960", m.NetSavedTokens)
	}
	if m.BaselineTokens != 1040 {
		t.Fatalf("baseline = %d, want 1040", m.BaselineTokens)
	}
	if m.OverheadRatio != 0.04 {
		t.Fatalf("overhead ratio = %v, want 0.04", m.OverheadRatio)
	}
	if !m.WithinBudget {
		t.Fatalf("expected within budget for 4%% overhead")
	}
}

func TestSavedVsOverheadWarnsAboveBudget(t *testing.T) {
	// 节省 300、overhead 120：占比 40% 远超 5%，应判为超预算告警。
	m := Report{
		EstimatedSavedTokens:      300,
		SentContextTokensEstimate: 120,
	}.SavedVsOverhead()

	if m.NetSavedTokens != 180 {
		t.Fatalf("net saved = %d, want 180", m.NetSavedTokens)
	}
	if m.WithinBudget {
		t.Fatalf("expected over-budget for 40%% overhead")
	}
}

func TestSavedVsOverheadEdgeCases(t *testing.T) {
	// 完全无活动：无节省无 overhead，不应误报超预算。
	idle := Report{}.SavedVsOverhead()
	if !idle.WithinBudget || idle.OverheadRatio != 0 || idle.ReductionRatio != 0 {
		t.Fatalf("idle report should be within budget with zero ratios: %+v", idle)
	}

	// 有 overhead 却无节省：违反“overhead 低于节省量”，应告警。
	leak := Report{SentContextTokensEstimate: 50}.SavedVsOverhead()
	if leak.WithinBudget {
		t.Fatalf("overhead without saving should be over budget: %+v", leak)
	}
	if leak.NetSavedTokens != -50 {
		t.Fatalf("net saved = %d, want -50", leak.NetSavedTokens)
	}
}
