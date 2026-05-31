package commands

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

// pomXML 仅声明命令发现关心的字段：构建插件用于识别框架（如 Spring Boot）。
type pomXML struct {
	Build struct {
		Plugins struct {
			Plugin []struct {
				GroupID    string `xml:"groupId"`
				ArtifactID string `xml:"artifactId"`
			} `xml:"plugin"`
		} `xml:"plugins"`
	} `xml:"build"`
}

// discoverMaven 解析 pom.xml。命令均以 pom 真实存在为前提：
//   - package：Maven 标准生命周期阶段，pom 存在即真实可用。
//   - test：仅当真实存在测试源码目录 src/test 时给出（红线：无测试不臆造 test）。
//   - spring-boot:run：仅当 pom 真实声明 spring-boot-maven-plugin 时给出。
func discoverMaven(root string) []Command {
	pomPath := filepath.Join(root, "pom.xml")
	data, err := os.ReadFile(pomPath)
	if err != nil {
		return nil
	}
	var pom pomXML
	// 解析失败不阻断、不伪造：直接跳过 Maven 来源。
	_ = xml.Unmarshal(data, &pom)

	mvn := mavenBinary(root)
	var cmds []Command

	cmds = append(cmds, Command{
		Name:       "package",
		Category:   CategoryBuild,
		Source:     SourcePomXML,
		Invocation: mvn + " -DskipTests package",
		Raw:        "mvn package",
	})

	if dirExists(filepath.Join(root, "src", "test")) {
		cmds = append(cmds, Command{
			Name:       "test",
			Category:   CategoryTest,
			Source:     SourcePomXML,
			Invocation: mvn + " test",
			Raw:        "mvn test",
		})
	}

	if hasSpringBootMavenPlugin(pom) {
		cmds = append(cmds, Command{
			Name:       "spring-boot:run",
			Category:   CategoryDev,
			Source:     SourcePomXML,
			Invocation: mvn + " spring-boot:run",
			Raw:        "spring-boot-maven-plugin",
		})
	}
	return cmds
}

func hasSpringBootMavenPlugin(pom pomXML) bool {
	for _, p := range pom.Build.Plugins.Plugin {
		if p.ArtifactID == "spring-boot-maven-plugin" {
			return true
		}
	}
	return false
}

// mavenBinary 优先使用项目自带的 Maven Wrapper（mvnw），否则回退到 mvn。
func mavenBinary(root string) string {
	if fileExists(filepath.Join(root, "mvnw")) {
		return "./mvnw"
	}
	return "mvn"
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
