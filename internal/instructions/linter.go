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

// 警告严重度分档。Linter 仍是 report-only：只提示不擅自修改用户指令文件，
// 严重度只用于排序与在周报中分级呈现，帮助用户优先处理高收益/高风险的异味。
const (
	SeverityHigh   = "HIGH"
	SeverityMedium = "MEDIUM"
	SeverityLow    = "LOW"
)

// ruleMeta 为每类指令异味定义严重度与可执行的修复建议。
type ruleMeta struct {
	Severity   string
	Suggestion string
}

// ruleCatalog 集中维护每个 rule 的严重度与修复建议，避免散落在各扫描函数里。
// 修复建议描述“用户可以怎么改”，Linter 自身不改写指令文件。
var ruleCatalog = map[string]ruleMeta{
	"secret_pattern": {
		Severity:   SeverityHigh,
		Suggestion: "疑似明文凭据：删除密钥/令牌/口令/私钥，改用环境变量或密钥管理；指令文件只描述获取方式，不存储真实值。",
	},
	"file_too_long": {
		Severity:   SeverityMedium,
		Suggestion: "文件过长：拆分为按路径或语言作用域的子指令，或提炼为简明要点；长背景移入独立文档并以链接引用。",
	},
	"missing_scope": {
		Severity:   SeverityMedium,
		Suggestion: "缺少作用域：在文件开头声明适用范围（路径/语言/模块），帮助助手判断何时套用该指令。",
	},
	"dynamic_info": {
		Severity:   SeverityMedium,
		Suggestion: "包含易过期信息（日期/临时路径/迭代状态/随机 ID）：移除或改为引用动态来源，避免指令很快失真。",
	},
	"duplicate_paragraph": {
		Severity:   SeverityLow,
		Suggestion: "段落重复：仅在单一权威文件保留，其它文件以链接引用，减少维护分叉与重复上下文的 token 浪费。",
	},
	"read_error": {
		Severity:   SeverityLow,
		Suggestion: "指令文件无法读取：确认路径与权限，或将其从治理范围中移除。",
	},
}

// newFinding 依据 ruleCatalog 自动填充严重度与修复建议，使各扫描函数只关注定位异味。
func newFinding(rule, file string, line int, message, excerpt string) Finding {
	meta := ruleCatalog[rule]
	severity := meta.Severity
	if severity == "" {
		severity = SeverityLow
	}
	return Finding{
		Severity:   severity,
		Rule:       rule,
		File:       file,
		Line:       line,
		Message:    message,
		Suggestion: meta.Suggestion,
		Excerpt:    excerpt,
	}
}

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
			report.Findings = append(report.Findings, newFinding("read_error", rel, 0, err.Error(), ""))
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
		report.Findings = append(report.Findings, newFinding("file_too_long", rel, 0,
			"instructions file is long; consider path-scoped rules or concise summaries", ""))
	}
	if !hasScopeMarker(string(data)) {
		report.Findings = append(report.Findings, newFinding("missing_scope", rel, 0,
			"no clear path or applicability scope marker found", ""))
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
			report.Findings = append(report.Findings, newFinding("secret_pattern", rel, lineNo,
				"line looks like a secret, token, cookie, password, or private key", trimExcerpt(line)))
		case datePattern.MatchString(line) || uuidPattern.MatchString(line) || strings.Contains(line, "/tmp/") || strings.Contains(strings.ToLower(line), "current sprint"):
			report.Findings = append(report.Findings, newFinding("dynamic_info", rel, lineNo,
				"line appears to contain date, temporary path, sprint state, or random identifier", trimExcerpt(line)))
		case randomIDPattern.MatchString(line) && !strings.Contains(line, "sha256"):
			report.Findings = append(report.Findings, newFinding("dynamic_info", rel, lineNo,
				"line appears to contain a random identifier", trimExcerpt(line)))
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
				report.Findings = append(report.Findings, newFinding("duplicate_paragraph", rel, line,
					"paragraph duplicates instructions from "+first.File, trimExcerpt(part)))
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
	case SeverityHigh:
		return 0
	case SeverityMedium:
		return 1
	case SeverityLow:
		return 2
	default:
		return 3
	}
}
