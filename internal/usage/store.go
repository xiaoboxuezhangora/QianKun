package usage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/xiaoboxuezhangora/QianKun/internal/localdb"
)

const usageDBName = "usage.sqlite"

type Store struct {
	db   *sql.DB
	path string
}

func DefaultDBPath() (string, error) {
	return localdb.DBPath(usageDBName)
}

func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}
	db, err := localdb.Open(path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, path: path}
	if err := store.ensureSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DBPath() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) ensureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS usage_event (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT NOT NULL,
		tool TEXT NOT NULL,
		root TEXT NOT NULL DEFAULT '',
		token_estimate INTEGER NOT NULL DEFAULT 0,
		saved_tokens INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		cache_key TEXT NOT NULL DEFAULT '',
		cache_avoided_tokens INTEGER NOT NULL DEFAULT 0,
		sent_context_tokens_estimate INTEGER NOT NULL DEFAULT 0,
		adjusted_saved_tokens INTEGER NOT NULL DEFAULT 0,
		ignored_tokens_estimate INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_usage_event_type_created ON usage_event(event_type, created_at)`)
	return err
}

func (s *Store) Record(event Event) error {
	if event.Type == "" {
		return fmt.Errorf("usage event type is required")
	}
	if event.Tool == "" {
		event.Tool = "unknown"
	}
	_, err := s.db.Exec(`INSERT INTO usage_event(event_type, tool, root, token_estimate, saved_tokens, latency_ms, cache_key, cache_avoided_tokens, sent_context_tokens_estimate, adjusted_saved_tokens, ignored_tokens_estimate, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(event.Type), event.Tool, event.Root, event.TokenEstimate, event.SavedTokens, event.LatencyMS, event.CacheKey,
		event.CacheAvoidedTokens, event.SentContextTokensEstimate, event.AdjustedSavedTokens, event.IgnoredTokensEstimate,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) Report() (Report, error) {
	report := Report{
		DBPath:      s.path,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	row := s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN event_type = 'call' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN event_type = 'cache_hit' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN event_type = 'cache_miss' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(token_estimate), 0),
		COALESCE(SUM(saved_tokens), 0),
		COALESCE(SUM(cache_avoided_tokens), 0),
		COALESCE(SUM(sent_context_tokens_estimate), 0),
		COALESCE(SUM(adjusted_saved_tokens), 0),
		COALESCE(SUM(ignored_tokens_estimate), 0)
		FROM usage_event`)
	if err := row.Scan(
		&report.TotalCalls,
		&report.CacheHits,
		&report.CacheMisses,
		&report.EstimatedTokens,
		&report.EstimatedSavedTokens,
		&report.CacheAvoidedTokens,
		&report.SentContextTokensEstimate,
		&report.AdjustedSavedTokens,
		&report.IgnoredTokensEstimate,
	); err != nil {
		return report, err
	}

	latencies, err := s.latencies()
	if err != nil {
		return report, err
	}
	report.P95LatencyMS = percentile95(latencies)
	return report, nil
}

func (s *Store) latencies() ([]int64, error) {
	rows, err := s.db.Query(`SELECT latency_ms FROM usage_event WHERE event_type = 'latency' AND latency_ms > 0 ORDER BY latency_ms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []int64
	for rows.Next() {
		var latency int64
		if err := rows.Scan(&latency); err != nil {
			return nil, err
		}
		result = append(result, latency)
	}
	return result, rows.Err()
}

func percentile95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	idx := int(float64(len(values))*0.95+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}
