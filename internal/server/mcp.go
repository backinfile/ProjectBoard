package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/projectboard/projectboard/internal/store"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	projectID, taskID, runID, permissions, err := s.store.ResolveCapability(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "无效或已撤销的 Run 能力令牌")
		return
	}
	var req mcpRequest
	if !decode(w, r, &req) {
		return
	}
	result := map[string]any{}
	switch req.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "projectboard", "version": "0.1.0"}}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "tools/list":
		result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		if !contains(permissions, req.Params.Name) {
			mcpError(w, req.ID, -32603, "工具未获得本次 Run 授权")
			return
		}
		value, callErr := s.callMCP(r, projectID, taskID, runID, req.Params.Name, req.Params.Arguments)
		if callErr != nil {
			if callErr == store.ErrNotFound {
				mcpError(w, req.ID, -32602, "内容不存在或不可读")
			} else {
				mcpError(w, req.ID, -32603, callErr.Error())
			}
			return
		}
		raw, _ := json.Marshal(value)
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(raw)}}, "structuredContent": value}
	default:
		mcpError(w, req.ID, -32601, "method not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *Server) callMCP(r *http.Request, projectID, taskID, runID, name string, args map[string]any) (any, error) {
	_ = s.store.AuditMCP(r.Context(), projectID, taskID, runID, name, args)
	switch name {
	case "knowledge.search":
		return s.store.ListReadableKnowledge(r.Context(), projectID, stringArg(args, "query"))
	case "knowledge.read":
		return s.store.GetReadableKnowledge(r.Context(), stringArg(args, "id"), projectID)
	case "task.conversations.list":
		return s.store.ListConversations(r.Context(), taskID)
	case "task.conversations.read":
		c, err := s.store.GetConversation(r.Context(), stringArg(args, "conversationId"))
		if err != nil || c.TaskID != taskID {
			if err == nil {
				err = store.ErrNotFound
			}
			return nil, err
		}
		messages, err := s.store.ListMessages(r.Context(), c.ID)
		return map[string]any{"conversation": c, "messages": messages}, err
	}
	return nil, store.ErrNotFound
}

func mcpTools() []map[string]any {
	return []map[string]any{
		{"name": "knowledge.search", "description": "搜索当前项目中本次 Run 可见的知识", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]string{"type": "string"}}}},
		{"name": "knowledge.read", "description": "读取一条可见知识的正文", "inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{"id": map[string]string{"type": "string"}}}},
		{"name": "task.conversations.list", "description": "列出当前任务的其他 Agent 对话及摘要", "inputSchema": map[string]string{"type": "object"}},
		{"name": "task.conversations.read", "description": "读取当前任务中的指定对话", "inputSchema": map[string]any{"type": "object", "required": []string{"conversationId"}, "properties": map[string]any{"conversationId": map[string]string{"type": "string"}}}},
	}
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func stringArg(args map[string]any, key string) string { v, _ := args[key].(string); return v }
func mcpError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
