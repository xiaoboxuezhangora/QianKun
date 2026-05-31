package index

import (
	"path/filepath"
	"testing"

	"github.com/xiaoboxuezhangora/QianKun/internal/memory/scan"
)

// retrievalCase 是一条检索验收用例：查询文本 + 期望命中的路径集合。
// 只要 Top-K 结果中出现 expect 中任一路径即记为命中。
type retrievalCase struct {
	query  string
	expect []string
}

// frameworkCases 为四套框架 fixtures 各配 query→期望命中集合，作为 ≥80% 命中率闸门。
var frameworkCases = map[string][]retrievalCase{
	"vue": {
		{"Vue 路由 router 配置 routes", []string{"src/router/index.ts"}},
		{"用户 store 状态管理 pinia", []string{"src/stores/user.ts"}},
		{"订单列表 页面 page view", []string{"src/pages/OrderListPage.vue"}},
		{"UserCard 组件 component", []string{"src/components/UserCard.vue"}},
		{"订单 service api 接口 fetchOrders", []string{"src/services/orderApi.ts"}},
		{"useAuth composable 登录 hook", []string{"src/composables/useAuth.ts"}},
	},
	"angular": {
		{"Angular 路由 routing module", []string{"src/app/app-routing.module.ts"}},
		{"user component 组件 视图", []string{"src/app/user/user.component.ts"}},
		{"user service 服务 注入 injectable", []string{"src/app/user/user.service.ts"}},
		{"order 组件 component", []string{"src/app/order/order.component.ts"}},
		{"ng module 模块 app", []string{"src/app/app.module.ts"}},
	},
	"react": {
		{"react 路由 route router", []string{"src/router.tsx"}},
		{"Dashboard 页面 component", []string{"src/pages/Dashboard.tsx"}},
		{"Button 组件 component", []string{"src/components/Button.tsx"}},
		{"counter store redux slice 状态", []string{"src/store/counterSlice.ts"}},
		{"useUser hook 钩子", []string{"src/hooks/useUser.ts"}},
		{"user service api getUser 接口", []string{"src/services/userApi.ts"}},
	},
	"spring": {
		{"OrderController controller endpoint orders mapping 接口", []string{"src/main/java/com/example/controller/OrderController.java"}},
		{"order service 服务 业务", []string{"src/main/java/com/example/service/OrderService.java"}},
		{"order repository 持久化 dao mapper 仓储", []string{"src/main/java/com/example/repository/OrderRepository.java"}},
		{"order entity 实体", []string{"src/main/java/com/example/entity/Order.java"}},
		{"spring boot application 启动 入口", []string{"src/main/java/com/example/Application.java"}},
	},
}

const hitRateGate = 0.80
const retrievalTopK = 5

// TestHybridHitRateFTS 在 FTS5 可用路径下验证四套框架 Top-5 命中率 ≥80%。
func TestHybridHitRateFTS(t *testing.T) {
	runHitRateGate(t, false)
}

// TestHybridHitRateKeywordFallback 在 FTS5 关闭（keyword 回退）路径下验证同一闸门，
// 证明 rerank 对召回来源透明、降级不阻断且不劣化命中。
func TestHybridHitRateKeywordFallback(t *testing.T) {
	runHitRateGate(t, true)
}

func runHitRateGate(t *testing.T, disableFTS bool) {
	t.Helper()
	var totalQueries, totalHits int
	for framework, cases := range frameworkCases {
		store := openTestStore(t)
		if disableFTS {
			// 强制走 keyword_index 回退路径。
			store.fts5Enabled = false
		}
		root := filepath.Join("..", "..", "..", "testdata", "retrieval-fixtures", framework)
		result, err := scan.Scan(scan.Options{Root: root})
		if err != nil {
			store.Close()
			t.Fatalf("[%s] Scan 失败: %v", framework, err)
		}
		if _, err := store.SyncScan(result); err != nil {
			store.Close()
			t.Fatalf("[%s] SyncScan 失败: %v", framework, err)
		}

		hits := 0
		for _, tc := range cases {
			resp, err := store.Query(result.Root, tc.query, retrievalTopK)
			if err != nil {
				store.Close()
				t.Fatalf("[%s] Query 失败: %v", framework, err)
			}
			if hit, rank := matchHit(resp.Results, tc.expect); hit {
				hits++
				t.Logf("[%s] 命中(rank=%d) query=%q", framework, rank, tc.query)
			} else {
				t.Logf("[%s] 未命中 query=%q top=%s", framework, tc.query, topPaths(resp.Results))
			}
		}
		rate := float64(hits) / float64(len(cases))
		t.Logf("[%s] 命中率 %.0f%% (%d/%d)", framework, rate*100, hits, len(cases))
		totalQueries += len(cases)
		totalHits += hits
		store.Close()
	}

	overall := float64(totalHits) / float64(totalQueries)
	mode := "FTS5"
	if disableFTS {
		mode = "keyword-fallback"
	}
	t.Logf("[%s] 总体 Top-%d 命中率 %.1f%% (%d/%d)", mode, retrievalTopK, overall*100, totalHits, totalQueries)
	if overall < hitRateGate {
		t.Fatalf("[%s] Top-%d 命中率 %.1f%% 低于闸门 %.0f%%（应停下分析瓶颈，禁止靠调系数硬凑）",
			mode, retrievalTopK, overall*100, hitRateGate*100)
	}
}

func matchHit(results []QueryResult, expect []string) (bool, int) {
	for i, r := range results {
		for _, e := range expect {
			if r.Path == e {
				return true, i + 1
			}
		}
	}
	return false, 0
}

func topPaths(results []QueryResult) string {
	var b []byte
	for i, r := range results {
		if i > 0 {
			b = append(b, ',', ' ')
		}
		b = append(b, r.Path...)
	}
	return string(b)
}
