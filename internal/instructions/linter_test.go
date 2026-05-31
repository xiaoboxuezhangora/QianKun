package instructions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLintReportsFindingsWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	content := `# Rules

Use this token = abcdefghijklmnop carefully.

Temporary note from 2026-05-29.
`
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	report, err := Lint(Options{Root: root})
	if err != nil {
		t.Fatalf("Lint failed: %v", err)
	}
	if report.FilesRead != 1 || len(report.Findings) == 0 {
		t.Fatalf("expected findings, got %+v", report)
	}
	if !hasFinding(report, "secret_pattern") || !hasFinding(report, "dynamic_info") || !hasFinding(report, "missing_scope") {
		t.Fatalf("expected secret, dynamic info, and missing scope findings, got %+v", report.Findings)
	}

	// 至少标记 3 类异味，且每条都带有严重度与可执行修复建议（report-only：只提示不改写）。
	for _, rule := range []string{"secret_pattern", "dynamic_info", "missing_scope"} {
		finding, ok := findingByRule(report, rule)
		if !ok {
			t.Fatalf("missing finding for rule %q", rule)
		}
		if finding.Severity == "" {
			t.Fatalf("rule %q has empty severity", rule)
		}
		if finding.Suggestion == "" {
			t.Fatalf("rule %q is missing an actionable suggestion", rule)
		}
	}

	// secret_pattern 是最高严重度，排序后应位于首条。
	if report.Findings[0].Severity != SeverityHigh || report.Findings[0].Rule != "secret_pattern" {
		t.Fatalf("expected highest-severity secret finding first, got %+v", report.Findings[0])
	}
}

func hasFinding(report Report, rule string) bool {
	_, ok := findingByRule(report, rule)
	return ok
}

func findingByRule(report Report, rule string) (Finding, bool) {
	for _, finding := range report.Findings {
		if finding.Rule == rule {
			return finding, true
		}
	}
	return Finding{}, false
}
