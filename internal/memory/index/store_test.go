package index

import (
	"path/filepath"
	"testing"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
)

func TestSyncScanAndQueryFixture(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	result := scanFixture(t)
	stats, err := store.SyncScan(result)
	if err != nil {
		t.Fatalf("SyncScan failed: %v", err)
	}
	if stats.FilesIndexed == 0 || stats.FilesUpdated == 0 || stats.EstimatedTokens == 0 {
		t.Fatalf("unexpected sync stats: %+v", stats)
	}

	response, err := store.Query(result.Root, "Vue router component", 5)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if response.Root == "" || response.Query == "" || response.TopK != 5 || len(response.Results) == 0 {
		t.Fatalf("unexpected query response: %+v", response)
	}
	if response.Results[0].Path == "" || response.Results[0].Snippet == "" || response.Results[0].Score == 0 {
		t.Fatalf("expected scored result with snippet, got %+v", response.Results[0])
	}
}

func TestSyncScanIsIncremental(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	result := scanFixture(t)
	first, err := store.SyncScan(result)
	if err != nil {
		t.Fatalf("first SyncScan failed: %v", err)
	}
	second, err := store.SyncScan(result)
	if err != nil {
		t.Fatalf("second SyncScan failed: %v", err)
	}
	if first.FilesUpdated == 0 {
		t.Fatalf("expected first sync to update rows: %+v", first)
	}
	if second.FilesUpdated != 0 || second.FilesDeleted != 0 || second.FilesUnchanged == 0 {
		t.Fatalf("expected second sync to reuse unchanged rows: %+v", second)
	}
}

func TestRootStats(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	result := scanFixture(t)
	if _, err := store.SyncScan(result); err != nil {
		t.Fatalf("SyncScan failed: %v", err)
	}
	stats, err := store.RootStats(result.Root)
	if err != nil {
		t.Fatalf("RootStats failed: %v", err)
	}
	if stats.FilesIndexed == 0 || stats.EstimatedTokens == 0 || stats.DBPath == "" {
		t.Fatalf("unexpected root stats: %+v", stats)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(Options{DBPath: filepath.Join(t.TempDir(), "memory.sqlite")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return store
}

func scanFixture(t *testing.T) scan.Result {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "memory-scan-fixture")
	result, err := scan.Scan(scan.Options{Root: root})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	return result
}
