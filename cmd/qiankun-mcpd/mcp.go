package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/xiaoboxuezhangora/QianKun/internal/mcp"
)

// MCP 工具的 top_k 约束：默认 8，硬上限 20。越界一律 clamp（不报错），
// 防止单次返回体积膨胀成新的 token 黑洞。
const (
	mcpDefaultTopK = 8
	mcpMaxTopK     = 20
)

// runMCP 以 stdio 常驻方式启动 MCP server，只暴露 memory-query 与 usage-report。
// 关键约束：stdout 是 JSON-RPC 帧通道，所有诊断只能写 stderr。
func runMCP(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 0 {
		printUsage(stderr)
		return 2
	}

	logf := func(format string, a ...any) {
		fmt.Fprintf(stderr, "[qiankun-mcp] "+format+"\n", a...)
	}
	server := mcp.NewServer("qiankun-mcpd", version, os.Stdin, stdout, logf)
	server.Register(memoryQueryTool(stderr))
	server.Register(usageReportTool())

	if err := server.Serve(); err != nil {
		fmt.Fprintf(stderr, "mcp server stopped: %v\n", err)
		return 1
	}
	return 0
}

// memoryQueryTool 暴露 Memory Index 检索。描述保持精简，避免工具描述本身成为 token 负担。
func memoryQueryTool(stderr io.Writer) mcp.Tool {
	schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "root": {"type": "string", "description": "仓库根路径"},
    "query": {"type": "string", "description": "查询文本"},
    "top_k": {"type": "integer", "description": "返回条数，默认 8，上限 20", "default": 8, "minimum": 1, "maximum": 20}
  },
  "required": ["root", "query"]
}`)
	return mcp.Tool{
		Name:        "memory-query",
		Description: "在本地仓库 Memory Index 上做关键词/混合检索，返回 top-k 高相关文件与片段。本地优先，不上传源码。",
		InputSchema: schema,
		Handler: func(arguments json.RawMessage) mcp.ToolResult {
			var in struct {
				Root  string `json:"root"`
				Query string `json:"query"`
				TopK  *int   `json:"top_k"`
			}
			if len(arguments) > 0 {
				if err := json.Unmarshal(arguments, &in); err != nil {
					return mcp.ErrorResult("无效参数: %v", err)
				}
			}
			if in.Root == "" || in.Query == "" {
				return mcp.ErrorResult("root 与 query 为必填参数")
			}
			response, err := queryMemory(in.Root, in.Query, clampTopK(in.TopK), stderr)
			if err != nil {
				return mcp.ErrorResult("memory-query 失败: %v", err)
			}
			return mcp.JSONResult(response)
		},
	}
}

// usageReportTool 暴露 UsageMeter 聚合报告。无入参，返回定长小结构，体积可控。
func usageReportTool() mcp.Tool {
	schema := json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`)
	return mcp.Tool{
		Name:        "usage-report",
		Description: "返回 UsageMeter 聚合报告：调用数、缓存命中、token 估算与节省、P95 延迟。",
		InputSchema: schema,
		Handler: func(json.RawMessage) mcp.ToolResult {
			report, err := usageReport()
			if err != nil {
				return mcp.ErrorResult("usage-report 失败: %v", err)
			}
			return mcp.JSONResult(report)
		},
	}
}

// clampTopK 将客户端传入的 top_k 规整到 [1, 20]：未提供则用默认 8，越界 clamp 不报错。
func clampTopK(v *int) int {
	if v == nil {
		return mcpDefaultTopK
	}
	switch {
	case *v < 1:
		return 1
	case *v > mcpMaxTopK:
		return mcpMaxTopK
	default:
		return *v
	}
}
