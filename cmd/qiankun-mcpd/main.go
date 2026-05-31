package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/commands"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/index"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
	"github.com/xiaoboxuezhangora/QianKun/internal/toolcache"
	"github.com/xiaoboxuezhangora/QianKun/internal/usage"
	"github.com/xiaoboxuezhangora/QianKun/internal/weekly"
)

const version = "0.4.0-w4"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "--version":
		if len(args) != 1 {
			printUsage(stderr)
			return 2
		}
		fmt.Fprintln(stdout, version)
		return 0
	case "--health":
		if len(args) != 1 {
			printUsage(stderr)
			return 2
		}
		if err := writeHealth(stdout); err != nil {
			fmt.Fprintf(stderr, "health check failed: %v\n", err)
			return 1
		}
		return 0
	case "memory-scan":
		return runMemoryScan(args[1:], stdout, stderr)
	case "memory-query":
		return runMemoryQuery(args[1:], stdout, stderr)
	case "usage-report":
		return runUsageReport(args[1:], stdout, stderr)
	case "weekly-report":
		return runWeeklyReport(args[1:], stdout, stderr)
	case "mcp":
		return runMCP(args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return 2
	}
}

type healthResponse struct {
	Status    string         `json:"status"`
	Toolcache toolcacheStats `json:"toolcache"`
	Usage     usageStats     `json:"usage"`
}

type toolcacheStats struct {
	Readiness string `json:"readiness"`
	Entries   int    `json:"entries"`
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Evictions uint64 `json:"evictions"`
	FilePath  string `json:"file_path"`
}

type usageStats struct {
	Readiness string `json:"readiness"`
	FilePath  string `json:"file_path"`
}

func writeHealth(w io.Writer) error {
	toolcacheStats := loadToolcacheStats()
	usageStats := loadUsageStats()
	return json.NewEncoder(w).Encode(healthResponse{
		Status:    "ready",
		Toolcache: toolcacheStats,
		Usage:     usageStats,
	})
}

func loadToolcacheStats() toolcacheStats {
	filePath, err := toolcache.DefaultFilePath()
	if err != nil {
		return toolcacheStats{Readiness: "degraded"}
	}

	store, err := toolcache.Open(toolcache.Options{FilePath: filePath})
	if err != nil {
		return toolcacheStats{
			Readiness: "degraded",
			FilePath:  filePath,
		}
	}
	defer store.Close()

	stats := store.Stats()
	return toolcacheStats{
		Readiness: stats.Readiness,
		Entries:   stats.Entries,
		Hits:      stats.Hits,
		Misses:    stats.Misses,
		Evictions: stats.Evictions,
		FilePath:  stats.FilePath,
	}
}

func loadUsageStats() usageStats {
	filePath, err := usage.DefaultDBPath()
	if err != nil {
		return usageStats{Readiness: "degraded"}
	}
	store, err := usage.Open(filePath)
	if err != nil {
		return usageStats{Readiness: "degraded", FilePath: filePath}
	}
	defer store.Close()
	return usageStats{Readiness: "ready", FilePath: filePath}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: qiankun-mcpd [--version|--health]")
	fmt.Fprintln(w, "       qiankun-mcpd memory-scan --root <path> --format json [--include pattern] [--exclude pattern]")
	fmt.Fprintln(w, "       qiankun-mcpd memory-scan <path>")
	fmt.Fprintln(w, "       qiankun-mcpd memory-query --root <path> --query <text> [--top-k 8]")
	fmt.Fprintln(w, "       qiankun-mcpd usage-report")
	fmt.Fprintln(w, "       qiankun-mcpd weekly-report --format markdown --instructions-root <path> [--output file]")
	fmt.Fprintln(w, "       qiankun-mcpd mcp")
}

