package scan

import (
	"path"
	"strings"
)

// Framework profile 名称：标识仓库中可被识别的框架工程。monorepo 中可多 profile 共存，
// 每个 profile 绑定到声明其 marker 的目录（Root），文件按“最长前缀”归属到对应 profile。
const (
	ProfileVue     = "vue"
	ProfileAngular = "angular"
	ProfileReact   = "react"
	ProfileSpring  = "spring"
)

// Role 是 indexed 文件的职责标签，供检索意图对齐与噪声治理使用。
// 取值固定为以下集合；无法归类的源码留空（""），不强行套用。
const (
	RoleAppEntry        = "app-entry"
	RoleRouteDefinition = "route-definition"
	RolePageView        = "page-view"
	RoleComponent       = "component"
	RoleStore           = "store"
	RoleService         = "service"
	RoleController      = "controller"
	RoleRepository      = "repository"
	RoleConfiguration   = "configuration"
	RoleDocumentation   = "documentation"
	RoleGeneratedNoisy  = "generated-noisy"
)

// FrameworkProfile 是一处被识别的框架工程：Name 为框架名，Root 为声明其 marker 的目录
// （仓库根记为 ""）。文件归属到 Root 为其最长前缀的 profile，从而支持 monorepo 多框架共存。
type FrameworkProfile struct {
	Name string
	Root string
}

// profileSignals 聚合单个 project root 目录（含其子树）观察到的框架特征。
type profileSignals struct {
	angularJSON bool
	viteConfig  bool
	packageJSON bool
	mavenGradle bool
	hasVue      bool
	hasTSXJSX   bool
	hasJava     bool
	springBoot  bool
}

// detectProfiles 从扫描得到的文件列表与 Spring Boot 入口集合推断框架 profile 集合。
// 先确定带有 marker 文件的 project root 目录，再把各文件的语言/特征信号聚合到其所有祖先
// project root，最后按规则分类。规则为纯路径/特征启发（先规则后模型），不依赖 LLM。
func detectProfiles(files []FileEntry, springApp map[string]bool) []FrameworkProfile {
	// 1) project root：直接包含 marker 文件的目录。
	roots := map[string]*profileSignals{}
	ensure := func(dir string) *profileSignals {
		sig, ok := roots[dir]
		if !ok {
			sig = &profileSignals{}
			roots[dir] = sig
		}
		return sig
	}
	for _, f := range files {
		dir := dirOf(f.Path)
		base := strings.ToLower(path.Base(f.Path))
		switch base {
		case "angular.json", "package.json", "pom.xml", "build.gradle", "settings.gradle":
			ensure(dir)
		}
		if strings.HasPrefix(base, "vite.config.") {
			ensure(dir)
		}
	}
	if len(roots) == 0 {
		return nil
	}

	// 2) 把每个文件的特征 OR 进它所有祖先 project root。
	for _, f := range files {
		base := strings.ToLower(path.Base(f.Path))
		ext := strings.ToLower(path.Ext(base))
		for dir, sig := range roots {
			if !underDir(f.Path, dir) {
				continue
			}
			switch base {
			case "angular.json":
				sig.angularJSON = true
			case "package.json":
				sig.packageJSON = true
			case "pom.xml", "build.gradle", "settings.gradle":
				sig.mavenGradle = true
			}
			if strings.HasPrefix(base, "vite.config.") {
				sig.viteConfig = true
			}
			switch ext {
			case ".vue":
				sig.hasVue = true
			case ".tsx", ".jsx":
				sig.hasTSXJSX = true
			case ".java", ".kt":
				sig.hasJava = true
			}
			if springApp[f.Path] {
				sig.springBoot = true
			}
		}
	}

	// 3) 分类：angular.json 优先；其次 Maven/Gradle + Java(优先 @SpringBootApplication)；
	// 再次 vite/package.json + .vue；最后 package.json + tsx/jsx。互斥取首个命中。
	var profiles []FrameworkProfile
	for dir, sig := range roots {
		name := classifyProfile(sig)
		if name != "" {
			profiles = append(profiles, FrameworkProfile{Name: name, Root: dir})
		}
	}
	return profiles
}

func classifyProfile(sig *profileSignals) string {
	switch {
	case sig.angularJSON:
		return ProfileAngular
	case sig.mavenGradle && (sig.springBoot || sig.hasJava):
		return ProfileSpring
	case (sig.viteConfig || sig.packageJSON) && sig.hasVue:
		return ProfileVue
	case sig.packageJSON && sig.hasTSXJSX:
		return ProfileReact
	default:
		return ""
	}
}

