package weekly

import (
	"strings"
	"testing"
)

func TestMarkdownIncludesW3Sections(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	out, err := Markdown(Options{InstructionsRoot: "../../testdata/memory-scan-fixture"})
	if err != nil {
		t.Fatalf("Markdown failed: %v", err)
	}
	for _, want := range []string{
		"# QianKun Weekly Report",
		"## Memory Index",
		"## Recent Changes",
		"## UsageMeter",
		"## Saved vs Overhead",
		"Net saved tokens (saved - overhead):",
		"Overhead ratio (overhead / saved):",
		"## Instructions Lint",
		"- Warnings:",
		"Suggested fix",
		"report-only",
		"## W3 Known Gaps",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in weekly report:\n%s", want, out)
		}
	}
}
