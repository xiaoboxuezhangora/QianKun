package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthOutput(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"--health"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}

	var payload struct {
		Status    string `json:"status"`
		Toolcache struct {
			Readiness string `json:"readiness"`
			FilePath  string `json:"file_path"`
		} `json:"toolcache"`
		Usage struct {
			Readiness string `json:"readiness"`
			FilePath  string `json:"file_path"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("health output is not JSON: %v; output=%q", err, stdout.String())
	}

	if payload.Status != "ready" {
		t.Fatalf("unexpected health status: %+v", payload)
	}
	if payload.Toolcache.Readiness == "" {
		t.Fatalf("expected toolcache readiness in health output: %+v", payload)
	}
	if payload.Toolcache.FilePath == "" {
		t.Fatalf("expected toolcache file path in health output: %+v", payload)
	}
	if payload.Usage.Readiness == "" || payload.Usage.FilePath == "" {
		t.Fatalf("expected usage readiness in health output: %+v", payload)
	}
}

func TestVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0.3.0-w3") {
		t.Fatalf("expected version in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestMemoryScanOutput(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := filepath.Join("..", "..", "testdata", "memory-scan-fixture")
	code := run([]string{"memory-scan", "--root", root, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}

	var payload struct {
		Root   string `json:"root"`
		Totals struct {
			FilesIndexed    int `json:"files_indexed"`
			FilesSkipped    int `json:"files_skipped"`
			EstimatedTokens int `json:"estimated_tokens"`
		} `json:"totals"`
		SkippedSummary map[string]struct {
			Count               int      `json:"count"`
			RepresentativePaths []string `json:"representative_paths"`
		} `json:"skipped_summary"`
		Files []struct {
			Path       string `json:"path"`
			Kind       string `json:"kind"`
			Skipped    bool   `json:"skipped"`
			SkipReason string `json:"skip_reason"`
		} `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("memory-scan output is not JSON: %v; output=%q", err, stdout.String())
	}
	if payload.Root == "" || payload.Totals.FilesIndexed == 0 || payload.Totals.EstimatedTokens == 0 {
		t.Fatalf("unexpected memory-scan totals: %+v", payload)
	}
	if _, ok := payload.SkippedSummary["build_artifact"]; !ok {
		t.Fatalf("expected build_artifact skipped summary, got %+v", payload.SkippedSummary)
	}

	var foundSource bool
	for _, file := range payload.Files {
		if file.Path == "src/main.ts" && file.Kind == "source" && !file.Skipped {
			foundSource = true
		}
	}
	if !foundSource {
		t.Fatalf("expected indexed src/main.ts in files: %+v", payload.Files)
	}
}

func TestMemoryScanShorthandPath(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := filepath.Join("..", "..", "testdata", "memory-scan-fixture")
	code := run([]string{"memory-scan", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"skipped_summary"`) {
		t.Fatalf("expected memory-scan JSON output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestMemoryScanUnsupportedFormat(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := filepath.Join("..", "..", "testdata", "memory-scan-fixture")
	code := run([]string{"memory-scan", "--root", root, "--format", "yaml"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code for unsupported format")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "unsupported memory-scan format") || !strings.Contains(got, "yaml") {
		t.Fatalf("expected clear unsupported format stderr, got %q", got)
	}
}

func TestMemoryScanMissingRootShowsUsage(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"memory-scan"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code for missing root")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "usage: qiankun-mcpd memory-scan") || !strings.Contains(got, "--root <path>") {
		t.Fatalf("expected memory-scan usage in stderr, got %q", got)
	}
}

func TestMemoryQueryOutput(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := filepath.Join("..", "..", "testdata", "memory-scan-fixture")
	code := run([]string{"memory-query", "--root", root, "--query", "Vue router component", "--top-k", "5"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var payload struct {
		Root    string `json:"root"`
		Query   string `json:"query"`
		TopK    int    `json:"top_k"`
		Results []struct {
			Path          string  `json:"path"`
			Kind          string  `json:"kind"`
			Weight        int     `json:"weight"`
			Score         float64 `json:"score"`
			TokenEstimate int     `json:"token_estimate"`
			Snippet       string  `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("memory-query output is not JSON: %v; output=%q", err, stdout.String())
	}
	if payload.Root == "" || payload.Query != "Vue router component" || payload.TopK != 5 || len(payload.Results) == 0 {
		t.Fatalf("unexpected memory-query payload: %+v", payload)
	}
	if payload.Results[0].Path == "" || payload.Results[0].Score == 0 || payload.Results[0].Snippet == "" {
		t.Fatalf("expected scored result with snippet, got %+v", payload.Results[0])
	}
}

func TestUsageReportOutput(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := filepath.Join("..", "..", "testdata", "memory-scan-fixture")
	if code := run([]string{"memory-query", "--root", root, "--query", "Vue router component"}, &bytes.Buffer{}, &stderr); code != 0 {
		t.Fatalf("memory-query setup failed with %d; stderr=%q", code, stderr.String())
	}
	stderr.Reset()

	code := run([]string{"usage-report"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	var payload struct {
		TotalCalls      int   `json:"total_calls"`
		EstimatedTokens int   `json:"estimated_tokens"`
		P95LatencyMS    int64 `json:"p95_latency_ms"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("usage-report output is not JSON: %v; output=%q", err, stdout.String())
	}
	if payload.TotalCalls == 0 || payload.EstimatedTokens == 0 {
		t.Fatalf("unexpected usage-report payload: %+v", payload)
	}
}

func TestWeeklyReportOutput(t *testing.T) {
	t.Setenv("QIANKUN_HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"weekly-report", "--format", "markdown", "--instructions-root", filepath.Join("..", "..")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"# QianKun Weekly Report", "## Memory Index", "## UsageMeter", "## Instructions Lint", "## W3 Known Gaps"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected %q in weekly report:\n%s", want, stdout.String())
		}
	}
}

func TestUnknownArgumentReturnsError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"--unknown"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code for unknown argument")
	}

	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout for unknown argument, got %q", stdout.String())
	}

	if got := stderr.String(); !strings.Contains(got, "usage: qiankun-mcpd") {
		t.Fatalf("expected usage in stderr, got %q", got)
	}
}
