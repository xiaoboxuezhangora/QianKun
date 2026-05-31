// Package commands 实现“命令发现”：只从真实来源（package.json scripts、
// Maven pom.xml、Gradle 构建脚本、框架配置）解析开发者真正可用的命令，
// 作为可被 memory-query 返回的上下文。
//
// 红线：严禁伪造命令。例如 package.json 没有 test 脚本时，绝不返回
// “pnpm test” 之类不存在的命令；build / lint / type-check 等分类必须来自
// 真实存在的 script 或真实声明的构建任务/插件。
package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Category 表示命令的语义分类。分类只是对“真实存在”命令的归类标签，
// 不会凭空创造命令本身。
type Category string

const (
	CategoryBuild     Category = "build"
	CategoryTest      Category = "test"
	CategoryLint      Category = "lint"
	CategoryTypeCheck Category = "type-check"
	CategoryDev       Category = "dev"
	// CategoryRun 是兜底分类：真实存在但不属于上述语义的脚本/任务，
	// 例如 preview、sync-android、build-release-apk 等。
	CategoryRun Category = "run"
)

// Source 标注命令来自哪个真实来源文件。
type Source string

const (
	SourcePackageJSON Source = "package.json"
	SourcePomXML      Source = "pom.xml"
	SourceGradle      Source = "build.gradle"
)

// Command 描述一条被发现的、真实可执行的命令。
type Command struct {
	Name       string   `json:"name"`              // 脚本名 / 任务名 / 目标名
	Category   Category `json:"category"`          // 语义分类
	Source     Source   `json:"source"`            // 真实来源
	Invocation string   `json:"invocation"`        // 推荐调用方式，如 "pnpm run build"
	Raw        string   `json:"raw,omitempty"`     // 原始脚本/目标，便于审计与溯源
}

// Discovery 是一次命令发现的结果。
type Discovery struct {
	Root           string    `json:"root"`
	PackageManager string    `json:"package_manager,omitempty"` // 仅当存在 package.json 时给出
	Commands       []Command `json:"commands"`
}

// packageJSON 仅声明命令发现关心的字段，避免把整份 manifest 读进内存。
type packageJSON struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
}

// Discover 在 root 上执行命令发现。它是纯文件系统操作，不调用任何 LLM，
// 任何来源缺失或解析失败都只跳过该来源，绝不阻断或伪造。
func Discover(root string) (Discovery, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Discovery{}, err
	}
	disc := Discovery{Root: absRoot}

	pm := detectPackageManager(absRoot)
	if cmds, ok := discoverPackageJSON(absRoot, pm); ok {
		disc.PackageManager = pm
		disc.Commands = append(disc.Commands, cmds...)
	}
	disc.Commands = append(disc.Commands, discoverMaven(absRoot)...)
	disc.Commands = append(disc.Commands, discoverGradle(absRoot)...)

	sortCommands(disc.Commands)
	return disc, nil
}

// detectPackageManager 依据真实存在的 lockfile / packageManager 字段推断包管理器，
// 推断不出时回退到 npm（npm 是 Node 项目最通用的默认值，且 `npm run` 必然可用）。
func detectPackageManager(root string) string {
	switch {
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(root, "yarn.lock")):
		return "yarn"
	case fileExists(filepath.Join(root, "bun.lockb")):
		return "bun"
	case fileExists(filepath.Join(root, "package-lock.json")):
		return "npm"
	}
	// 退而求其次：读取 package.json 的 packageManager 字段（如 "pnpm@9.0.0"）。
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pkg packageJSON
		if json.Unmarshal(data, &pkg) == nil && pkg.PackageManager != "" {
			name := pkg.PackageManager
			if idx := strings.IndexByte(name, '@'); idx > 0 {
				name = name[:idx]
			}
			return name
		}
	}
	return "npm"
}

