package injection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultInstructionFiles(t *testing.T) {
	root := filepath.Join("tmp", "project")
	files := DefaultInstructionFiles(root)

	if len(files) != 2 {
		t.Fatalf("expected two default files, got %+v", files)
	}
	if files[0] != filepath.Join(root, "AGENTS.md") {
		t.Fatalf("unexpected AGENTS path: %s", files[0])
	}
	if files[1] != filepath.Join(root, "CLAUDE.md") {
		t.Fatalf("unexpected CLAUDE path: %s", files[1])
	}
}

func TestExtractZonesFromFileReadsAgentsSingleZone(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, path, "user before\n"+StartMarker+"\nmanaged agents\n"+EndMarker+"\nuser after")

	zones, err := ExtractZonesFromFile(path)
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected one zone, got %+v", zones)
	}
	if zones[0].Content != "\nmanaged agents\n" {
		t.Fatalf("unexpected zone content: %q", zones[0].Content)
	}
}

func TestExtractZonesFromFilesReadsClaudeMultipleZones(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	writeTestFile(t, path, StartMarker+"first"+EndMarker+"\nuser middle\n"+StartMarker+"second"+EndMarker)

	documents, err := ExtractZonesFromFiles([]string{path})
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("expected one document, got %+v", documents)
	}
	if documents[0].Path != path {
		t.Fatalf("unexpected document path: %s", documents[0].Path)
	}
	if len(documents[0].Zones) != 2 {
		t.Fatalf("expected two zones, got %+v", documents[0].Zones)
	}
	if documents[0].Zones[0].Content != "first" || documents[0].Zones[1].Content != "second" {
		t.Fatalf("unexpected zone contents: %+v", documents[0].Zones)
	}
}

func TestExtractZonesFromFileMissingReturnsEmpty(t *testing.T) {
	zones, err := ExtractZonesFromFile(filepath.Join(t.TempDir(), "AGENTS.md"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(zones) != 0 {
		t.Fatalf("expected empty zones for missing file, got %+v", zones)
	}

	documents, err := ExtractZonesFromFiles([]string{filepath.Join(t.TempDir(), "CLAUDE.md")})
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("expected empty documents for missing file, got %+v", documents)
	}
}

func TestExtractZonesFromFileReadErrorIncludesPath(t *testing.T) {
	path := t.TempDir()

	_, err := ExtractZonesFromFile(path)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("expected error to contain path %q, got %v", path, err)
	}
}

func TestExtractZonesFromFileMissingEndErrorIncludesPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, path, "user before\n"+StartMarker+"\nmanaged")

	_, err := ExtractZonesFromFile(path)
	if err == nil {
		t.Fatal("expected missing end marker error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("expected error to contain path %q, got %v", path, err)
	}
	if !strings.Contains(err.Error(), "missing end marker") {
		t.Fatalf("expected missing end marker details, got %v", err)
	}
}

func TestExtractZonesFromFileOrphanEndErrorIncludesPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	writeTestFile(t, path, "user before\n"+EndMarker+"\nuser after")

	_, err := ExtractZonesFromFile(path)
	if err == nil {
		t.Fatal("expected orphan end marker error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("expected error to contain path %q, got %v", path, err)
	}
	if !strings.Contains(err.Error(), "no matching start marker") {
		t.Fatalf("expected orphan end marker details, got %v", err)
	}
}

func TestExtractZonesFromFileOutsideTextDoesNotEnterResult(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	writeTestFile(t, path, "outside before\n"+StartMarker+"inside"+EndMarker+"\noutside after")

	zones, err := ExtractZonesFromFile(path)
	if err != nil {
		t.Fatalf("extract zones: %v", err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected one zone, got %+v", zones)
	}
	if zones[0].Content != "inside" {
		t.Fatalf("unexpected content: %q", zones[0].Content)
	}
	if strings.Contains(zones[0].Content, "outside") {
		t.Fatalf("outside text leaked into result: %q", zones[0].Content)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
