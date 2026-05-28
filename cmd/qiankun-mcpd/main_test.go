package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHealthOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"--health"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}

	if got := strings.TrimSpace(stdout.String()); got != `{"status":"ready"}` {
		t.Fatalf("unexpected health output: %q", got)
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