// discoverPackageJSON 解析 package.json 的 scripts。仅当文件存在且确有 scripts
// 时返回命令；返回的每条命令都对应一个真实声明的脚本，绝不补全缺失脚本。
func discoverPackageJSON(root, pm string) ([]Command, bool) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil, false
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, false
	}
	if len(pkg.Scripts) == 0 {
		return nil, true
	}

	cmds := make([]Command, 0, len(pkg.Scripts))
	for name, body := range pkg.Scripts {
		cmds = append(cmds, Command{
			Name:       name,
			Category:   categorizeScript(name, body),
			Source:     SourcePackageJSON,
			Invocation: invocation(pm, name),
			Raw:        body,
		})
	}
	return cmds, true
}

// invocation 给出推荐调用方式。pnpm/npm/bun 使用 `<pm> run <name>` 形式以避免与
// 内置子命令歧义；yarn 习惯直接 `yarn <name>`。
func invocation(pm, name string) string {
	switch pm {
	case "yarn":
		return "yarn " + name
	case "bun":
		return "bun run " + name
	case "pnpm":
		return "pnpm run " + name
	default:
		return "npm run " + name
	}
}

// categorizeScript 依据脚本名与脚本体推断语义分类。分类只影响标签，
// 不影响“是否存在”，因此即便分类不确定也只是落到 CategoryRun，不会丢弃命令。
//
// 判定顺序很重要：移动端/打包类专有任务（如 build-release-apk）必须先于
// build 规则识别，避免被名字里的 “build” 误吞。
func categorizeScript(name, body string) Category {
	n := strings.ToLower(name)
	b := strings.ToLower(body)

	// 1) 移动端打包 / Capacitor / 原生任务：优先归到 run，避免被 build 误判。
	if containsAny(n, "android", "ios", "apk") ||
		containsAny(b, "cap ", "cap sync", "capacitor", "jetpack ", "gradlew", "xcodebuild") {
		return CategoryRun
	}
	// 2) type-check
	if containsAny(n, "type-check", "typecheck", "tsc") ||
		containsAny(b, "vue-tsc", "tsc --noemit", "tsc --no-emit") {
		return CategoryTypeCheck
	}
	// 3) lint / format-check
	if n == "lint" || strings.HasPrefix(n, "lint") ||
		containsAny(b, "eslint", "stylelint", "biome lint", "biome check", "oxlint") {
		return CategoryLint
	}
	// 4) test —— 仅当确有测试脚本/测试运行器时命中（红线：不臆造 test）。
	if n == "test" || strings.HasPrefix(n, "test:") || strings.HasPrefix(n, "test-") ||
		containsAny(b, "vitest", "jest", "playwright", "mocha", "cypress", "ava ", "node --test") {
		return CategoryTest
	}
	// 5) build
	if n == "build" || strings.HasPrefix(n, "build") ||
		containsAny(b, "vite build", "tsc -b", "tsc --build", "webpack", "rollup -c",
			"next build", "nuxt build", "vue-cli-service build", "ng build") {
		return CategoryBuild
	}
	// 6) dev / serve
	if n == "dev" || n == "serve" || n == "start" || strings.HasPrefix(n, "dev:") ||
		containsAny(b, "vite dev", "vite serve", "vite --", "next dev", "nuxt dev",
			"webpack serve", "webpack-dev-server", "vue-cli-service serve", "ng serve", "nodemon") {
		return CategoryDev
	}
	return CategoryRun
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// sortCommands 给出确定性顺序：先按来源、再按分类优先级、最后按名字，
// 便于测试断言与稳定输出。
func sortCommands(cmds []Command) {
	sort.SliceStable(cmds, func(i, j int) bool {
		if cmds[i].Source != cmds[j].Source {
			return cmds[i].Source < cmds[j].Source
		}
		pi, pj := categoryRank(cmds[i].Category), categoryRank(cmds[j].Category)
		if pi != pj {
			return pi < pj
		}
		return cmds[i].Name < cmds[j].Name
	})
}

func categoryRank(c Category) int {
	switch c {
	case CategoryBuild:
		return 0
	case CategoryTest:
		return 1
	case CategoryLint:
		return 2
	case CategoryTypeCheck:
		return 3
	case CategoryDev:
		return 4
	default:
		return 5
	}
}
