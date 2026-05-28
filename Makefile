.PHONY: smoke test build idea-plugin

BINARY := bin/qiankun-mcpd

smoke: test build
	./$(BINARY) --health | grep -q '{"status":"ready"}'

test:
	go test ./...

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/qiankun-mcpd

idea-plugin:
	@echo "IDEA plugin not implemented in W0"
