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

	fmt.Fprintln(&buf, "## Instructions Lint")
	if instructionsErr != nil {
		fmt.Fprintf(&buf, "- Status: degraded: %s\n", instructionsErr)
	} else {
		fmt.Fprintf(&buf, "- Files read: %d\n", instructionsReport.FilesRead)
		fmt.Fprintf(&buf, "- Findings: %d\n", len(instructionsReport.Findings))
		if len(instructionsReport.Findings) > 0 {
			fmt.Fprintln(&buf)
			fmt.Fprintln(&buf, "| Severity | Rule | File | Line | Message |")
			fmt.Fprintln(&buf, "| --- | --- | --- | ---: | --- |")
			for _, finding := range instructionsReport.Findings {
				fmt.Fprintf(&buf, "| %s | %s | `%s` | %d | %s |\n",
					finding.Severity, finding.Rule, finding.File, finding.Line, escapeTable(finding.Message))
			}
		}
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

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "keyword fallback"
}

func escapeTable(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
