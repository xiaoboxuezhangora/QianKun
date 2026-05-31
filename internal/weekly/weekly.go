package weekly

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoboxuezhangora/QianKun/internal/instructions"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/index"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
	"github.com/xiaoboxuezhangora/QianKun/internal/usage"
)

const recentChangesLimit = 10

type Options struct {
	InstructionsRoot string
}

func Markdown(opts Options) (string, error) {
	root := opts.InstructionsRoot
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	memoryStats, memorySync, memoryChanges, memoryErr := buildMemoryStats(absRoot)
	usageReport, usageErr := buildUsageReport()
	instructionsReport, instructionsErr := instructions.Lint(instructions.Options{Root: absRoot})

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "# QianKun Weekly Report")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "- Generated at: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&buf, "- Instructions root: `%s`\n", absRoot)
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## Memory Index")
	if memoryErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", memoryErr)
	} else {
		fmt.Fprintf(&buf, "- DB path: `%s`\n", memoryStats.DBPath)
		fmt.Fprintf(&buf, "- FTS5: %s\n", enabledLabel(memoryStats.FTS5Enabled))
		fmt.Fprintf(&buf, "- Files indexed: %d\n", memoryStats.FilesIndexed)
		fmt.Fprintf(&buf, "- Estimated tokens: %d\n", memoryStats.EstimatedTokens)
		fmt.Fprintf(&buf, "- Incremental sync: updated=%d unchanged=%d deleted=%d read_errors=%d\n",
			memorySync.FilesUpdated, memorySync.FilesUnchanged, memorySync.FilesDeleted, memorySync.ReadErrors)
	}
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## Recent Changes")
	if memoryErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", memoryErr)
	} else if len(memoryChanges) == 0 {
		fmt.Fprintln(&buf, "- No recorded changes yet.")
	} else {
		fmt.Fprintf(&buf, "- Showing latest %d of recorded upsert/delete events.\n", len(memoryChanges))
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, "| Change | File | Changed at |")
		fmt.Fprintln(&buf, "| --- | --- | --- |")
		for _, change := range memoryChanges {
			fmt.Fprintf(&buf, "| %s | `%s` | %s |\n", change.ChangeType, change.Path, change.ChangedAt)
		}
	}
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## UsageMeter")
	if usageErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", usageErr)
	} else {
		fmt.Fprintf(&buf, "- DB path: `%s`\n", usageReport.DBPath)
		fmt.Fprintf(&buf, "- Total calls: %d\n", usageReport.TotalCalls)
		fmt.Fprintf(&buf, "- Cache hits: %d\n", usageReport.CacheHits)
		fmt.Fprintf(&buf, "- Cache misses: %d\n", usageReport.CacheMisses)
		fmt.Fprintf(&buf, "- Estimated tokens: %d\n", usageReport.EstimatedTokens)
		fmt.Fprintf(&buf, "- Estimated saved tokens: %d\n", usageReport.EstimatedSavedTokens)
		fmt.Fprintf(&buf, "- P95 latency ms: %d\n", usageReport.P95LatencyMS)
	}
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## Saved vs Overhead")
	if usageErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", usageErr)
	} else {
		writeSavedVsOverhead(&buf, usageReport.SavedVsOverhead())
	}
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## Instructions Lint")
	if instructionsErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", instructionsErr)
	} else {
		writeInstructionsLint(&buf, instructionsReport)
	}
	fmt.Fprintln(&buf)

	fmt.Fprintln(&buf, "## W3 Known Gaps")
	fmt.Fprintln(&buf, "- `recent_change` records minimal upsert/delete events, surfaced in the Recent Changes section above (latest N only).")
	fmt.Fprintln(&buf, "- `symbol` table exists for future work, but W3 does not perform LSP or framework symbol extraction.")
	fmt.Fprintln(&buf, "- FTS5 is used when the SQLite driver supports it; otherwise keyword/LIKE-style scoring is used.")
	fmt.Fprintln(&buf, "- W4+ items remain out of scope: full framework profile, hybrid rerank, MCP server, IDEA productization, and enterprise dashboard.")

	return buf.String(), nil
}

func buildMemoryStats(root string) (index.RootStats, index.SyncStats, []index.RecentChange, error) {
	store, err := index.Open(index.Options{})
	if err != nil {
		return index.RootStats{}, index.SyncStats{}, nil, err
	}
	defer store.Close()

	result, err := scan.Scan(scan.Options{Root: root})
	if err != nil {
		return index.RootStats{}, index.SyncStats{}, nil, err
	}
	syncStats, err := store.SyncScan(result)
	if err != nil {
		return index.RootStats{}, syncStats, nil, err
	}
	stats, err := store.RootStats(root)
	if err != nil {
		return stats, syncStats, nil, err
	}
	changes, err := store.RecentChanges(root, recentChangesLimit)
	return stats, syncStats, changes, err
}

