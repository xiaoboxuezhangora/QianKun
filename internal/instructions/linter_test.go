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
}

func hasFinding(report Report, rule string) bool {
	for _, finding := range report.Findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}
