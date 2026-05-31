package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestServerProtocol 覆盖最小协议面：initialize / tools/list / tools/call、
// 通知不回响应、未知 method 返回标准 error、工具 panic 转 isError、
// 以及 stdout 帧必须是逐行合法 JSON（无嵌入换行）。
func TestServerProtocol(t *testing.T) {
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"missing"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`,
	}
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")

	var out bytes.Buffer
	s := NewServer("test", "0.0.0", in, &out, nil)
	s.Register(Tool{
		Name:        "echo",
		Description: "echo",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     func(a json.RawMessage) ToolResult { return TextResult(string(a)) },
	})
	s.Register(Tool{
		Name:    "boom",
		Handler: func(json.RawMessage) ToolResult { panic("kaboom") },
	})

	if err := s.Serve(); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	// 6 个带 id 的请求应得 6 条响应；通知不产生输出。
	lines := bytes.Split(bytes.TrimRight(out.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 6 {
		t.Fatalf("expected 6 response frames, got %d: %s", len(lines), out.String())
	}

	byID := map[float64]map[string]any{}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("stdout frame is not valid single-line JSON: %q (%v)", line, err)
		}
		id, ok := m["id"].(float64)
		if !ok {
			t.Fatalf("response missing numeric id: %s", line)
		}
		byID[id] = m
	}

	// initialize
	initResult := mustResult(t, byID[1])
	if initResult["protocolVersion"] != "2024-11-05" {
		t.Errorf("initialize protocolVersion = %v", initResult["protocolVersion"])
	}
	if info, _ := initResult["serverInfo"].(map[string]any); info["name"] != "test" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}

	// tools/list
	listResult := mustResult(t, byID[2])
	tools, _ := listResult["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools/list returned %d tools, want 2", len(tools))
	}

	// tools/call echo → 正常结果，回显 arguments
	echoResult := mustResult(t, byID[3])
	if isErr, _ := echoResult["isError"].(bool); isErr {
		t.Errorf("echo unexpectedly isError")
	}
	if text := firstText(t, echoResult); text != `{"msg":"hi"}` {
		t.Errorf("echo text = %q", text)
	}

	// tools/call boom → panic 被 recover，转 isError
	boomResult := mustResult(t, byID[4])
	if isErr, _ := boomResult["isError"].(bool); !isErr {
		t.Errorf("boom should be isError, got %v", boomResult)
	}

	// tools/call missing → 未知工具，isError
	missingResult := mustResult(t, byID[5])
	if isErr, _ := missingResult["isError"].(bool); !isErr {
		t.Errorf("missing tool should be isError, got %v", missingResult)
	}

	// 未知 method → 标准 JSON-RPC error
	unknown := byID[6]
	if unknown["result"] != nil {
		t.Errorf("unknown method should not carry result: %v", unknown)
	}
	rpcErr, _ := unknown["error"].(map[string]any)
	if rpcErr == nil || rpcErr["code"].(float64) != codeMethodNotFnd {
		t.Errorf("unknown method error = %v", unknown["error"])
	}
}

func mustResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response missing result object: %v", resp)
	}
	return result
}

func firstText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("result has no content: %v", result)
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	return text
}
