package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaoboxuezhangora/QianKun/internal/localdb"
	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
)

const memoryDBName = "memory.sqlite"

type Store struct {
	db          *sql.DB
	path        string
	fts5Enabled bool
}

func DefaultDBPath() (string, error) {
	return localdb.DBPath(memoryDBName)
}

func Open(opts Options) (*Store, error) {
	path := opts.DBPath
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

func (s *Store) FTS5Enabled() bool {
	if s == nil {
		return false
	}
	return s.fts5Enabled
}

func (s *Store) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS file_summary (
			root TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			weight INTEGER NOT NULL,
			token_estimate INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			content_sample TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			mtime_unix_nano INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (root, path)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_summary_root_weight ON file_summary(root, weight DESC)`,
		`CREATE TABLE IF NOT EXISTS keyword_index (
			root TEXT NOT NULL,
			path TEXT NOT NULL,
			term TEXT NOT NULL,
			count INTEGER NOT NULL,
			PRIMARY KEY (root, path, term)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_keyword_index_term ON keyword_index(root, term)`,
		`CREATE TABLE IF NOT EXISTS recent_change (
			root TEXT NOT NULL,
			path TEXT NOT NULL,
			change_type TEXT NOT NULL,
			changed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recent_change_root ON recent_change(root, changed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS symbol (
			root TEXT NOT NULL,
			path TEXT NOT NULL,
			symbol_name TEXT NOT NULL,
			symbol_kind TEXT NOT NULL,
			line INTEGER NOT NULL DEFAULT 0,
			weight INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (root, path, symbol_name, symbol_kind)
		)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	// FTS5 availability depends on the SQLite library compiled into the driver.
	// When unavailable, W3 keeps keyword_index + LIKE scoring as a deterministic fallback.
	if _, err := s.db.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS file_fts USING fts5(root UNINDEXED, path, kind UNINDEXED, content_sample, tokenize='unicode61')`); err == nil {
		s.fts5Enabled = true
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO meta(key, value) VALUES('fts5_enabled', 'true')`)
	} else {
		s.fts5Enabled = false
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO meta(key, value) VALUES('fts5_enabled', 'false')`)
	}
	return nil
}

func (s *Store) SyncScan(result scan.Result) (SyncStats, error) {
	root, err := filepath.Abs(result.Root)
	if err != nil {
		return SyncStats{}, err
	}
	stats := SyncStats{
		Root:            root,
		DBPath:          s.path,
		FTS5Enabled:     s.fts5Enabled,
		EstimatedTokens: result.Totals.EstimatedTokens,
	}

	existing, err := s.loadExisting(root)
	if err != nil {
		return stats, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	current := make(map[string]bool)
	for _, entry := range result.Files {
		if entry.Skipped {
			continue
		}
		current[entry.Path] = true
		stats.FilesIndexed++

		fullPath := filepath.Join(root, filepath.FromSlash(entry.Path))
		info, err := os.Stat(fullPath)
		if err != nil {
			stats.ReadErrors++
			continue
		}
		mtime := info.ModTime().UnixNano()
		if old, ok := existing[entry.Path]; ok && old.SizeBytes == info.Size() && old.MTimeUnixNano == mtime {
			stats.FilesUnchanged++
			continue
		}

		fileHash, sample, err := hashAndSample(fullPath, info.Size())
		if err != nil {
			stats.ReadErrors++
			continue
		}
		if old, ok := existing[entry.Path]; ok && old.SizeBytes == info.Size() && old.FileHash == fileHash {
			if _, err := tx.Exec(`UPDATE file_summary SET mtime_unix_nano = ?, updated_at = ? WHERE root = ? AND path = ?`, mtime, now, root, entry.Path); err != nil {
				return stats, err
			}
			stats.FilesUnchanged++
			continue
		}

		if _, err := tx.Exec(`INSERT OR REPLACE INTO file_summary(root, path, kind, weight, token_estimate, size_bytes, content_sample, file_hash, mtime_unix_nano, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			root, entry.Path, string(entry.Kind), entry.Weight, entry.TokenEstimate, entry.SizeBytes, sample, fileHash, mtime, now); err != nil {
			return stats, err
		}
		if err := s.replaceSearchRows(tx, root, entry.Path, string(entry.Kind), sample); err != nil {
			return stats, err
		}
		if err := insertRecentChange(tx, root, entry.Path, "upsert", now); err != nil {
			return stats, err
		}
		stats.FilesUpdated++
	}

	for path := range existing {
		if current[path] {
			continue
		}
		if err := s.deletePath(tx, root, path); err != nil {
			return stats, err
		}
		if err := insertRecentChange(tx, root, path, "delete", now); err != nil {
			return stats, err
		}
		stats.FilesDeleted++
	}

	if err := tx.Commit(); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *Store) RootStats(root string) (RootStats, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return RootStats{}, err
	}
	stats := RootStats{Root: absRoot, DBPath: s.path, FTS5Enabled: s.fts5Enabled}
	row := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(token_estimate), 0) FROM file_summary WHERE root = ?`, absRoot)
	if err := row.Scan(&stats.FilesIndexed, &stats.EstimatedTokens); err != nil {
		return stats, err
	}
	return stats, nil
}

// RecentChanges 返回指定 root 最近的 upsert/delete 事件，按时间倒序，
// 用于周报等读取侧消费 recent_change 表。limit <= 0 时回退到默认 10 条。
func (s *Store) RecentChanges(root string, limit int) ([]RecentChange, error) {
	if limit <= 0 {
		limit = 10
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT path, change_type, changed_at FROM recent_change WHERE root = ? ORDER BY changed_at DESC, rowid DESC LIMIT ?`, absRoot, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []RecentChange
	for rows.Next() {
		var change RecentChange
		if err := rows.Scan(&change.Path, &change.ChangeType, &change.ChangedAt); err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, rows.Err()
}

type existingFile struct {
	SizeBytes     int64
	FileHash      string
	MTimeUnixNano int64
}

func (s *Store) loadExisting(root string) (map[string]existingFile, error) {
	rows, err := s.db.Query(`SELECT path, size_bytes, file_hash, mtime_unix_nano FROM file_summary WHERE root = ?`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]existingFile)
	for rows.Next() {
		var path string
		var file existingFile
		if err := rows.Scan(&path, &file.SizeBytes, &file.FileHash, &file.MTimeUnixNano); err != nil {
			return nil, err
		}
		result[path] = file
	}
	return result, rows.Err()
}

