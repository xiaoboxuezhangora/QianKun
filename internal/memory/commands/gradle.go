package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 匹配真实声明的自定义 Gradle 任务：
//   task fooBar { ... }            (Groovy DSL)
//   tasks.register('fooBar') { }   (Groovy DSL)
//   tasks.register("fooBar") { }   (Kotlin DSL)
var (
	reGradleTaskKeyword  = regexp.MustCompile(`(?m)^\s*task\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reGradleTaskRegister = regexp.MustCompile(`tasks\.register(?:<[^>]*>)?\(\s*["']([A-Za-z_][A-Za-z0-9_]*)["']`)
)

// discoverGradle 解析 build.gradle / build.gradle.kts。命令均以构建脚本真实内容为前提：
//   - build：Gradle 标准任务，构建脚本存在即真实可用。
//   - test：仅当真实存在 src/test 目录时给出（红线：无测试不臆造 test）。
//   - bootRun：仅当脚本真实应用 org.springframework.boot 插件时给出。
//   - 自定义任务：仅来自脚本中真实声明的 task / tasks.register。
func discoverGradle(root string) []Command {
	scriptPath, ok := gradleScript(root)
	if !ok {
		return nil
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil
	}
	script := string(data)
	gradle := gradleBinary(root)

	var cmds []Command
	cmds = append(cmds, Command{
		Name:       "build",
		Category:   CategoryBuild,
		Source:     SourceGradle,
		Invocation: gradle + " build",
		Raw:        "gradle build",
	})

	if dirExists(filepath.Join(root, "src", "test")) {
		cmds = append(cmds, Command{
			Name:       "test",
			Category:   CategoryTest,
			Source:     SourceGradle,
			Invocation: gradle + " test",
			Raw:        "gradle test",
		})
	}

	if strings.Contains(script, "org.springframework.boot") || strings.Contains(script, "bootRun") {
		cmds = append(cmds, Command{
			Name:       "bootRun",
			Category:   CategoryDev,
			Source:     SourceGradle,
			Invocation: gradle + " bootRun",
			Raw:        "spring-boot gradle plugin",
		})
	}

	for _, name := range customGradleTasks(script) {
		cmds = append(cmds, Command{
			Name:       name,
			Category:   CategoryRun,
			Source:     SourceGradle,
			Invocation: gradle + " " + name,
			Raw:        "declared gradle task",
		})
	}
	return cmds
}

// customGradleTasks 从脚本文本中提取真实声明的自定义任务名，去重并排序。
func customGradleTasks(script string) []string {
	seen := map[string]bool{}
	for _, m := range reGradleTaskKeyword.FindAllStringSubmatch(script, -1) {
		seen[m[1]] = true
	}
	for _, m := range reGradleTaskRegister.FindAllStringSubmatch(script, -1) {
		seen[m[1]] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func gradleScript(root string) (string, bool) {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		p := filepath.Join(root, name)
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

// gradleBinary 优先使用项目自带的 Gradle Wrapper（gradlew），否则回退到 gradle。
func gradleBinary(root string) string {
	if fileExists(filepath.Join(root, "gradlew")) {
		return "./gradlew"
	}
	return "gradle"
}