func runMemoryScan(args []string, stdout io.Writer, stderr io.Writer) int {
	start := time.Now()
	var root string
	var format string
	var include repeatedFlag
	var exclude repeatedFlag

	fs := flag.NewFlagSet("memory-scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&root, "root", "", "repository root to scan")
	fs.StringVar(&format, "format", "json", "output format; only json is supported in W3")
	fs.Var(&include, "include", "include glob pattern; may be repeated")
	fs.Var(&exclude, "exclude", "exclude glob pattern; may be repeated")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: qiankun-mcpd memory-scan --root <path> --format json [--include pattern] [--exclude pattern]")
		fmt.Fprintln(stderr, "       qiankun-mcpd memory-scan <path>")
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	remaining := fs.Args()
	if root == "" && len(remaining) == 1 {
		root = remaining[0]
	} else if len(remaining) > 0 {
		fs.Usage()
		return 2
	}
	if root == "" {
		fs.Usage()
		return 2
	}
	if format != "json" {
		fmt.Fprintf(stderr, "unsupported memory-scan format %q; only json is supported\n", format)
		return 2
	}

	result, err := scan.Scan(scan.Options{
		Root:    root,
		Include: []string(include),
		Exclude: []string(exclude),
	})
	if err != nil {
		fmt.Fprintf(stderr, "memory-scan failed: %v\n", err)
		return 1
	}

	if _, err := syncMemoryIndex(result); err != nil {
		fmt.Fprintf(stderr, "memory index degraded: %v\n", err)
	}
	recordUsageEvents(stderr,
		usage.Event{Type: usage.EventCall, Tool: "memory-scan", Root: result.Root},
		usage.Event{Type: usage.EventTokenEstimate, Tool: "memory-scan", Root: result.Root, TokenEstimate: result.Totals.EstimatedTokens},
		usage.Event{Type: usage.EventLatency, Tool: "memory-scan", Root: result.Root, LatencyMS: time.Since(start).Milliseconds()},
	)

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "write memory-scan output failed: %v\n", err)
		return 1
	}
	return 0
}

func runMemoryQuery(args []string, stdout io.Writer, stderr io.Writer) int {
	var root string
	var query string
	var topK int

	fs := flag.NewFlagSet("memory-query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&root, "root", "", "repository root to query")
	fs.StringVar(&query, "query", "", "query text")
	fs.IntVar(&topK, "top-k", 8, "number of results")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: qiankun-mcpd memory-query --root <path> --query <text> [--top-k 8]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if root == "" || query == "" || len(fs.Args()) > 0 {
		fs.Usage()
		return 2
	}
	if topK <= 0 {
		fmt.Fprintln(stderr, "memory-query --top-k must be greater than 0")
		return 2
	}

	response, err := queryMemory(root, query, topK, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "memory-query failed: %v\n", err)
		return 1
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		fmt.Fprintf(stderr, "write memory-query output failed: %v\n", err)
		return 1
	}
	return 0
}

