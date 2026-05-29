package scan

import (
	"path"
	"strings"
)

func classifyFile(rel string) (FileKind, int, bool, string) {
	rel = normalizeRel(rel)
	if isLockfile(rel) {
		return KindLockfile, 5, true, SkipLockfile
	}
	if isAICachePath(rel) {
		return KindAICache, 0, true, SkipAICache
	}
	if isBinaryPath(rel) {
		return KindBinary, 0, true, SkipBinary
	}
	if isBuildArtifactFile(rel) {
		return KindBuildArtifact, 0, true, SkipBuildArtifact
	}

	kind := inferTextKind(rel)
	return kind, weightForKind(kind), false, ""
}

func classifySkippedDir(rel string) (FileKind, string, bool) {
	rel = normalizeRel(rel)
	if rel == "" {
		return KindUnknown, "", false
	}
	if hasPathPrefix(rel, ".opencode/skills") || hasPathPrefix(rel, ".ai-docs") {
		return KindAICache, SkipAICache, true
	}

	for _, segment := range strings.Split(rel, "/") {
		switch segment {
		case "node_modules", "dist", "build", "target", "coverage", "tmp", "logs":
			return KindBuildArtifact, SkipBuildArtifact, true
		case ".git", ".idea", ".gradle":
			return KindIDEConfig, SkipIDEConfig, true
		}
	}
	return KindUnknown, "", false
}

func inferTextKind(rel string) FileKind {
	name := path.Base(rel)
	lowerName := strings.ToLower(name)
	ext := strings.ToLower(path.Ext(name))

	if lowerName == "readme" || strings.HasPrefix(lowerName, "readme.") {
		return KindDocumentation
	}
	switch ext {
	case ".md", ".markdown", ".rst", ".adoc", ".txt":
		return KindDocumentation
	}

	switch lowerName {
	case "package.json", "tsconfig.json", "jsconfig.json", "angular.json", "project.json", "nx.json",
		"go.mod", "go.sum", "pom.xml", "build.gradle", "settings.gradle", "makefile", "dockerfile",
		".gitignore", ".contextgateignore":
		return KindConfig
	}
	if strings.HasPrefix(lowerName, "vite.config.") ||
		strings.HasPrefix(lowerName, "next.config.") ||
		strings.HasPrefix(lowerName, "application.") ||
		strings.HasPrefix(lowerName, "application-") {
		return KindConfig
	}
	switch ext {
	case ".json", ".yaml", ".yml", ".toml", ".xml", ".ini", ".properties", ".env":
		return KindConfig
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".vue", ".java", ".kt", ".kts", ".py", ".rb",
		".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".php", ".swift", ".m", ".mm",
		".sql", ".sh", ".bash", ".zsh", ".ps1", ".html", ".css", ".scss", ".sass", ".less":
		return KindSource
	default:
		return KindUnknown
	}
}

func weightForKind(kind FileKind) int {
	switch kind {
	case KindSource:
		return 90
	case KindConfig:
		return 80
	case KindDocumentation:
		return 50
	case KindUnknown:
		return 20
	default:
		return 0
	}
}

func isLockfile(rel string) bool {
	name := strings.ToLower(path.Base(rel))
	if strings.HasSuffix(name, ".lock") {
		return true
	}
	switch name {
	case "pnpm-lock.yaml", "package-lock.json", "yarn.lock":
		return true
	default:
		return false
	}
}

func isAICachePath(rel string) bool {
	return rel == ".claude/settings.local.json" ||
		hasPathPrefix(rel, ".opencode/skills") ||
		hasPathPrefix(rel, ".ai-docs")
}

func isBuildArtifactFile(rel string) bool {
	name := strings.ToLower(path.Base(rel))
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") {
		return true
	}
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".map", ".class":
		return true
	default:
		return false
	}
}

func isBinaryPath(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg", ".svgz", ".pdf", ".zip", ".gz", ".tgz",
		".jar", ".war", ".ear", ".class", ".apk", ".ipa", ".dmg", ".exe", ".dll", ".so", ".dylib",
		".woff", ".woff2", ".ttf", ".otf", ".eot", ".bin":
		return true
	default:
		return false
	}
}

func hasPathPrefix(rel, prefix string) bool {
	rel = normalizeRel(rel)
	prefix = normalizeRel(prefix)
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}
