package toolcache

import "testing"

func TestBuildKeyStableForMapOrder(t *testing.T) {
	first, err := BuildKey("memory-query", map[string]any{
		"query": "router",
		"top_k": 8,
		"filters": map[string]any{
			"framework": "vue",
			"role":      "route",
		},
	}, "schema-v1")
	if err != nil {
		t.Fatalf("build first key: %v", err)
	}

	second, err := BuildKey("memory-query", map[string]any{
		"filters": map[string]any{
			"role":      "route",
			"framework": "vue",
		},
		"top_k": 8,
		"query": "router",
	}, "schema-v1")
	if err != nil {
		t.Fatalf("build second key: %v", err)
	}

	if first != second {
		t.Fatalf("expected stable key for reordered maps:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestBuildKeySchemaVersionChangesKey(t *testing.T) {
	args := map[string]string{"query": "router"}

	first, err := BuildKey("memory-query", args, "schema-v1")
	if err != nil {
		t.Fatalf("build first key: %v", err)
	}
	second, err := BuildKey("memory-query", args, "schema-v2")
	if err != nil {
		t.Fatalf("build second key: %v", err)
	}

	if first == second {
		t.Fatalf("expected different keys for schema versions, got %s", first)
	}
}

func TestBuildKeyToolNameIsolated(t *testing.T) {
	args := map[string]string{"query": "router"}

	first, err := BuildKey("memory-query", args, "schema-v1")
	if err != nil {
		t.Fatalf("build first key: %v", err)
	}
	second, err := BuildKey("usage-report", args, "schema-v1")
	if err != nil {
		t.Fatalf("build second key: %v", err)
	}

	if first == second {
		t.Fatalf("expected different keys for tool names, got %s", first)
	}
}

func TestBuildKeyUsesDefaultSchemaVersion(t *testing.T) {
	key, err := BuildKey("usage-report", nil, "")
	if err != nil {
		t.Fatalf("build key: %v", err)
	}

	wantPrefix := "usage-report:"
	wantSuffix := ":" + DefaultSchemaVersion
	if len(key) <= len(wantPrefix)+len(wantSuffix) || key[:len(wantPrefix)] != wantPrefix || key[len(key)-len(wantSuffix):] != wantSuffix {
		t.Fatalf("expected default schema version in key, got %s", key)
	}
}
