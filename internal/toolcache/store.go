package toolcache

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	DefaultMaxEntries = 1024
	diskVersion       = 1
)

var ErrClosed = errors.New("toolcache store is closed")

type Options struct {
	FilePath   string
	MaxEntries int
}

type Store struct {
	mu         sync.Mutex
	filePath   string
	maxEntries int
	items      map[string]*entry
	order      *list.List
	nodes      map[string]*list.Element
	hits       uint64
	misses     uint64
	evictions  uint64
	dirty      bool
	closed     bool
}

type entry struct {
	Key          string
	Value        []byte
	ExpiresAt    time.Time
	LastAccess   time.Time
	CreatedAt    time.Time
	LastModified time.Time
}

type Stats struct {
	Readiness string `json:"readiness"`
	Entries   int    `json:"entries"`
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Evictions uint64 `json:"evictions"`
	FilePath  string `json:"file_path"`
}

type diskFile struct {
	Version int         `json:"version"`
	Entries []diskEntry `json:"entries"`
}

type diskEntry struct {
	Key                  string `json:"key"`
	Value                []byte `json:"value"`
	ExpiresAtUnixNano    int64  `json:"expires_at_unix_nano,omitempty"`
	LastAccessUnixNano   int64  `json:"last_access_unix_nano"`
	CreatedAtUnixNano    int64  `json:"created_at_unix_nano"`
	LastModifiedUnixNano int64  `json:"last_modified_unix_nano"`
}

func DefaultFilePath() (string, error) {
	if home := os.Getenv("QIANKUN_HOME"); home != "" {
		return filepath.Join(home, "cache", "toolcache.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".qiankun", "cache", "toolcache.json"), nil
}

func Open(opts Options) (*Store, error) {
	filePath := opts.FilePath
	if filePath == "" {
		var err error
		filePath, err = DefaultFilePath()
		if err != nil {
			return nil, err
		}
	}

	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}

	store := &Store{
		filePath:   filePath,
		maxEntries: maxEntries,
		items:      make(map[string]*entry),
		order:      list.New(),
		nodes:      make(map[string]*list.Element),
	}

	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Get(key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, false, ErrClosed
	}

	item, ok := s.items[key]
	if !ok {
		s.misses++
		return nil, false, nil
	}

	now := time.Now()
	if item.expired(now) {
		s.removeLocked(key)
		s.misses++
		s.dirty = true
		return nil, false, nil
	}

	item.LastAccess = now
	if node := s.nodes[key]; node != nil {
		s.order.MoveToFront(node)
	}
	s.hits++

	value := make([]byte, len(item.Value))
	copy(value, item.Value)
	return value, true, nil
}

func (s *Store) Set(key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	now := time.Now()
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	if item, ok := s.items[key]; ok {
		item.Value = cloneBytes(value)
		item.ExpiresAt = expiresAt
		item.LastAccess = now
		item.LastModified = now
		if node := s.nodes[key]; node != nil {
			s.order.MoveToFront(node)
		}
		s.dirty = true
		return nil
	}

	s.items[key] = &entry{
		Key:          key,
		Value:        cloneBytes(value),
		ExpiresAt:    expiresAt,
		LastAccess:   now,
		CreatedAt:    now,
		LastModified: now,
	}
	s.nodes[key] = s.order.PushFront(key)
	s.dirty = true
	s.enforceCapacityLocked()
	return nil
}

func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	if _, ok := s.items[key]; ok {
		s.removeLocked(key)
		s.dirty = true
	}
	return nil
}

