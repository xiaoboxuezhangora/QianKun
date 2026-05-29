package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/tokens"
)

func TestScanFixtureClassifiesAndSkipsNoise(t *testing.T) {
	result, err := Scan(Options{
		Root:   fixtureRoot(t),
		NowUTC: func() string { return "2026-05-28T00:00:00Z" },
	})
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if result.GeneratedAt != "2026-05-28T00:00:00Z" {
		t.Fatalf("unexpected generated_at: %q", result.GeneratedAt)
	}
	if result.Totals.FilesIndexed == 0 {
		t.Fatalf("expected indexed files, got totals=%+v", result.Totals)
	}
	if result.Totals.DirectoriesSkipped == 0 {
		t.Fatalf("expected skipped directories, got totals=%+v", result.Totals)
	}

	mainFile := mustEntry(t, result, "src/main.ts")
	if mainFile.Skipped || mainFile.Kind != KindSource || mainFile.Weight < 80 || mainFile.TokenEstimate == 0 {
		t.Fatalf("expected indexed high-weight source file, got %+v", mainFile)
	}

	packageJSON := mustEntry(t, result, "package.json")
	if packageJSON.Skipped || packageJSON.Kind != KindConfig || packageJSON.Weight < 70 {
		t.Fatalf("expected indexed high-weight package.json, got %+v", packageJSON)
	}

	androidGradle := mustEntry(t, result, "android/app/build.gradle")
	if androidGradle.Skipped || androidGradle.Kind != KindConfig || androidGradle.Weight >= packageJSON.Weight || androidGradle.Weight >= mainFile.Weight {
		t.Fatalf("expected indexed low-weight android Gradle config, got %+v", androidGradle)
	}

	androidManifest := mustEntry(t, result, "android/app/src/main/AndroidManifest.xml")
	if androidManifest.Skipped || androidManifest.Kind != KindConfig || androidManifest.Weight != androidGradle.Weight {
		t.Fatalf("expected indexed low-weight android manifest, got %+v", androidManifest)
	}

	readme := mustEntry(t, result, "README.md")
	if readme.Skipped || readme.Kind != KindDocumentation || readme.Weight >= packageJSON.Weight {
		t.Fatalf("expected indexed medium-weight README, got %+v", readme)
	}

	lockfile := mustEntry(t, result, "pnpm-lock.yaml")
	if !lockfile.Skipped || lockfile.Kind != KindLockfile || lockfile.Weight > 10 || lockfile.TokenEstimate != 0 {
		t.Fatalf("expected skipped or low-weight lockfile, got %+v", lockfile)
	}

	aiCache := mustEntry(t, result, ".claude/settings.local.json")
	if !aiCache.Skipped || aiCache.Kind != KindAICache || aiCache.SkipReason != SkipAICache {
		t.Fatalf("expected skipped AI cache file, got %+v", aiCache)
	}

	binary := mustEntry(t, result, "assets/logo.png")
	if !binary.Skipped || binary.Kind != KindBinary || binary.SkipReason != SkipBinary {
		t.Fatalf("expected skipped binary asset, got %+v", binary)
	}

	gitIgnored := mustEntry(t, result, "ignored-from-gitignore.txt")
	if !gitIgnored.Skipped || gitIgnored.SkipReason != SkipGitignore {
		t.Fatalf("expected gitignore skip, got %+v", gitIgnored)
	}

	contextgateIgnored := mustEntry(t, result, "ignored-from-contextgate.txt")
	if !contextgateIgnored.Skipped || contextgateIgnored.SkipReason != SkipContextgateIgnore {
		t.Fatalf("expected contextgateignore skip, got %+v", contextgateIgnored)
	}

	assertSummary(t, result, SkipBuildArtifact)
	assertSummary(t, result, SkipIDEConfig)
	assertSummary(t, result, SkipAICache)
	assertSummary(t, result, SkipBinary)
	assertSummary(t, result, SkipLockfile)
}

