.PHONY: smoke test build idea-plugin

BINARY := bin/qiankun-mcpd

smoke: test build
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) --health > "$$tmp/health.json"; \
	grep -q '"status":"ready"' "$$tmp/health.json"; \
	grep -q '"toolcache":' "$$tmp/health.json"; \
	grep -q '"usage":' "$$tmp/health.json"; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) memory-scan --root testdata/memory-scan-fixture --format json > "$$tmp/memory-scan.json"; \
	grep -q '"files_indexed":' "$$tmp/memory-scan.json"; \
	grep -q '"skipped_summary":' "$$tmp/memory-scan.json"; \
	grep -q '"path": "src/main.ts"' "$$tmp/memory-scan.json"; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) memory-query --root testdata/memory-scan-fixture --query "Vue router component" --top-k 5 > "$$tmp/memory-query.json"; \
	grep -q '"results":' "$$tmp/memory-query.json"; \
	grep -q '"score":' "$$tmp/memory-query.json"; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) usage-report > "$$tmp/usage-report.json"; \
	grep -q '"total_calls":' "$$tmp/usage-report.json"; \
	grep -q '"estimated_tokens":' "$$tmp/usage-report.json"; \
	QIANKUN_HOME="$$tmp" ./$(BINARY) weekly-report --format markdown --instructions-root testdata/memory-scan-fixture > "$$tmp/weekly-report.md"; \
	grep -q '## Memory Index' "$$tmp/weekly-report.md"; \
	grep -q '## UsageMeter' "$$tmp/weekly-report.md"; \
	grep -q '## Instructions Lint' "$$tmp/weekly-report.md"

test:
	go test ./...

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/qiankun-mcpd

idea-plugin:
	@echo "IDEA plugin placeholder only; W3 keeps Memory/Usage logic in qiankun-mcpd sidecar"
