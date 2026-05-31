package symbols

import "testing"

func hasSymbol(syms []Symbol, name string, kind Kind) bool {
	for _, s := range syms {
		if s.Name == name && s.Kind == kind {
			return true
		}
	}
	return false
}

func TestExtractVueComposable(t *testing.T) {
	content := "import { ref } from 'vue'\n\nexport function useAuth() {\n  const loggedIn = ref(false)\n  return { loggedIn }\n}\n"
	syms := Extract(content, "src/composables/useAuth.ts")
	t.Logf("syms=%+v", syms)
	if !hasSymbol(syms, "useAuth", KindComposable) {
		t.Fatalf("期望提取 useAuth composable，得到 %+v", syms)
	}
}

func TestExtractVueStore(t *testing.T) {
	content := "import { defineStore } from 'pinia'\nexport const useUserStore = defineStore('user', {})\n"
	syms := Extract(content, "src/stores/user.ts")
	if !hasSymbol(syms, "user", KindStore) {
		t.Fatalf("期望提取 user store，得到 %+v", syms)
	}
}

func TestExtractSpringController(t *testing.T) {
	content := "@RestController\n@RequestMapping(\"/orders\")\npublic class OrderController {\n  @GetMapping(\"/list\")\n  public Object list() { return null; }\n}\n"
	syms := Extract(content, "src/main/java/com/example/controller/OrderController.java")
	if !hasSymbol(syms, "OrderController", KindController) {
		t.Fatalf("期望提取 OrderController，得到 %+v", syms)
	}
	if !hasSymbol(syms, "/list", KindEndpoint) {
		t.Fatalf("期望提取 endpoint /list，得到 %+v", syms)
	}
}
