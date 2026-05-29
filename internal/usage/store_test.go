package usage

import (
	"path/filepath"
	"testing"
)

func TestUsageReportAggregatesEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	events := []Event{
		{Type: EventCall, Tool: "memory-query"},
		{Type: EventCacheHit, Tool: "memory-query", SavedTokens: 100, CacheAvoidedTokens: 100, AdjustedSavedTokens: 90},
		{Type: EventCacheMiss, Tool: "memory-scan"},
		{Type: EventTokenEstimate, Tool: "memory-query", TokenEstimate: 40, SentContextTokensEstimate: 40, IgnoredTokensEstimate: 60},
		{Type: EventLatency, Tool: "memory-query", LatencyMS: 10},
		{Type: EventLatency, Tool: "memory-query", LatencyMS: 30},
	}
	for _, event := range events {
		if err := store.Record(event); err != nil {
			t.Fatalf("Record failed: %v", err)
		}
	}

	report, err := store.Report()
	if err != nil {
		t.Fatalf("Report failed: %v", err)
	}
	if report.TotalCalls != 1 || report.CacheHits != 1 || report.CacheMisses != 1 {
		t.Fatalf("unexpected call/cache counts: %+v", report)
	}
	if report.EstimatedTokens != 40 || report.EstimatedSavedTokens != 100 || report.CacheAvoidedTokens != 100 {
		t.Fatalf("unexpected token totals: %+v", report)
	}
	if report.P95LatencyMS != 30 {
		t.Fatalf("expected p95 latency 30, got %+v", report)
	}
}