func (s *Store) replaceSearchRows(tx *sql.Tx, root, path, kind, sample string) error {
	if err := s.deleteSearchRows(tx, root, path); err != nil {
		return err
	}
	if s.fts5Enabled {
		if _, err := tx.Exec(`INSERT INTO file_fts(root, path, kind, content_sample) VALUES(?, ?, ?, ?)`, root, path, kind, sample); err != nil {
			return err
		}
	}
	terms := termCounts(path + " " + kind + " " + sample)
	for term, count := range terms {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO keyword_index(root, path, term, count) VALUES(?, ?, ?, ?)`, root, path, term, count); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deletePath(tx *sql.Tx, root, path string) error {
	if _, err := tx.Exec(`DELETE FROM file_summary WHERE root = ? AND path = ?`, root, path); err != nil {
		return err
	}
	if err := s.deleteSearchRows(tx, root, path); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM symbol WHERE root = ? AND path = ?`, root, path)
	return err
}

func (s *Store) deleteSearchRows(tx *sql.Tx, root, path string) error {
	if s.fts5Enabled {
		if _, err := tx.Exec(`DELETE FROM file_fts WHERE root = ? AND path = ?`, root, path); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`DELETE FROM keyword_index WHERE root = ? AND path = ?`, root, path)
	return err
}

func insertRecentChange(tx *sql.Tx, root, path, changeType, changedAt string) error {
	_, err := tx.Exec(`INSERT INTO recent_change(root, path, change_type, changed_at) VALUES(?, ?, ?, ?)`, root, path, changeType, changedAt)
	return err
}

func hashAndSample(path string, size int64) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	h := sha256.New()
	sample, err := streamHashAndSample(file, h, size)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(h.Sum(nil)), sample, nil
}

func streamHashAndSample(r io.Reader, h hash.Hash, size int64) (string, error) {
	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	var sample []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			part := buf[:n]
			if _, hashErr := h.Write(part); hashErr != nil {
				return "", hashErr
			}
			if size > scan.LargeTextFileThresholdBytes {
				sample = append(sample, part...)
				if len(sample) > scan.TailSampleBytes {
					sample = sample[len(sample)-scan.TailSampleBytes:]
				}
			} else {
				sample = append(sample, part...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return string(sample), nil
}
