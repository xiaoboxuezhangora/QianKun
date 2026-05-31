// Package symbols 实现 Symbol Index v0 的轻量符号提取（正则 / AST-lite，先规则后模型，
// 不引入 LSP）。它从源文件头部读取有限字节，按扩展名与框架特征识别组件、路由、store、
// composable/hook、service/API 方法、Controller、Service、Repository、Mapper、
// RequestMapping endpoint 等符号，供索引层写入 symbol 表并折入召回。
//
// 设计要点：
//   - 仅读取文件头部（小文件读全量），因为绝大多数符号声明位于文件头部；
//     这与索引层 content_sample 的“尾部 8KB”语义互不影响，互为补充。
//   - 提取是纯函数，不触碰数据库；幂等由调用方（按文件重建）保证。
package symbols

import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
)

// HeadReadLimit 是符号提取读取的文件头部上限（小文件读全量）。
const HeadReadLimit = 16 * 1024

// Kind 是符号语义类别，与检索意图、role 信号对齐。
type Kind string

const (
	KindComponent  Kind = "component"
	KindRoute      Kind = "route"
	KindStore      Kind = "store"
	KindComposable Kind = "composable"
	KindHook       Kind = "hook"
	KindAPIMethod  Kind = "api-method"
	KindService    Kind = "service"
	KindController Kind = "controller"
	KindEndpoint   Kind = "endpoint"
	KindRepository Kind = "repository"
	KindEntity     Kind = "entity"
	KindModule     Kind = "module"
)

// kindWeight 是各符号类别的基础权重，体现“文件职责”强弱：
// 入口型（route/endpoint/controller）权重最高，定义型（entity）较低。
var kindWeight = map[Kind]int{
	KindEndpoint:   85,
	KindRoute:      80,
	KindController: 75,
	KindStore:      70,
	KindAPIMethod:  65,
	KindService:    65,
	KindRepository: 65,
	KindComponent:  60,
	KindModule:     55,
	KindComposable: 50,
	KindHook:       50,
	KindEntity:     50,
}

// WeightFor 返回符号类别的基础权重，未知类别回退为 40。
func WeightFor(k Kind) int {
	if w, ok := kindWeight[k]; ok {
		return w
	}
	return 40
}

// Symbol 是一条被提取的符号。
type Symbol struct {
	Name   string
	Kind   Kind
	Line   int
	Weight int
}

