package scan

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestProfileAndRoleAssignment 在四套框架 fixtures 上验证 profile 检测与 role 分配：
// profile 应按框架正确识别，关键文件应拿到与其职责一致的 role（替代旧的死桩 "unknown"）。
func TestProfileAndRoleAssignment(t *testing.T) {
	cases := map[string]struct {
		profile string
		roles   map[string]string
	}{
		"vue": {
			profile: ProfileVue,
			roles: map[string]string{
				"src/main.ts":                 RoleAppEntry,
				"src/router/index.ts":         RoleRouteDefinition,
				"src/stores/user.ts":          RoleStore,
				"src/services/orderApi.ts":    RoleService,
				"src/pages/OrderListPage.vue": RolePageView,
				"src/components/UserCard.vue": RoleComponent,
				"vite.config.ts":              RoleConfiguration,
				"components.d.ts":             RoleGeneratedNoisy,
			},
		},
		"angular": {
			profile: ProfileAngular,
			roles: map[string]string{
				"src/app/app-routing.module.ts":  RoleRouteDefinition,
				"src/app/user/user.component.ts": RoleComponent,
				"src/app/user/user.service.ts":   RoleService,
				"angular.json":                   RoleConfiguration,
			},
		},
		"react": {
			profile: ProfileReact,
			roles: map[string]string{
				"src/main.tsx":              RoleAppEntry,
				"src/router.tsx":            RoleRouteDefinition,
				"src/store/counterSlice.ts": RoleStore,
				"src/services/userApi.ts":   RoleService,
				"src/pages/Dashboard.tsx":   RolePageView,
				"src/components/Button.tsx": RoleComponent,
			},
		},
		"spring": {
			profile: ProfileSpring,
			roles: map[string]string{
				"src/main/java/com/example/Application.java":                RoleAppEntry,
				"src/main/java/com/example/controller/OrderController.java": RoleController,
				"src/main/java/com/example/service/OrderService.java":       RoleService,
				"src/main/java/com/example/repository/OrderRepository.java": RoleRepository,
				"pom.xml": RoleConfiguration,
			},
		},
	}

	for framework, want := range cases {
		framework, want := framework, want
		t.Run(framework, func(t *testing.T) {
			result, err := Scan(Options{Root: fixtureForFramework(t, framework)})
			if err != nil {
				t.Fatalf("Scan(%s) error: %v", framework, err)
			}
			byPath := make(map[string]FileEntry, len(result.Files))
			for _, f := range result.Files {
				byPath[f.Path] = f
			}
			for path, role := range want.roles {
				entry, ok := byPath[path]
				if !ok {
					t.Fatalf("%s: entry %q not found", framework, path)
				}
				if entry.Profile != want.profile {
					t.Errorf("%s: %q profile = %q, want %q", framework, path, entry.Profile, want.profile)
				}
				if entry.Role != role {
					t.Errorf("%s: %q role = %q, want %q", framework, path, entry.Role, role)
				}
			}
		})
	}
}

func fixtureForFramework(t *testing.T, framework string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "retrieval-fixtures", framework))
}
