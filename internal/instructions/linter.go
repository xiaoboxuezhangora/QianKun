package instructions

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var defaultTargets = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	filepath.ToSlash(filepath.Join(".github", "copilot-instructions.md")),
}

var (
	datePattern       = regexp.MustCompile(`\b20\d{2}[-/]\d{2}[-/]\d{2}\b`)
	uuidPattern       = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	randomIDPattern   = regexp.MustCompile(`\b[a-f0-9]{24,64}\b`)
	secretLinePattern = regexp.MustCompile(`(?i)(api[_-]?key|password|secret|token|cookie|private[_-]?key)\s*[:=]\s*['"]?[A-Za-z0-9_./+=-]{12,}`)
	privateKeyPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

func Lint(opts Options) (Report, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Root:        root,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	paragraphs := make(map[string]Finding)
	for _, rel := range defaultTargets {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			report.Findings = append(report.Findings, Finding{
				Severity: "QUALITY",
				Rule:     "read_error",
				File:     rel,
				Message:  err.Error(),
			})
			continue
		}
		report.FilesRead++
		analyzeFile(&report, rel, data, paragraphs)
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity == report.Findings[j].Severity {
			if report.Findings[i].File == report.Findings[j].File {
				return report.Findings[i].Line < report.Findings[j].Line
			}
			return report.Findings[i].File < report.Findings[j].File
		}
		return severityRank(report.Findings[i].Severity) < severityRank(report.Findings[j].Severity)
	})
	return report, nil
}

func analyzeFile(report *Report, rel string, data []byte, paragraphs map[string]Finding) {
	lines := bytes.Count(data, []byte{'\n'}) + 1
	if lines > 300 || len(data) > 24*1024 {
		report.Findings = append(report.Findings, Finding{
			Severity: "QUALITY",
			Rule:     "file_too_long",
			File:     rel,
			Message:  "instructions file is long; consider path-scoped rules or concise summaries",
		})
	}
	if !hasScopeMarker(string(data)) {
		report.Findings = append(report.Findings, Finding{
			Severity: "QUALITY",
			Rule:     "missing_scope",
			File:     rel,
			Message:  "no clear path or applicability scope marker found",
		})
	}
	scanLines(report, rel, data)
	scanDuplicateParagraphs(report, rel, data, paragraphs)
}

func scanLines(report *Report, rel string, data []byte) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		switch {
		case privateKeyPattern.MatchString(line) || secretLinePattern.MatchString(line):
			report.Findings = append(report.Findings, Finding{
				Severity: "SECURITY",
				Rule:     "secret_pattern",
				File:     rel,
				Line:     lineNo,
				Message:  "line looks like a secret, token, cookie, password, or private key",
				Excerpt:  trimExcerpt(line),
			})
		case datePattern.MatchString(line) || uuidPattern.MatchString(line) || strings.Contains(line, "/tmp/") || strings.Contains(strings.ToLower(line), "current sprint"):
			report.Findings = append(report.Findings, Finding{
				Severity: "QUALITY",
				Rule:     "dynamic_info",
				File:     rel,
				Line:     lineNo,
				Message:  "line appears to contain date, temporary path, sprint state, or random identifier",
				Excerpt:  trimExcerpt(line),
			})
		case randomIDPattern.MatchString(line) && !strings.Contains(line, "sha256"):
			report.Findings = append(report.Findings, Finding{
				Severity: "QUALITY",
				Rule:     "dynamic_info",
				File:     rel,
				Line:     lineNo,
				Message:  "line appears to contain a random identifier",
				Excerpt:  trimExcerpt(line),
			})
		}
	}
}

func scanDuplicateParagraphs(report *Report, rel string, data []byte, paragraphs map[string]Finding) {
	parts := strings.Split(string(data), "\n\n")
	line := 1
	for _, part := range parts {
		normalized := normalizeParagraph(part)
		if len([]rune(normalized)) >= 100 {
			if first, ok := paragraphs[normalized]; ok {
				report.Findings = append(report.Findings, Finding{
					Severity: "QUALITY",
					Rule:     "duplicate_paragraph",
					File:     rel,
					Line:     line,
					Message:  "paragraph duplicates instructions from " + first.File,
					Excerpt:  trimExcerpt(part),
				})
			} else {
				paragraphs[normalized] = Finding{File: rel, Line: line}
			}
		}
		line += strings.Count(part, "\n") + 2
	}
}

func hasScopeMarker(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{"scope", "path", "路径", "作用域", "适用", "范围"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeParagraph(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func trimExcerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return s
}

func severityRank(severity string) int {
	switch severity {
	case "SECURITY":
		return 0
	case "TYPE":
		return 1
	case "ARCH":
		return 2
	case "QUALITY":
		return 3
	case "PERF":
		return 4
	default:
		return 5
	}
}