// 预编译正则：集中定义，便于审计与维护。
var (
	reDefineStore   = regexp.MustCompile(`defineStore\(\s*['"]([A-Za-z0-9_-]+)['"]`)
	reCreateRouter  = regexp.MustCompile(`createRouter\s*\(`)
	reRoutePath     = regexp.MustCompile(`path:\s*['"]([^'"]+)['"]`)
	reUseComposable = regexp.MustCompile(`(?m)export\s+(?:const|function)\s+(use[A-Z][A-Za-z0-9_]*)`)
	reExportFunc    = regexp.MustCompile(`(?m)export\s+(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reExportConstFn = regexp.MustCompile(`(?m)export\s+const\s+([A-Za-z_][A-Za-z0-9_]*)\s*[:=]`)
	reReactComp     = regexp.MustCompile(`(?m)(?:export\s+default\s+function|export\s+function|function)\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	reReactCompVar  = regexp.MustCompile(`(?m)(?:export\s+)?const\s+([A-Z][A-Za-z0-9_]*)\s*[:=]`)
	reCreateSlice   = regexp.MustCompile(`createSlice\(\s*\{[^}]*name:\s*['"]([A-Za-z0-9_-]+)['"]`)
	reReactRoute    = regexp.MustCompile(`<Route\s+[^>]*path=['"]([^'"]+)['"]`)
	reBrowserRouter = regexp.MustCompile(`createBrowserRouter\s*\(`)
	reNgComponent   = regexp.MustCompile(`@Component\(`)
	reNgInjectable  = regexp.MustCompile(`@Injectable\(`)
	reNgModule      = regexp.MustCompile(`@NgModule\(`)
	reNgClass       = regexp.MustCompile(`(?m)export\s+class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reRouterModule  = regexp.MustCompile(`RouterModule\.for(?:Root|Child)`)

	reJavaController = regexp.MustCompile(`@(?:RestController|Controller)\b`)
	reJavaService    = regexp.MustCompile(`@Service\b`)
	reJavaRepository = regexp.MustCompile(`@(?:Repository|Mapper)\b`)
	reJavaEntity     = regexp.MustCompile(`@Entity\b`)
	reJavaClass      = regexp.MustCompile(`(?m)(?:public\s+)?(?:class|interface)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reJavaMapping    = regexp.MustCompile(`@(?:Get|Post|Put|Delete|Patch|Request)Mapping\(\s*(?:value\s*=\s*)?['"]([^'"]*)['"]`)
	reJpaRepository  = regexp.MustCompile(`(?m)interface\s+([A-Za-z_][A-Za-z0-9_]*)\s+extends\s+[A-Za-z]*Repository`)
)

// ExtractFromFile 读取文件头部并提取符号；rel 为相对仓库根的斜杠路径，用于路径相关判定。
// 读取失败返回 nil（不阻断索引）。
func ExtractFromFile(absPath, rel string) []Symbol {
	data, err := readHead(absPath)
	if err != nil {
		return nil
	}
	return Extract(string(data), rel)
}

// Extract 从内容文本与相对路径提取符号，便于直接进行单元测试。
func Extract(content, rel string) []Symbol {
	rel = strings.ToLower(strings.ReplaceAll(rel, "\\", "/"))
	ext := lowerExt(rel)

	var out []Symbol
	switch ext {
	case ".vue":
		out = append(out, extractVueSFC(content, rel)...)
	case ".ts", ".js", ".mts", ".cts", ".mjs", ".cjs":
		out = append(out, extractScript(content, rel, false)...)
	case ".tsx", ".jsx":
		out = append(out, extractScript(content, rel, true)...)
	case ".java", ".kt":
		out = append(out, extractJava(content)...)
	}
	return dedupe(out)
}

// extractVueSFC：.vue 单文件组件本身即为一个组件，文件名（去扩展名）为组件名；
// 若内部声明 store/路由也一并提取。
func extractVueSFC(content, rel string) []Symbol {
	out := []Symbol{{Name: baseName(rel), Kind: KindComponent, Line: 1, Weight: WeightFor(KindComponent)}}
	out = append(out, extractScript(content, rel, false)...)
	return out
}

// extractScript 处理 JS/TS/TSX/JSX。isJSX 时额外识别 React 组件与路由。
func extractScript(content, rel string, isJSX bool) []Symbol {
	var out []Symbol

	for _, m := range reDefineStore.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, sym(content, m, reDefineStore, KindStore))
	}
	for _, m := range reCreateSlice.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, sym(content, m, reCreateSlice, KindStore))
	}
	for _, m := range reUseComposable.FindAllStringSubmatchIndex(content, -1) {
		kind := KindComposable
		if isJSX {
			kind = KindHook
		}
		out = append(out, sym(content, m, reUseComposable, kind))
	}

	// 路由：createRouter / createBrowserRouter / RouterModule，或路径位于 router 目录。
	isRouterFile := strings.Contains(rel, "/router") || strings.Contains(rel, "/routes") ||
		strings.Contains(rel, "-routing.module") || baseName(rel) == "router"
	if reCreateRouter.MatchString(content) || reBrowserRouter.MatchString(content) ||
		reRouterModule.MatchString(content) || isRouterFile {
		for _, m := range reRoutePath.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reRoutePath, KindRoute))
		}
		for _, m := range reReactRoute.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reReactRoute, KindRoute))
		}
		// 即便没有显式 path，也把路由文件本身登记为一个 route 符号。
		out = append(out, Symbol{Name: baseName(rel), Kind: KindRoute, Line: 1, Weight: WeightFor(KindRoute)})
	}

	// service / API 方法：仅在 services/api 目录下，把导出函数登记为 api-method。
	if strings.Contains(rel, "/services") || strings.Contains(rel, "/service") ||
		strings.Contains(rel, "/api") {
		for _, m := range reExportFunc.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reExportFunc, KindAPIMethod))
		}
		for _, m := range reExportConstFn.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reExportConstFn, KindAPIMethod))
		}
	}

	// Angular 装饰器（出现在 .ts 中）。
	if reNgComponent.MatchString(content) {
		out = append(out, classSymbols(content, KindComponent)...)
	}
	if reNgInjectable.MatchString(content) {
		out = append(out, classSymbols(content, KindService)...)
	}
	if reNgModule.MatchString(content) {
		out = append(out, classSymbols(content, KindModule)...)
	}

	// React 组件（仅 JSX 文件，避免把普通 PascalCase 函数误判为组件）。
	if isJSX {
		for _, m := range reReactComp.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reReactComp, KindComponent))
		}
		for _, m := range reReactCompVar.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reReactCompVar, KindComponent))
		}
	}

	return out
}

