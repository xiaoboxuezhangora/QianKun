package main

import (
	"bytes"
	"encoding/json"
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
	if !strings.Contains(stdout.String(), "0.1.0-w1") {
		t.Fatalf("expected version in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
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
