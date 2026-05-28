package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/xiaoboxuezhangora/QianKun/internal/toolcache"
)

const version = "0.1.0-w1"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "--health":
		if err := writeHealth(stdout); err != nil {
			fmt.Fprintf(stderr, "health check failed: %v\n", err)
			return 1
		}
		return 0
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
}