// classSymbols 提取 export class / class 名作为指定 kind 的符号（用于 Angular 装饰器场景）。
func classSymbols(content string, kind Kind) []Symbol {
	var out []Symbol
	for _, m := range reNgClass.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, sym(content, m, reNgClass, kind))
	}
	return out
}

// extractJava 处理 Java/Kotlin：依据类级注解判定 controller/service/repository/entity，
// 并为每个 @*Mapping 提取 endpoint 路径符号。
func extractJava(content string) []Symbol {
	var out []Symbol

	classKind := Kind("")
	switch {
	case reJavaController.MatchString(content):
		classKind = KindController
	case reJavaService.MatchString(content):
		classKind = KindService
	case reJavaRepository.MatchString(content):
		classKind = KindRepository
	case reJavaEntity.MatchString(content):
		classKind = KindEntity
	}
	if classKind != "" {
		for _, m := range reJavaClass.FindAllStringSubmatchIndex(content, -1) {
			out = append(out, sym(content, m, reJavaClass, classKind))
		}
	}
	// 未显式注解但符合 JpaRepository 继承的接口也算 repository。
	for _, m := range reJpaRepository.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, sym(content, m, reJpaRepository, KindRepository))
	}
	// endpoint 路径符号。
	for _, m := range reJavaMapping.FindAllStringSubmatchIndex(content, -1) {
		s := sym(content, m, reJavaMapping, KindEndpoint)
		if s.Name == "" {
			s.Name = "/"
		}
		out = append(out, s)
	}
	return out
}

// sym 根据正则的第一个捕获组构造符号，并计算行号。
func sym(content string, match []int, _ *regexp.Regexp, kind Kind) Symbol {
	start, end := match[2], match[3]
	name := ""
	if start >= 0 && end >= 0 {
		name = content[start:end]
	}
	line := 1 + strings.Count(content[:match[0]], "\n")
	return Symbol{Name: name, Kind: kind, Line: line, Weight: WeightFor(kind)}
}

// dedupe 按 (Name, Kind) 去重，保留首次出现（行号最小）。
func dedupe(in []Symbol) []Symbol {
	seen := make(map[string]bool, len(in))
	out := make([]Symbol, 0, len(in))
	for _, s := range in {
		if s.Name == "" {
			continue
		}
		key := string(s.Kind) + "\x00" + s.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, HeadReadLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	data := buf[:n]
	// 二进制文件（含 NUL）不提取符号。
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, os.ErrInvalid
	}
	return data, nil
}

func lowerExt(rel string) string {
	if i := strings.LastIndexByte(rel, '.'); i >= 0 {
		return rel[i:]
	}
	return ""
}

func baseName(rel string) string {
	rel = strings.TrimRight(rel, "/")
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		rel = rel[i+1:]
	}
	if i := strings.LastIndexByte(rel, '.'); i > 0 {
		rel = rel[:i]
	}
	return rel
}
