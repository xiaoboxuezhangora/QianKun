package index

import (
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type indexedFile struct {
	Path          string
	Kind          string
	Weight        int
	TokenEstimate int
	SizeBytes     int64
	ContentSample string
	Role          string
	KeywordCount  int
}

func (s *Store) Query(root, query string, topK int) (QueryResponse, error) {
	if topK <= 0 {
		topK = 8
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return QueryResponse{}, err
	}
	terms := tokenize(query)

	files, err := s.queryFTS(absRoot, terms, topK*recallMultiplier)
	if err != nil || len(files) == 0 {
		files, err = s.queryKeywordFallback(absRoot, terms)
		if err != nil {
			return QueryResponse{}, err
		}
	}

	results, err := s.hybridRank(absRoot, files, query, terms, topK)
	if err != nil {
		return QueryResponse{}, err
	}
	return QueryResponse{
		Root:    absRoot,
		Query:   query,
		TopK:    topK,
		Results: results,
	}, nil
}

// hybridRank 在召回候选上计算 Rel，再以 MMR rerank 选出 topK。
// 召回来源（FTS 或 keyword 回退）对本函数透明，降级路径行为一致。
func (s *Store) hybridRank(root string, files []indexedFile, query string, terms []string, topK int) ([]QueryResult, error) {
	if len(files) == 0 {
		return []QueryResult{}, nil
	}
	intent := detectIntent(query)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	symbolsByPath, err := s.loadSymbols(root, paths)
	if err != nil {
		return nil, err
	}

	cands := make([]scoredFile, 0, len(files))
	for _, f := range files {
		syms := symbolsByPath[f.Path]
		rel := computeRel(f, syms, terms, intent)
		if rel <= 0 {
			// 噪声/无关候选不进入 rerank，满足“默认不进 top-k”。
			continue
		}
		cands = append(cands, scoredFile{
			file:     f,
			syms:     syms,
			rel:      rel,
			segments: pathSegments(f.Path),
			termSet:  fileTermSet(f),
		})
	}

	ranked := rerankMMR(cands, topK)
	results := make([]QueryResult, 0, len(ranked))
	for _, c := range ranked {
		results = append(results, QueryResult{
			Path:          c.file.Path,
			Kind:          c.file.Kind,
			Weight:        c.file.Weight,
			Score:         math.Round(c.rel*100) / 100,
			TokenEstimate: c.file.TokenEstimate,
			Snippet:       makeSnippet(c.file, terms),
		})
	}
	return results, nil
}

// fileTermSet 计算文件的词元集合，用于 MMR 的 Jaccard 相似度。
func fileTermSet(f indexedFile) map[string]bool {
	set := make(map[string]bool)
	for _, term := range tokenize(f.Path + " " + f.ContentSample) {
		set[term] = true
	}
	return set
}

func (s *Store) queryFTS(root string, terms []string, limit int) ([]indexedFile, error) {
	if !s.fts5Enabled || len(terms) == 0 {
		return nil, sql.ErrNoRows
	}
	if limit <= 0 {
		limit = 64
	}
	ftsQuery := makeFTSQuery(terms)
	rows, err := s.db.Query(`SELECT fs.path, fs.kind, fs.weight, fs.token_estimate, fs.size_bytes, fs.content_sample, fs.role
		FROM file_fts AS ft
		JOIN file_summary AS fs ON fs.root = ft.root AND fs.path = ft.path
		WHERE file_fts MATCH ? AND ft.root = ?
		LIMIT ?`, ftsQuery, root, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []indexedFile
	for rows.Next() {
		var file indexedFile
		if err := rows.Scan(&file.Path, &file.Kind, &file.Weight, &file.TokenEstimate, &file.SizeBytes, &file.ContentSample, &file.Role); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (s *Store) queryKeywordFallback(root string, terms []string) ([]indexedFile, error) {
	// 召回集：命中任意词项的路径（含 from_symbol=1 的符号词项），保证符号可被召回。
	recall, err := s.recallPaths(root, terms)
	if err != nil {
		return nil, err
	}
	// 计数：仅统计基础词项（from_symbol=0），作为 keywordCount。
	keywordCounts, err := s.keywordCounts(root, terms)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT path, kind, weight, token_estimate, size_bytes, content_sample, role FROM file_summary WHERE root = ?`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []indexedFile
	for rows.Next() {
		var file indexedFile
		if err := rows.Scan(&file.Path, &file.Kind, &file.Weight, &file.TokenEstimate, &file.SizeBytes, &file.ContentSample, &file.Role); err != nil {
			return nil, err
		}
		if len(recall) > 0 {
			if !recall[file.Path] {
				continue
			}
		}
		file.KeywordCount = keywordCounts[file.Path]
		files = append(files, file)
	}
	return files, rows.Err()
}

// recallPaths 返回命中任意词项的路径集合，不区分 from_symbol，用于召回。
func (s *Store) recallPaths(root string, terms []string) (map[string]bool, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(terms))
	args := make([]any, 0, len(terms)+1)
	args = append(args, root)
	for i, term := range terms {
		placeholders[i] = "?"
		args = append(args, term)
	}
	rows, err := s.db.Query(fmt.Sprintf(`SELECT DISTINCT path FROM keyword_index WHERE root = ? AND term IN (%s)`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		result[path] = true
	}
	return result, rows.Err()
}

func (s *Store) keywordCounts(root string, terms []string) (map[string]int, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(terms))
	args := make([]any, 0, len(terms)+1)
	args = append(args, root)
	for i, term := range terms {
		placeholders[i] = "?"
		args = append(args, term)
	}
	// from_symbol=0 过滤掉折入的符号词项：符号信号由 SymbolScore 显式计入，
	// 不让其重复进入 keywordCount 以保持 Rel 可解释。
	rows, err := s.db.Query(fmt.Sprintf(`SELECT path, SUM(count) FROM keyword_index WHERE root = ? AND from_symbol = 0 AND term IN (%s) GROUP BY path`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var path string
		var count int
		if err := rows.Scan(&path, &count); err != nil {
			return nil, err
		}
		result[path] = count
	}
	return result, rows.Err()
}

func makeSnippet(file indexedFile, terms []string) string {
	source := normalizeSnippet(file.ContentSample)
	if source == "" {
		source = file.Path
	}
	lower := strings.ToLower(source)
	for _, term := range terms {
		if idx := strings.Index(lower, term); idx >= 0 {
			return clipAroundByte(source, idx, 90)
		}
	}
	return clipRunes(source, 180)
}

func normalizeSnippet(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clipAroundByte(s string, byteIndex, radius int) string {
	startRunes := utf8.RuneCountInString(s[:byteIndex]) - radius
	if startRunes < 0 {
		startRunes = 0
	}
	runes := []rune(s)
	endRunes := startRunes + radius*2
	if endRunes > len(runes) {
		endRunes = len(runes)
	}
	prefix := ""
	suffix := ""
	if startRunes > 0 {
		prefix = "..."
	}
	if endRunes < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[startRunes:endRunes]) + suffix
}

func clipRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

func makeFTSQuery(terms []string) string {
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ReplaceAll(term, `"`, `""`)
		quoted = append(quoted, `"`+term+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func termCounts(text string) map[string]int {
	counts := make(map[string]int)
	for _, term := range tokenize(text) {
		counts[term]++
	}
	return counts
}

func tokenize(text string) []string {
	var terms []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := strings.ToLower(string(current))
		if len([]rune(term)) > 1 {
			terms = append(terms, term)
		}
		current = current[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return dedupeTerms(terms)
}

func dedupeTerms(terms []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if seen[term] {
			continue
		}
		seen[term] = true
		result = append(result, term)
	}
	return result
}
