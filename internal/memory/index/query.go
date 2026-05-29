package index

import (
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"sort"
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

	files, err := s.queryFTS(absRoot, terms, topK*8)
	if err != nil || len(files) == 0 {
		files, err = s.queryKeywordFallback(absRoot, terms)
		if err != nil {
			return QueryResponse{}, err
		}
	}

	results := rankFiles(files, terms, topK)
	return QueryResponse{
		Root:    absRoot,
		Query:   query,
		TopK:    topK,
		Results: results,
	}, nil
}

func (s *Store) queryFTS(root string, terms []string, limit int) ([]indexedFile, error) {
	if !s.fts5Enabled || len(terms) == 0 {
		return nil, sql.ErrNoRows
	}
	if limit <= 0 {
		limit = 64
	}
	ftsQuery := makeFTSQuery(terms)
	rows, err := s.db.Query(`SELECT fs.path, fs.kind, fs.weight, fs.token_estimate, fs.size_bytes, fs.content_sample
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
		if err := rows.Scan(&file.Path, &file.Kind, &file.Weight, &file.TokenEstimate, &file.SizeBytes, &file.ContentSample); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (s *Store) queryKeywordFallback(root string, terms []string) ([]indexedFile, error) {
	keywordCounts, err := s.keywordCounts(root, terms)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT path, kind, weight, token_estimate, size_bytes, content_sample FROM file_summary WHERE root = ?`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []indexedFile
	for rows.Next() {
		var file indexedFile
		if err := rows.Scan(&file.Path, &file.Kind, &file.Weight, &file.TokenEstimate, &file.SizeBytes, &file.ContentSample); err != nil {
			return nil, err
		}
		if len(keywordCounts) > 0 {
			count, ok := keywordCounts[file.Path]
			if !ok {
				continue
			}
			file.KeywordCount = count
		}
		files = append(files, file)
	}
	return files, rows.Err()
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
	rows, err := s.db.Query(fmt.Sprintf(`SELECT path, SUM(count) FROM keyword_index WHERE root = ? AND term IN (%s) GROUP BY path`, strings.Join(placeholders, ",")), args...)
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

func rankFiles(files []indexedFile, terms []string, topK int) []QueryResult {
	results := make([]QueryResult, 0, len(files))
	for _, file := range files {
		score := scoreFile(file, terms)
		results = append(results, QueryResult{
			Path:          file.Path,
			Kind:          file.Kind,
			Weight:        file.Weight,
			Score:         math.Round(score*100) / 100,
			TokenEstimate: file.TokenEstimate,
			Snippet:       makeSnippet(file, terms),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Path < results[j].Path
		}
		return results[i].Score > results[j].Score
	})
	if topK > len(results) {
		topK = len(results)
	}
	return results[:topK]
}

func scoreFile(file indexedFile, terms []string) float64 {
	score := float64(file.Weight)
	lowerPath := strings.ToLower(file.Path)
	lowerKind := strings.ToLower(file.Kind)
	lowerContent := strings.ToLower(file.ContentSample)
	for _, term := range terms {
		if strings.Contains(lowerPath, term) {
			score += 35
		}
		if strings.Contains(lowerKind, term) {
			score += 10
		}
		if count := strings.Count(lowerContent, term); count > 0 {
			score += math.Min(float64(count), 5) * 12
		}
	}
	score += float64(file.KeywordCount) * 4
	if file.SizeBytes > 32*1024 {
		score -= math.Min(float64(file.SizeBytes)/(32*1024), 20)
	}
	if score < 0 {
		return 0
	}
	return score
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