func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		if s.pruneExpiredLocked(time.Now()) > 0 {
			s.dirty = true
		}
	}

	readiness := "ready"
	if s.closed {
		readiness = "closed"
	}

	return Stats{
		Readiness: readiness,
		Entries:   len(s.items),
		Hits:      s.hits,
		Misses:    s.misses,
		Evictions: s.evictions,
		FilePath:  s.filePath,
	}
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.pruneExpiredLocked(time.Now()) > 0 {
		s.dirty = true
	}

	if !s.dirty {
		return nil
	}

	if err := s.writeLocked(); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read toolcache file %s: %w", s.filePath, err)
	}

	var disk diskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("parse toolcache file %s: %w", s.filePath, err)
	}
	if disk.Version != diskVersion {
		return fmt.Errorf("unsupported toolcache file version %d", disk.Version)
	}

	now := time.Now()
	loaded := make([]*entry, 0, len(disk.Entries))
	for _, record := range disk.Entries {
		item := record.toEntry()
		if item.Key == "" {
			continue
		}
		if item.expired(now) {
			s.dirty = true
			continue
		}
		loaded = append(loaded, item)
	}

	sort.SliceStable(loaded, func(i, j int) bool {
		return loaded[i].LastAccess.Before(loaded[j].LastAccess)
	})
	for _, item := range loaded {
		s.items[item.Key] = item
		s.nodes[item.Key] = s.order.PushFront(item.Key)
	}
	s.enforceCapacityLocked()
	return nil
}

func (s *Store) writeLocked() error {
	records := make([]diskEntry, 0, len(s.items))
	for node := s.order.Front(); node != nil; node = node.Next() {
		key := node.Value.(string)
		item := s.items[key]
		records = append(records, item.toDiskEntry())
	}

	data, err := json.MarshalIndent(diskFile{
		Version: diskVersion,
		Entries: records,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode toolcache file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return fmt.Errorf("create toolcache directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.filePath), ".toolcache-*.json")
	if err != nil {
		return fmt.Errorf("create temporary toolcache file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary toolcache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary toolcache file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod temporary toolcache file: %w", err)
	}
	if err := os.Rename(tmpName, s.filePath); err != nil {
		return fmt.Errorf("replace toolcache file: %w", err)
	}
	return nil
}

func (s *Store) enforceCapacityLocked() {
	for len(s.items) > s.maxEntries {
		node := s.order.Back()
		if node == nil {
			return
		}
		key := node.Value.(string)
		s.removeLocked(key)
		s.evictions++
		s.dirty = true
	}
}

func (s *Store) pruneExpiredLocked(now time.Time) int {
	removed := 0
	for key, item := range s.items {
		if item.expired(now) {
			s.removeLocked(key)
			removed++
		}
	}
	return removed
}

func (s *Store) removeLocked(key string) {
	delete(s.items, key)
	if node := s.nodes[key]; node != nil {
		s.order.Remove(node)
		delete(s.nodes, key)
	}
}

func (e *entry) expired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now)
}

func (e *entry) toDiskEntry() diskEntry {
	record := diskEntry{
		Key:                  e.Key,
		Value:                cloneBytes(e.Value),
		LastAccessUnixNano:   e.LastAccess.UnixNano(),
		CreatedAtUnixNano:    e.CreatedAt.UnixNano(),
		LastModifiedUnixNano: e.LastModified.UnixNano(),
	}
	if !e.ExpiresAt.IsZero() {
		record.ExpiresAtUnixNano = e.ExpiresAt.UnixNano()
	}
	return record
}

func (d diskEntry) toEntry() *entry {
	item := &entry{
		Key:          d.Key,
		Value:        cloneBytes(d.Value),
		LastAccess:   timeFromUnixNano(d.LastAccessUnixNano),
		CreatedAt:    timeFromUnixNano(d.CreatedAtUnixNano),
		LastModified: timeFromUnixNano(d.LastModifiedUnixNano),
	}
	if d.ExpiresAtUnixNano > 0 {
		item.ExpiresAt = timeFromUnixNano(d.ExpiresAtUnixNano)
	}
	return item
}

func timeFromUnixNano(value int64) time.Time {
	if value <= 0 {
		return time.Now()
	}
	return time.Unix(0, value)
}

func cloneBytes(value []byte) []byte {
	out := make([]byte, len(value))
	copy(out, value)
	return out
}
