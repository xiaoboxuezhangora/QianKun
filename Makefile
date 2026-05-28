.PHONY: smoke test build idea-plugin

BINARY := bin/qiankun-mcpd

smoke: test build
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) --health > "$$tmp/health.json"; \
	grep -q '"status":"ready"' "$$tmp/health.json"; \
	grep -q '"toolcache":' "$$tmp/health.json"

test:
	go test ./...

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/qiankun-mcpd

idea-plugin:
	@echo "IDEA plugin not implemented yet"