// queryMemory 执行一次 Memory Index 检索的完整逻辑：扫描 → 同步索引 → 检索 →
// 命令发现，并以非阻断方式记录 usage 事件。CLI 与 MCP 路径共用此函数，不重复实现。
// 入参 topK 由调用方负责校验/clamp；usage 记录或命令发现失败只写 stderr，不影响返回。
func queryMemory(root, query string, topK int, stderr io.Writer) (index.QueryResponse, error) {
	start := time.Now()

	result, err := scan.Scan(scan.Options{Root: root})
	if err != nil {
		return index.QueryResponse{}, fmt.Errorf("scan failed: %w", err)
	}
	store, err := index.Open(index.Options{})
	if err != nil {
		return index.QueryResponse{}, fmt.Errorf("index open failed: %w", err)
	}
	defer store.Close()

	syncStats, err := store.SyncScan(result)
	if err != nil {
		return index.QueryResponse{}, fmt.Errorf("index sync failed: %w", err)
	}
	response, err := store.Query(result.Root, query, topK)
	if err != nil {
		return index.QueryResponse{}, fmt.Errorf("query failed: %w", err)
	}

	// 命令发现：以非阻断方式附加真实可用命令作为查询上下文，失败只告警不影响主输出。
	if disc, derr := commands.Discover(result.Root); derr != nil {
		fmt.Fprintf(stderr, "memory-query command discovery failed: %v\n", derr)
	} else {
		response.Commands = disc.Commands
	}

	resultTokens := sumResultTokens(response.Results)
	ignoredTokens := result.Totals.EstimatedTokens - resultTokens
	if ignoredTokens < 0 {
		ignoredTokens = 0
	}
	events := []usage.Event{
		{Type: usage.EventCall, Tool: "memory-query", Root: result.Root},
		{Type: usage.EventTokenEstimate, Tool: "memory-query", Root: result.Root, TokenEstimate: resultTokens, SentContextTokensEstimate: resultTokens, IgnoredTokensEstimate: ignoredTokens},
		{Type: usage.EventLatency, Tool: "memory-query", Root: result.Root, LatencyMS: time.Since(start).Milliseconds()},
	}
	if syncStats.Changed() {
		events = append(events, usage.Event{Type: usage.EventCacheMiss, Tool: "memory-query", Root: result.Root})
	} else {
		events = append(events, usage.Event{
			Type:                usage.EventCacheHit,
			Tool:                "memory-query",
			Root:                result.Root,
			SavedTokens:         result.Totals.EstimatedTokens,
			CacheAvoidedTokens:  result.Totals.EstimatedTokens,
			AdjustedSavedTokens: ignoredTokens,
		})
	}
	recordUsageEvents(stderr, events...)

	return response, nil
}

func runUsageReport(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 0 {
		printUsage(stderr)
		return 2
	}
	report, err := usageReport()
	if err != nil {
		fmt.Fprintf(stderr, "usage-report failed: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "write usage-report output failed: %v\n", err)
		return 1
	}
	return 0
}

// usageReport 打开 UsageMeter 并生成聚合报告，CLI 与 MCP 路径共用。
func usageReport() (usage.Report, error) {
	store, err := usage.Open("")
	if err != nil {
		return usage.Report{}, err
	}
	defer store.Close()
	return store.Report()
}

func runWeeklyReport(args []string, stdout io.Writer, stderr io.Writer) int {
	var format string
	var root string
	var output string

	fs := flag.NewFlagSet("weekly-report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&format, "format", "markdown", "output format; only markdown is supported in W3")
	fs.StringVar(&root, "instructions-root", ".", "root directory for instructions lint and memory stats")
	fs.StringVar(&output, "output", "", "optional output file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: qiankun-mcpd weekly-report --format markdown --instructions-root <path> [--output file]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fs.Usage()
		return 2
	}
	if format != "markdown" {
		fmt.Fprintf(stderr, "unsupported weekly-report format %q; only markdown is supported\n", format)
		return 2
	}
	report, err := weekly.Markdown(weekly.Options{InstructionsRoot: root})
	if err != nil {
		fmt.Fprintf(stderr, "weekly-report failed: %v\n", err)
		return 1
	}
	if output != "" {
		if err := os.WriteFile(output, []byte(report), 0o644); err != nil {
			fmt.Fprintf(stderr, "write weekly-report output failed: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprint(stdout, report)
	return 0
}

func syncMemoryIndex(result scan.Result) (index.SyncStats, error) {
	store, err := index.Open(index.Options{})
	if err != nil {
		return index.SyncStats{}, err
	}
	defer store.Close()
	return store.SyncScan(result)
}

func recordUsageEvents(stderr io.Writer, events ...usage.Event) {
	store, err := usage.Open("")
	if err != nil {
		fmt.Fprintf(stderr, "usage degraded: %v\n", err)
		return
	}
	defer store.Close()
	for _, event := range events {
		if err := store.Record(event); err != nil {
			fmt.Fprintf(stderr, "usage degraded: %v\n", err)
			return
		}
	}
}

func sumResultTokens(results []index.QueryResult) int {
	total := 0
	for _, result := range results {
		total += result.TokenEstimate
	}
	return total
}

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