func TestScanIncludeExcludePatterns(t *testing.T) {
	includeResult, err := Scan(Options{
		Root:    fixtureRoot(t),
		Include: []string{"src/**"},
	})
	if err != nil {
		t.Fatalf("Scan with include returned error: %v", err)
	}
	if entry := mustEntry(t, includeResult, "src/main.ts"); entry.Skipped {
		t.Fatalf("expected included src/main.ts, got %+v", entry)
	}
	if entry := mustEntry(t, includeResult, "package.json"); !entry.Skipped || entry.SkipReason != SkipIncludeMismatch {
		t.Fatalf("expected package.json include mismatch, got %+v", entry)
	}

	excludeResult, err := Scan(Options{
		Root:    fixtureRoot(t),
		Exclude: []string{"src/**"},
	})
	if err != nil {
		t.Fatalf("Scan with exclude returned error: %v", err)
	}
	if hasEntry(excludeResult, "src/main.ts") {
		t.Fatalf("expected src/main.ts to be pruned by excluded directory")
	}
	if summary := excludeResult.SkippedSummary[SkipUserExclude]; summary.Count == 0 {
		t.Fatalf("expected user_exclude summary for src directory, got %+v", excludeResult.SkippedSummary)
	}
	if entry := mustEntry(t, excludeResult, "package.json"); entry.Skipped {
		t.Fatalf("expected package.json to remain indexed, got %+v", entry)
	}
}

func TestScanLargeTextReadsTailSample(t *testing.T) {
	root := t.TempDir()
	full := strings.Repeat("a", LargeTextFileThresholdBytes+128) + "\ntail 中文 sample\n"
	if err := os.WriteFile(filepath.Join(root, "large.go"), []byte(full), 0o644); err != nil {
		t.Fatalf("write large fixture: %v", err)
	}

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	entry := mustEntry(t, result, "large.go")
	if entry.Skipped || !entry.Sampled {
		t.Fatalf("expected sampled indexed large file, got %+v", entry)
	}
	if entry.SampleBytes != TailSampleBytes {
		t.Fatalf("sample bytes = %d, want %d", entry.SampleBytes, TailSampleBytes)
	}
	if entry.TokenEstimate >= tokens.EstimateTextTokens(full) {
		t.Fatalf("expected token estimate to use tail sample, got entry=%+v", entry)
	}
}

func TestScanReadErrorDoesNotAbort(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write normal file: %v", err)
	}

	err := os.Symlink("missing-target.txt", filepath.Join(root, "broken.txt"))
	if err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("Scan returned overall error for single read failure: %v", err)
	}

	normal := mustEntry(t, result, "main.go")
	if normal.Skipped || normal.TokenEstimate == 0 {
		t.Fatalf("expected normal file to remain indexed, got %+v", normal)
	}

	broken := mustEntry(t, result, "broken.txt")
	if !broken.Skipped || broken.SkipReason != SkipReadError || broken.Error == "" {
		t.Fatalf("expected broken symlink read_error entry, got %+v", broken)
	}
	assertSummary(t, result, SkipReadError)
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "memory-scan-fixture"))
}

func mustEntry(t *testing.T, result Result, rel string) FileEntry {
	t.Helper()
	for _, entry := range result.Files {
		if entry.Path == rel {
			return entry
		}
	}
	t.Fatalf("entry %q not found in result", rel)
	return FileEntry{}
}

func hasEntry(result Result, rel string) bool {
	for _, entry := range result.Files {
		if entry.Path == rel {
			return true
		}
	}
	return false
}

func assertSummary(t *testing.T, result Result, reason string) {
	t.Helper()
	summary, ok := result.SkippedSummary[reason]
	if !ok {
		t.Fatalf("skipped_summary missing %q: %+v", reason, result.SkippedSummary)
	}
	if summary.Count == 0 || len(summary.RepresentativePaths) == 0 {
		t.Fatalf("empty summary for %q: %+v", reason, summary)
	}
}
