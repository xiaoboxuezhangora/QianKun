package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMCPEndToEnd 以真实子进程方式端到端验收 mcp 子命令：构建二进制、启动
// `qiankun-mcpd mcp`，经 stdin/stdout 走 JSON-RPC，断言 initialize/tools/list/
// tools/call 行为。同时校验 stdout 只含合法 JSON-RPC 帧（无日志混入）。
func TestMCPEndToEnd(t *testing.T) {
	bin := buildBinary(t)
	root, err := filepath.Abs("../../testdata/memory-scan-fixture")
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}

	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(), "QIANKUN_HOME="+t.TempDir())
	cmd.Stderr = os.Stderr // 诊断走 stderr，不污染被检验的 stdout 帧通道

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp subprocess: %v", err)
	}
	defer func() {
		stdin.Close()
		_ = cmd.Wait()
	}()

	// 后台逐行读取 stdout，每行必须是合法单行 JSON-RPC 帧。
	lines := make(chan []byte, 16)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()

	send := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	recv := func() map[string]any {
		t.Helper()
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stdout closed before response")
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err != nil {
				t.Fatalf("stdout frame not valid JSON-RPC: %q (%v)", line, err)
			}
			return m
		case <-time.After(20 * time.Second):
			t.Fatal("timeout waiting for response")
			return nil
		}
	}

	// initialize
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2024-11-05"}})
	initResp := recv()
	initResult, _ := initResp["result"].(map[string]any)
	if initResult == nil {
		t.Fatalf("initialize missing result: %v", initResp)
	}
	if info, _ := initResult["serverInfo"].(map[string]any); info["name"] != "qiankun-mcpd" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}

	// notifications/initialized：通知，无响应
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	// tools/list：应恰好暴露两个工具
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	listResult, _ := recv()["result"].(map[string]any)
	tools, _ := listResult["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		names[m["name"].(string)] = true
		if desc, _ := m["description"].(string); desc == "" {
			t.Errorf("tool %v missing description", m["name"])
		}
		if m["inputSchema"] == nil {
			t.Errorf("tool %v missing inputSchema", m["name"])
		}
	}
	if len(tools) != 2 || !names["memory-query"] || !names["usage-report"] {
		t.Fatalf("tools/list = %v, want exactly memory-query & usage-report", names)
	}

	// tools/call memory-query：top_k 越界 50 应被 clamp 到 20，不报错
	send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "memory-query", "arguments": map[string]any{"root": root, "query": "Vue router component", "top_k": 50}}})
	queryResult := decodeToolJSON(t, recv(), false)
	if queryResult["top_k"].(float64) != 20 {
		t.Errorf("top_k not clamped to 20: %v", queryResult["top_k"])
	}
	if _, ok := queryResult["results"].([]any); !ok {
		t.Errorf("memory-query missing results array: %v", queryResult)
	}

	// tools/call memory-query：缺 query → isError 降级，进程不崩
	send(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "memory-query", "arguments": map[string]any{"root": root}}})
	if isErr, _ := recv()["result"].(map[string]any)["isError"].(bool); !isErr {
		t.Error("memory-query without query should return isError")
	}

	// tools/call usage-report：无入参，返回聚合报告
	send(map[string]any{"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": "usage-report"}})
	usageResult := decodeToolJSON(t, recv(), false)
	if _, ok := usageResult["total_calls"]; !ok {
		t.Errorf("usage-report missing total_calls: %v", usageResult)
	}

	// 未知 method → 标准 JSON-RPC error，进程存活
	send(map[string]any{"jsonrpc": "2.0", "id": 6, "method": "foo/bar"})
	if recv()["error"] == nil {
		t.Error("unknown method should return JSON-RPC error")
	}

	// 未知工具 → isError
	send(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": "nope"}})
	if isErr, _ := recv()["result"].(map[string]any)["isError"].(bool); !isErr {
		t.Error("unknown tool should return isError")
	}
}

// decodeToolJSON 取出 tools/call 结果的首个 text 内容并解析为 JSON 对象，
// 同时断言 isError 与期望一致。
func decodeToolJSON(t *testing.T, resp map[string]any, wantErr bool) map[string]any {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call missing result: %v", resp)
	}
	if isErr, _ := result["isError"].(bool); isErr != wantErr {
		t.Fatalf("isError = %v, want %v: %v", isErr, wantErr, result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call result has no content: %v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("tool text not JSON: %q (%v)", text, err)
	}
	return payload
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "qiankun-mcpd")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	build.Stdout = io.Discard
	if err := build.Run(); err != nil {
		t.Fatalf("build binary: %v", err)
	}
	return bin
}
