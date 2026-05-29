package scan

import (
	"bufio"
	"os"
	"path"
	"regexp"
	"strings"
)

type patternSet []string

func (p patternSet) match(rel string) bool {
	rel = normalizeRel(rel)
	for _, pattern := range p {
		if matchPattern(pattern, rel) {
			return true
		}
	}
	return false
}

type ignoreRule struct {
	pattern       string
	source        string
	directoryOnly bool
}

func loadIgnoreRules(root string) []ignoreRule {
	var rules []ignoreRule
	rules = append(rules, readIgnoreFile(root, ".gitignore", SkipGitignore)...)
	rules = append(rules, readIgnoreFile(root, ".contextgateignore", SkipContextgateIgnore)...)
	return rules
}

func readIgnoreFile(root, name, source string) []ignoreRule {
	file, err := os.Open(pathFromRoot(root, name))
	if err != nil {
		return nil
	}
	defer file.Close()

	var rules []ignoreRule
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		directoryOnly := strings.HasSuffix(line, "/")
		line = strings.TrimPrefix(strings.TrimSuffix(line, "/"), "/")
		if line == "" {
			continue
		}
		rules = append(rules, ignoreRule{
			pattern:       normalizePattern(line),
			source:        source,
			directoryOnly: directoryOnly,
		})
	}
	return rules
}

func matchIgnore(rules []ignoreRule, rel string, isDir bool) (string, bool) {
	rel = normalizeRel(rel)
	for _, rule := range rules {
		if rule.directoryOnly {
			if matchPattern(rule.pattern, rel) || strings.HasPrefix(rel, rule.pattern+"/") {
				return rule.source, true
			}
			continue
		}
		if matchPattern(rule.pattern, rel) {
			return rule.source, true
		}
		if !isDir && strings.HasPrefix(rel, rule.pattern+"/") {
			return rule.source, true
		}
	}
	return "", false
}

func matchPattern(pattern, rel string) bool {
	pattern = normalizePattern(pattern)
	rel = normalizeRel(rel)
	if pattern == "" || rel == "" {
		return false
	}
	if pattern == rel || strings.HasPrefix(rel, pattern+"/") {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}
	if strings.Contains(pattern, "**") {
		return regexpMatch(globToRegex(pattern), rel)
	}
	if strings.Contains(pattern, "/") {
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		return strings.HasSuffix(rel, "/"+pattern)
	}

	base := path.Base(rel)
	if ok, _ := path.Match(pattern, base); ok {
		return true
	}
	for _, segment := range strings.Split(rel, "/") {
		if ok, _ := path.Match(pattern, segment); ok {
			return true
		}
	}
	return false
}

func regexpMatch(pattern, rel string) bool {
	ok, err := regexp.MatchString(pattern, rel)
	return err == nil && ok
}

func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

func normalizePattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	pattern = strings.TrimPrefix(pattern, "./")
	pattern = strings.TrimPrefix(pattern, "/")
	return normalizeRel(pattern)
}

func normalizeRel(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	rel = path.Clean(rel)
	if rel == "." {
		return ""
	}
	return rel
}
