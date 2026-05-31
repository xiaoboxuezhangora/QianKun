package commands

import (
	"path/filepath"
	"runtime"
	"testing"
)

// fixtureRoot 返回 testdata 下指定 fixture 的绝对路径。
func fixtureRoot(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	// commands_test.go 位于 internal/memory/commands，回到仓库根再进 testdata。
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
	return filepath.Join(repoRoot, "testdata", name)
}

// byName 把命令切片转成按名字索引的 map，便于断言。
func byName(cmds []Command) map[string]Command {
	m := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		m[c.Name] = c
	}
	return m
}

func TestDiscoverPackageJSONRealScriptsOnly(t *testing.T) {
	disc, err := Discover(fixtureRoot(t, "command-fixture"))
	if err != nil {
		t.Fatalf("Discover 出错: %v", err)
	}
	if disc.PackageManager != "pnpm" {
		t.Fatalf("期望 pnpm，得到 %q", disc.PackageManager)
	}

	cmds := byName(disc.Commands)

	// 红线：fixture 没有 test 脚本，绝不能出现任何 test 命令。
	if _, ok := cmds["test"]; ok {
		t.Fatal("不应伪造 test 命令")
	}
	for _, c := range disc.Commands {
		if c.Source == SourcePackageJSON && c.Category == CategoryTest {
			t.Fatalf("不应从 package.json 产生 test 分类命令: %+v", c)
		}
	}

	// 真实脚本与分类必须正确。
	checks := []struct {
		name       string
		category   Category
		invocation string
	}{
		{"build", CategoryBuild, "pnpm run build"},
		{"build-only", CategoryBuild, "pnpm run build-only"},
		{"type-check", CategoryTypeCheck, "pnpm run type-check"},
		{"lint", CategoryLint, "pnpm run lint"},
		{"dev", CategoryDev, "pnpm run dev"},
		// 移动端打包脚本必须被发现，且不被名字里的 build 误判为 build。
		{"sync-android", CategoryRun, "pnpm run sync-android"},
		{"build-release-apk", CategoryRun, "pnpm run build-release-apk"},
		{"build-debug-apk", CategoryRun, "pnpm run build-debug-apk"},
	}
	for _, want := range checks {
		got, ok := cmds[want.name]
		if !ok {
			t.Fatalf("缺少命令 %q", want.name)
		}
		if got.Category != want.category {
			t.Fatalf("命令 %q 分类期望 %q，得到 %q", want.name, want.category, got.Category)
		}
		if got.Invocation != want.invocation {
			t.Fatalf("命令 %q 调用方式期望 %q，得到 %q", want.name, want.invocation, got.Invocation)
		}
		if got.Source != SourcePackageJSON {
			t.Fatalf("命令 %q 来源期望 package.json，得到 %q", want.name, got.Source)
		}
		if got.Raw == "" {
			t.Fatalf("命令 %q 应保留原始脚本以便审计", want.name)
		}
	}
}

func TestDiscoverNoPackageJSONNoCommands(t *testing.T) {
	// 用一个不含任何来源文件的目录，确认不臆造任何命令。
	disc, err := Discover(t.TempDir())
	if err != nil {
		t.Fatalf("Discover 出错: %v", err)
	}
	if len(disc.Commands) != 0 {
		t.Fatalf("空目录不应发现命令，得到 %+v", disc.Commands)
	}
	if disc.PackageManager != "" {
		t.Fatalf("无 package.json 时不应给出包管理器，得到 %q", disc.PackageManager)
	}
}

func TestDiscoverMaven(t *testing.T) {
	disc, err := Discover(fixtureRoot(t, "command-maven-fixture"))
	if err != nil {
		t.Fatalf("Discover 出错: %v", err)
	}
	cmds := byName(disc.Commands)

	pkg, ok := cmds["package"]
	if !ok || pkg.Category != CategoryBuild || pkg.Source != SourcePomXML {
		t.Fatalf("缺少正确的 Maven package 命令: %+v", pkg)
	}
	if pkg.Invocation != "mvn -DskipTests package" {
		t.Fatalf("Maven package 调用方式异常: %q", pkg.Invocation)
	}
	// fixture 含 src/test，应给出 test；其来源是真实的测试目录而非臆造。
	if test, ok := cmds["test"]; !ok || test.Category != CategoryTest {
		t.Fatalf("含 src/test 时应给出 Maven test 命令: %+v", test)
	}
	// pom 真实声明 spring-boot-maven-plugin，应给出 spring-boot:run。
	if run, ok := cmds["spring-boot:run"]; !ok || run.Category != CategoryDev {
		t.Fatalf("应从 spring-boot 插件发现 spring-boot:run: %+v", run)
	}
}

func TestDiscoverGradle(t *testing.T) {
	disc, err := Discover(fixtureRoot(t, "command-gradle-fixture"))
	if err != nil {
		t.Fatalf("Discover 出错: %v", err)
	}
	cmds := byName(disc.Commands)

	if build, ok := cmds["build"]; !ok || build.Category != CategoryBuild || build.Source != SourceGradle {
		t.Fatalf("缺少正确的 Gradle build 命令: %+v", build)
	}
	// fixture 无 src/test，红线：不应臆造 test。
	if _, ok := cmds["test"]; ok {
		t.Fatal("无 src/test 时不应给出 Gradle test 命令")
	}
	if boot, ok := cmds["bootRun"]; !ok || boot.Category != CategoryDev {
		t.Fatalf("应从 spring-boot 插件发现 bootRun: %+v", boot)
	}
	// 自定义任务必须来自脚本真实声明。
	for _, name := range []string{"printVersion", "syncConfig"} {
		c, ok := cmds[name]
		if !ok || c.Category != CategoryRun || c.Source != SourceGradle {
			t.Fatalf("缺少声明的 Gradle 自定义任务 %q: %+v", name, c)
		}
	}
}
