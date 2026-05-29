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
}

func TestVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0.2.0-w2") {
		t.Fatalf("expected version in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestMemoryScanOutput(t *testing.T) {
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
