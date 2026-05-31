package scan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/tokens"
)

var errBinarySample = errors.New("binary sample")

func Scan(opts Options) (Result, error) {
	if opts.Root == "" {
		return Result{}, errors.New("root is required")
	}

	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("root is not a directory: %s", root)
	}

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	if opts.NowUTC != nil {
		generatedAt = opts.NowUTC()
	}

	state := scannerState{
		root:       root,
		include:    patternSet(opts.Include),
		exclude:    patternSet(opts.Exclude),
		ignores:    loadIgnoreRules(root),
		hasInclude: len(opts.Include) > 0,
		springApp:  make(map[string]bool),
		result: Result{
			Root:           root,
			GeneratedAt:    generatedAt,
			SkippedSummary: make(map[string]SkippedSummary),
		},
	}

	err = filepath.WalkDir(root, state.walk)
	if err != nil {
		return Result{}, err
	}
	sort.SliceStable(state.result.Files, func(i, j int) bool {
		return state.result.Files[i].Path < state.result.Files[j].Path
	})
	state.assignProfilesAndRoles()
	return state.result, nil
}

type scannerState struct {
	root       string
	include    patternSet
	exclude    patternSet
	ignores    []ignoreRule
	hasInclude bool
	springApp  map[string]bool // 含 @SpringBootApplication 的源文件相对路径集合
	result     Result
}

// assignProfilesAndRoles 在遍历完成后统一为每个文件填充真实 profile 与 role：
// profile 需要全量文件视角（marker + 语言分布）才能判定，故放在 walk 之后；
// role 由相对路径与文件分档纯启发式得出，与 profile 解耦。
func (s *scannerState) assignProfilesAndRoles() {
	profiles := detectProfiles(s.result.Files, s.springApp)
	for i := range s.result.Files {
		f := &s.result.Files[i]
		f.Profile = profileForPath(profiles, f.Path)
		f.Role = roleForRel(f.Path, f.Kind)
	}
}

func (s *scannerState) walk(path string, d fs.DirEntry, walkErr error) error {
	if path == s.root {
		return nil
	}

	rel, relErr := filepath.Rel(s.root, path)
	if relErr != nil {
		rel = filepath.Base(path)
	}
	rel = filepath.ToSlash(rel)

	if walkErr != nil {
		s.addSkippedFile(FileEntry{
			Path:       rel,
			Kind:       KindUnknown,
			Weight:     0,
			Skipped:    true,
			SkipReason: SkipWalkError,
			Error:      walkErr.Error(),
		})
		if d != nil && d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	if d.IsDir() {
		if s.exclude.match(rel) {
			s.addSkippedDir(SkipUserExclude, rel)
			return filepath.SkipDir
		}
		if reason, ok := matchIgnore(s.ignores, rel, true); ok {
			s.addSkippedDir(reason, rel)
			return filepath.SkipDir
		}
		if _, reason, ok := classifySkippedDir(rel); ok {
			s.addSkippedDir(reason, rel)
			return filepath.SkipDir
		}
		return nil
	}

	info, err := d.Info()
	if err != nil {
		s.addSkippedFile(FileEntry{
			Path:       rel,
			Kind:       KindUnknown,
			Weight:     0,
			Skipped:    true,
			SkipReason: SkipReadError,
			Error:      err.Error(),
		})
		return nil
	}

	entry := FileEntry{
		Path:      rel,
		SizeBytes: info.Size(),
	}

	if s.exclude.match(rel) {
		entry.Kind = inferTextKind(rel)
		entry.Weight = 0
		entry.Skipped = true
		entry.SkipReason = SkipUserExclude
		s.addSkippedFile(entry)
		return nil
	}
	if s.hasInclude && !s.include.match(rel) {
		entry.Kind = inferTextKind(rel)
		entry.Weight = 0
		entry.Skipped = true
		entry.SkipReason = SkipIncludeMismatch
		s.addSkippedFile(entry)
		return nil
	}
	if reason, ok := matchIgnore(s.ignores, rel, false); ok {
		entry.Kind = inferTextKind(rel)
		entry.Weight = 0
		entry.Skipped = true
		entry.SkipReason = reason
		s.addSkippedFile(entry)
		return nil
	}

	kind, weight, skipped, skipReason := classifyFile(rel)
	entry.Kind = kind
	entry.Weight = weight
	entry.Skipped = skipped
	entry.SkipReason = skipReason
	if skipped {
		s.addSkippedFile(entry)
		return nil
	}

	sample, sampleBytes, sampled, err := readTextSample(path, info.Size())
	if err != nil {
		if errors.Is(err, errBinarySample) {
			entry.Kind = KindBinary
			entry.Weight = 0
			entry.Skipped = true
			entry.SkipReason = SkipBinary
		} else {
			entry.Weight = 0
			entry.Skipped = true
			entry.SkipReason = SkipReadError
			entry.Error = err.Error()
		}
		s.addSkippedFile(entry)
		return nil
	}

	entry.Sampled = sampled
	entry.SampleBytes = sampleBytes
	entry.TokenEstimate = tokens.EstimateTextTokens(sample)
	// 记录 Spring Boot 入口信号，供 profile 检测使用（@SpringBootApplication 位于文件头部）。
	if isJavaLike(rel) && strings.Contains(sample, "@SpringBootApplication") {
		s.springApp[rel] = true
	}
	s.addIndexedFile(entry)
	return nil
}

func isJavaLike(rel string) bool {
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".java") || strings.HasSuffix(lower, ".kt")
}

func readTextSample(path string, size int64) (string, int, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer file.Close()

	if size > LargeTextFileThresholdBytes {
		readBytes := TailSampleBytes
		if size < int64(readBytes) {
			readBytes = int(size)
		}
		start := size - int64(readBytes)
		buf := make([]byte, readBytes)
		n, err := file.ReadAt(buf, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", 0, true, err
		}
		if bytes.IndexByte(buf[:n], 0) >= 0 {
			return "", 0, true, errBinarySample
		}
		return string(buf[:n]), n, true, nil
	}

	buf, err := io.ReadAll(file)
	if err != nil {
		return "", 0, false, err
	}
	if bytes.IndexByte(buf, 0) >= 0 {
		return "", 0, false, errBinarySample
	}
	return string(buf), len(buf), false, nil
}

func (s *scannerState) addIndexedFile(entry FileEntry) {
	s.result.Files = append(s.result.Files, entry)
	s.result.Totals.FilesSeen++
	s.result.Totals.FilesIndexed++
	s.result.Totals.EstimatedTokens += entry.TokenEstimate
}

func (s *scannerState) addSkippedFile(entry FileEntry) {
	s.result.Files = append(s.result.Files, entry)
	s.result.Totals.FilesSeen++
	s.result.Totals.FilesSkipped++
	s.addSkippedSummary(entry.SkipReason, entry.Path)
}

func (s *scannerState) addSkippedDir(reason, rel string) {
	s.result.Totals.DirectoriesSkipped++
	s.addSkippedSummary(reason, rel)
}

func (s *scannerState) addSkippedSummary(reason, rel string) {
	if reason == "" {
		reason = "unknown"
	}
	summary := s.result.SkippedSummary[reason]
	summary.Count++
	if len(summary.RepresentativePaths) < 3 {
		summary.RepresentativePaths = append(summary.RepresentativePaths, filepath.ToSlash(rel))
	}
	s.result.SkippedSummary[reason] = summary
}

func pathFromRoot(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}