func buildUsageReport() (usage.Report, error) {
	store, err := usage.Open("")
	if err != nil {
		return usage.Report{}, err
	}
	defer store.Close()
	return store.Report()
}

// writeSavedVsOverhead 渲染“节省 vs 开销”并排视图、基线对照、净收益与 overhead 占比，
// 并按设计原则校验 overhead ≤ 节省的 5%，超阈值时输出告警。
func writeSavedVsOverhead(buf *bytes.Buffer, m usage.SavedVsOverhead) {
	// 并排视图：左列节省、右列开销，二者均为可回溯的 UsageMeter 字段原值。
	fmt.Fprintln(buf, "| Side | Tokens | Source field |")
	fmt.Fprintln(buf, "| --- | ---: | --- |")
	fmt.Fprintf(buf, "| Saved | %d | estimated_saved_tokens |\n", m.GrossSavedTokens)
	fmt.Fprintf(buf, "| Overhead | %d | sent_context_tokens_estimate |\n", m.OverheadTokens)
	fmt.Fprintln(buf)

	// 净收益与 overhead 占比是本节的验收指标。
	fmt.Fprintf(buf, "- Net saved tokens (saved - overhead): %d\n", m.NetSavedTokens)
	fmt.Fprintf(buf, "- Overhead ratio (overhead / saved): %s\n", formatPercent(m.OverheadRatio))
	fmt.Fprintf(buf, "- Overhead budget (design principle): <= %s\n", formatPercent(usage.OverheadBudgetRatio))

	// 基线对照：无乾坤袋时该工作负载预计发送的上下文 = 已发送(overhead) + 已节省(saved)。
	fmt.Fprintf(buf, "- Baseline tokens (saved + overhead): %d\n", m.BaselineTokens)
	fmt.Fprintf(buf, "- Reduction vs baseline: %s\n", formatPercent(m.ReductionRatio))

	// 参考字段：缓存避免、噪声忽略，以及逐事件记录的调整后节省（交叉校验净收益口径）。
	fmt.Fprintf(buf, "- Cache avoided tokens (cache_avoided_tokens): %d\n", m.CacheAvoidedTokens)
	fmt.Fprintf(buf, "- Ignored tokens (ignored_tokens_estimate): %d\n", m.IgnoredTokens)
	fmt.Fprintf(buf, "- Adjusted saved tokens (adjusted_saved_tokens, cross-check): %d\n", m.AdjustedSavedTokens)

	if m.WithinBudget {
		fmt.Fprintf(buf, "- Budget check: OK, overhead %s within the %s budget.\n",
			formatPercent(m.OverheadRatio), formatPercent(usage.OverheadBudgetRatio))
	} else {
		fmt.Fprintf(buf, "- Budget check: WARNING: overhead %s exceeds the %s budget (saved=%d, overhead=%d).\n",
			formatPercent(m.OverheadRatio), formatPercent(usage.OverheadBudgetRatio), m.GrossSavedTokens, m.OverheadTokens)
	}
}

// writeInstructionsLint 以 warning 形式呈现指令异味：按严重度统计，逐条给出
// 严重度、规则、定位与可执行的修复建议。遵循 report-only 原则——只提示不擅自修改用户指令文件。
func writeInstructionsLint(buf *bytes.Buffer, report instructions.Report) {
	fmt.Fprintf(buf, "- Files read: %d\n", report.FilesRead)
	fmt.Fprintf(buf, "- Warnings: %d\n", len(report.Findings))
	if len(report.Findings) == 0 {
		fmt.Fprintln(buf, "- No instruction smells detected.")
		return
	}

	high, medium, low := 0, 0, 0
	for _, finding := range report.Findings {
		switch finding.Severity {
		case instructions.SeverityHigh:
			high++
		case instructions.SeverityMedium:
			medium++
		default:
			low++
		}
	}
	fmt.Fprintf(buf, "- By severity: HIGH=%d MEDIUM=%d LOW=%d\n", high, medium, low)
	fmt.Fprintln(buf, "- Mode: report-only (suggestions below are advisory; QianKun never rewrites your instruction files).")
	fmt.Fprintln(buf)

	fmt.Fprintln(buf, "| Severity | Rule | File | Line | Warning | Suggested fix |")
	fmt.Fprintln(buf, "| --- | --- | --- | ---: | --- | --- |")
	for _, finding := range report.Findings {
		fmt.Fprintf(buf, "| %s | %s | `%s` | %d | %s | %s |\n",
			finding.Severity, finding.Rule, finding.File, finding.Line,
			escapeTable(finding.Message), escapeTable(finding.Suggestion))
	}
}

func formatPercent(ratio float64) string {
	return fmt.Sprintf("%.1f%%", ratio*100)
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "keyword fallback"
}

func escapeTable(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
