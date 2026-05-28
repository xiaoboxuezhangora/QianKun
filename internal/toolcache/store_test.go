package toolcache

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreSetGetDeleteAndStats(t *testing.T) {
	store := openTempStore(t, 8)
	defer closeStore(t, store)

	if err := store.Set("tool:hash:v1", []byte("value"), 0); err != nil {
		t.Fatalf("set value: %v", err)
	}

	got, ok, err := store.Get("tool:hash:v1")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("value")) {
		t.Fatalf("unexpected get result ok=%v value=%q", ok, got)
	}

	if err := store.Delete("tool:hash:v1"); err != nil {
		t.Fatalf("delete value: %v", err)
	}
	_, ok, err = store.Get("tool:hash:v1")
	if err != nil {
		t.Fatalf("get deleted value: %v", err)
	}
	if ok {
		t.Fatal("expected deleted value to miss")
	}

	stats := store.Stats()
	if stats.Entries != 0 {
		t.Fatalf("expected no entries, got %+v", stats)
	}
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("unexpected hit/miss stats: %+v", stats)
	}
}

func TestStoreExpiresTTL(t *testing.T) {
	store := openTempStore(t, 8)
	defer closeStore(t, store)

	if err := store.Set("tool:ttl:v1", []byte("value"), 10*time.Millisecond); err != nil {
		t.Fatalf("set value: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	_, ok, err := store.Get("tool:ttl:v1")
	if err != nil {
		t.Fatalf("get expired value: %v", err)
	}
	if ok {
		t.Fatal("expected expired value to miss")
	}
	if stats := store.Stats(); stats.Entries != 0 {
		t.Fatalf("expected expired entry to be pruned, got %+v", stats)
	}
}

func TestStoreEvictsLeastRecentlyUsed(t *testing.T) {
	store := openTempStore(t, 2)
	defer closeStore(t, store)

	mustSet(t, store, "a", "A")
	mustSet(t, store, "b", "B")
	if _, ok, err := store.Get("a"); err != nil || !ok {
		t.Fatalf("refresh a err=%v ok=%v", err, ok)
	}
	mustSet(t, store, "c", "C")

	if _, ok, err := store.Get("b"); err != nil || ok {
		t.Fatalf("expected b to be evicted err=%v ok=%v", err, ok)
	}
	if _, ok, err := store.Get("a"); err != nil || !ok {
		t.Fatalf("expected a to remain err=%v ok=%v", err, ok)
	}
	if _, ok, err := store.Get("c"); err != nil || !ok {
		t.Fatalf("expected c to remain err=%v ok=%v", err, ok)
	}
	if stats := store.Stats(); stats.Evictions != 1 {
		t.Fatalf("expected one eviction, got %+v", stats)
	}
}

func TestStorePersistsToJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "toolcache.json")
	store, err := Open(Options{FilePath: path, MaxEntries: 8})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	mustSet(t, store, "tool:persist:v1", "persisted")
	closeStore(t, store)

	reopened, err := Open(Options{FilePath: path, MaxEntries: 8})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer closeStore(t, reopened)

	got, ok, err := reopened.Get("tool:persist:v1")
	if err != nil {
		t.Fatalf("get persisted value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("persisted")) {
		t.Fatalf("unexpected persisted value ok=%v value=%q", ok, got)
	}
}

func TestStoreIsSafeForConcurrentAccess(t *testing.T) {
	store := openTempStore(t, 128)
	defer closeStore(t, store)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := BuildTestKey(t, id)
			for j := 0; j < 20; j++ {
				if err := store.Set(key, []byte("value"), time.Minute); err != nil {
					t.Errorf("set key %s: %v", key, err)
					return
				}
				if _, _, err := store.Get(key); err != nil {
					t.Errorf("get key %s: %v", key, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func openTempStore(t *testing.T, maxEntries int) *Store {
	t.Helper()

	store, err := Open(Options{
		FilePath:   filepath.Join(t.TempDir(), "cache", "toolcache.json"),
		MaxEntries: maxEntries,
	})
	if err != nil {
		t.Fatalf("open temp store: %v", err)
	}
	return store
}

func closeStore(t *testing.T, store *Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func mustSet(t *testing.T, store *Store, key string, value string) {
	t.Helper()
	if err := store.Set(key, []byte(value), time.Minute); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func BuildTestKey(t *testing.T, id int) string {
	t.Helper()
	key, err := BuildKey("test-tool", map[string]int{"id": id}, "schema-v1")
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	return key
}
