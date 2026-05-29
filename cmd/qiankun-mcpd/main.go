package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
	"github.com/xiaoboxuezhangora/QianKun/internal/toolcache"
)

const version = "0.2.0-w2"

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
	default:
		printUsage(stderr)
		return 2
	}
}

type healthResponse struct {
	Status    string         `json:"status"`
	Toolcache toolcacheStats `json:"toolcache"`
}

type toolcacheStats struct {
	Readiness string `json:"readiness"`
	Entries   int    `json:"entries"`
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Evictions uint64 `json:"evictions"`
	FilePath  string `json:"file_path"`
}

func writeHealth(w io.Writer) error {
	stats := loadToolcacheStats()
	return json.NewEncoder(w).Encode(healthResponse{
		Status:    "ready",
		Toolcache: stats,
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

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: qiankun-mcpd [--version|--health]")
	fmt.Fprintln(w, "       qiankun-mcpd memory-scan --root <path> --format json [--include pattern] [--exclude pattern]")
	fmt.Fprintln(w, "       qiankun-mcpd memory-scan <path>")
}

func runMemoryScan(args []string, stdout io.Writer, stderr io.Writer) int {
	var root string
	var format string
	var include repeatedFlag
	var exclude repeatedFlag

	fs := flag.NewFlagSet("memory-scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&root, "root", "", "repository root to scan")
	fs.StringVar(&format, "format", "json", "output format; only json is supported in W2")
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

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "write memory-scan output failed: %v\n", err)
		return 1
	}
	return 0
}

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