// profileForPath 返回 rel 文件归属的 profile 名：在所有 Root 为其前缀的 profile 中取 Root 最长者。
func profileForPath(profiles []FrameworkProfile, rel string) string {
	name := ""
	best := -1
	for _, p := range profiles {
		if !underDir(rel, p.Root) {
			continue
		}
		if len(p.Root) > best {
			best = len(p.Root)
			name = p.Name
		}
	}
	return name
}

// roleForRel 依据相对路径与文件分档分配真实 role。规则为纯路径/扩展名启发，
// 顺序敏感：先判生成噪声，再按 documentation/configuration，最后在源码内按职责细分。
// 无法归类的源码（如 entity、composable、util）返回 ""，不强行套用 role。
func roleForRel(rel string, kind FileKind) string {
	lower := strings.ToLower(strings.ReplaceAll(rel, "\\", "/"))
	base := path.Base(lower)

	if isGeneratedNoisyRel(base) {
		return RoleGeneratedNoisy
	}
	// app-entry 先于 kind 判定：Spring 的 *Application.java 会被 classify 规则归为 config
	// kind（与 application.properties 共用前缀），若按 kind 判定会误标为 configuration。
	if isAppEntryRel(base) {
		return RoleAppEntry
	}
	if kind == KindDocumentation {
		return RoleDocumentation
	}
	if kind == KindConfig {
		return RoleConfiguration
	}
	if kind != KindSource {
		return ""
	}

	switch {
	case strings.Contains(lower, "/router") || strings.Contains(lower, "/routes") ||
		base == "router.ts" || base == "router.tsx" || base == "router.js" ||
		strings.HasSuffix(base, "-routing.module.ts"):
		return RoleRouteDefinition
	case strings.Contains(lower, "/stores/") || strings.Contains(lower, "/store/") ||
		strings.HasSuffix(base, "slice.ts") || strings.HasSuffix(base, "slice.tsx") ||
		strings.HasSuffix(base, ".store.ts"):
		return RoleStore
	case strings.Contains(lower, "/services/") || strings.Contains(lower, "/service/") ||
		strings.HasSuffix(base, ".service.ts") ||
		strings.HasSuffix(base, "service.java") || strings.HasSuffix(base, "service.kt") ||
		strings.HasSuffix(base, "api.ts") || strings.HasSuffix(base, "api.tsx"):
		return RoleService
	case strings.Contains(lower, "/controller") ||
		strings.HasSuffix(base, "controller.java") || strings.HasSuffix(base, "controller.kt"):
		return RoleController
	case strings.Contains(lower, "/repository") || strings.Contains(lower, "/repositories") ||
		strings.Contains(lower, "/dao") || strings.Contains(lower, "/mapper") ||
		strings.HasSuffix(base, "repository.java") || strings.HasSuffix(base, "mapper.java") ||
		strings.HasSuffix(base, "dao.java"):
		return RoleRepository
	case strings.Contains(lower, "/pages/") || strings.Contains(lower, "/views/") ||
		strings.HasSuffix(base, "page.vue") || strings.HasSuffix(base, "page.tsx") ||
		strings.HasSuffix(base, "view.vue"):
		return RolePageView
	case strings.Contains(lower, "/components/") ||
		strings.HasSuffix(base, ".component.ts") ||
		strings.HasSuffix(base, ".vue") ||
		strings.HasSuffix(base, "component.tsx"):
		return RoleComponent
	default:
		return ""
	}
}

// isAppEntryRel 依据文件名判定是否为应用入口（前端 main/index 入口或 Spring Boot 入口）。
func isAppEntryRel(base string) bool {
	switch base {
	case "main.ts", "main.tsx", "main.js", "main.jsx", "index.tsx":
		return true
	}
	return strings.HasSuffix(base, "application.java") || strings.HasSuffix(base, "application.kt")
}

// isGeneratedNoisyRel 判定文件名是否为生成/噪声产物（当前覆盖各类 *.d.ts 声明文件）。
// role 落地后由本函数充当 generated-noisy 的唯一判定来源。
func isGeneratedNoisyRel(base string) bool {
	return strings.HasSuffix(base, ".d.ts")
}

// dirOf 返回斜杠路径的目录部分，根级文件返回 ""。
func dirOf(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

// underDir 判断 rel 是否位于目录 dir（含 dir 自身）之下；dir=="" 代表仓库根，匹配全部。
func underDir(rel, dir string) bool {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if dir == "" {
		return true
	}
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}
