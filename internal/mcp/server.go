// Package mcp 实现一个最小的 MCP（Model Context Protocol）stdio server。
//
// 设计约束（来自项目核心原则）：
//   - 最小协议面：只实现 initialize / tools/list / tools/call 三个方法，
//     外加 notifications/initialized 通知；未知 method 返回标准 JSON-RPC error，
//     既不 panic 也不退出进程。
//   - stdio 正确性：stdout 是 JSON-RPC 帧通道（换行分隔的单行 JSON），
//     绝不写入任何日志/诊断；日志一律走 logf（stderr 或 logs 文件）。
//   - 可降级：工具内部错误转为 MCP isError 结果返回，sidecar 不崩溃，
//     从而不波及 Copilot 本体。
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion 是缺省协议版本；initialize 时若客户端声明了版本则回显其版本。
const protocolVersion = "2024-11-05"

// Content 是工具返回的内容块。本实现只用 text 类型承载 JSON 文本。
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult 是 tools/call 的返回体。IsError 为 true 时表示工具内部失败，
// 但这仍是一次成功的 JSON-RPC 调用（错误信息在 Content 里），符合 MCP 约定。
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// TextResult 用一段文本构造正常结果。
func TextResult(text string) ToolResult {
	return ToolResult{Content: []Content{{Type: "text", Text: text}}}
}

// JSONResult 将任意值序列化为紧凑 JSON 文本作为结果；序列化失败转为 ErrorResult。
func JSONResult(v any) ToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return ErrorResult("结果序列化失败: %v", err)
	}
	return TextResult(string(data))
}

// ErrorResult 构造一个 isError 结果，承载人类可读的错误说明。
func ErrorResult(format string, args ...any) ToolResult {
	return ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

// Tool 描述一个被暴露的工具。InputSchema 为 JSON Schema 原文。
// Handler 不返回 error：任何失败都应转为 ErrorResult，保证进程不崩。
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(arguments json.RawMessage) ToolResult
}

// Server 是一个 stdio JSON-RPC server。它串行读取 in 的每一行消息并处理。
type Server struct {
	name    string
	version string
	in      io.Reader
	out     io.Writer
	logf    func(format string, args ...any)
	tools   []Tool
}

// NewServer 创建 server。logf 用于诊断输出（必须写 stderr/日志，绝不写 out）。
func NewServer(name, version string, in io.Reader, out io.Writer, logf func(format string, args ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{name: name, version: version, in: in, out: out, logf: logf}
}

// Register 注册一个工具。
func (s *Server) Register(tool Tool) {
	s.tools = append(s.tools, tool)
}

// rpcRequest 是入站 JSON-RPC 消息。ID 缺省（通知）时不回响应。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 标准错误码。
const (
	codeParseError    = -32700
	codeInvalidReq    = -32600
	codeMethodNotFnd  = -32601
	codeInvalidParams = -32602
)

// Serve 进入读取-处理循环，直到 in 到达 EOF 或发生读错误。
func (s *Server) Serve() error {
	reader := bufio.NewReader(s.in)
	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			s.handleLine(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// readLine 读取一行（可任意长），返回去除尾部换行后的内容。
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	// 去掉尾部 \n 与可能的 \r。
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line, err
}

func (s *Server) handleLine(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.logf("invalid json-rpc message: %v", err)
		// 解析失败时 id 未知，按规范以 null id 回 parse error。
		s.write(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "parse error"}})
		return
	}

	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		s.replyResult(req.ID, isNotification, s.initializeResult(req.Params))
	case "notifications/initialized", "initialized":
		// 通知，无需响应。
	case "tools/list":
		s.replyResult(req.ID, isNotification, s.toolsListResult())
	case "tools/call":
		s.replyResult(req.ID, isNotification, s.toolsCallResult(req.Params))
	default:
		if !isNotification {
			s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeMethodNotFnd, Message: "method not found: " + req.Method}})
		} else {
			s.logf("ignored unknown notification: %s", req.Method)
		}
	}
}

// replyResult 仅在请求（非通知）时写出 result。
func (s *Server) replyResult(id json.RawMessage, isNotification bool, result any) {
	if isNotification {
		return
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) initializeResult(params json.RawMessage) any {
	version := protocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": s.name, "version": s.version},
	}
}

func (s *Server) toolsListResult() any {
	type toolDesc struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	descs := make([]toolDesc, 0, len(s.tools))
	for _, t := range s.tools {
		descs = append(descs, toolDesc{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return map[string]any{"tools": descs}
}

func (s *Server) toolsCallResult(params json.RawMessage) ToolResult {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ErrorResult("无效的 tools/call 参数: %v", err)
	}
	for _, t := range s.tools {
		if t.Name != p.Name {
			continue
		}
		return s.invoke(t, p.Arguments)
	}
	return ErrorResult("未知工具: %s", p.Name)
}

// invoke 调用工具 Handler，并以 recover 兜底任何 panic，转为 isError 结果，
// 确保单个工具的异常不会拖垮整个 sidecar 进程。
func (s *Server) invoke(t Tool, arguments json.RawMessage) (res ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("tool %s panicked: %v", t.Name, r)
			res = ErrorResult("工具内部错误: %v", r)
		}
	}()
	return t.Handler(arguments)
}

// write 将一条 JSON-RPC 消息作为单行帧写入 out（紧凑 JSON + 换行）。
// json.Marshal 产出的紧凑 JSON 不含换行，保证不破坏帧边界。
func (s *Server) write(msg rpcResponse) {
	data, err := json.Marshal(msg)
	if err != nil {
		s.logf("marshal response failed: %v", err)
		return
	}
	if _, err := s.out.Write(append(data, '\n')); err != nil {
		s.logf("write response failed: %v", err)
	}
}
