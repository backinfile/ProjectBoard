package knowledgemcp

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/projectboard/projectboard/internal/knowledge"
	"github.com/projectboard/projectboard/internal/store"
	_ "modernc.org/sqlite"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Serve(ctx context.Context, databasePath, projectID string, input io.Reader, output io.Writer) error {
	database, err := openReadOnly(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	module := knowledge.New(&store.Store{DB: database})
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var in request
		if err = json.Unmarshal(scanner.Bytes(), &in); err != nil {
			continue
		}
		if len(in.ID) == 0 {
			continue
		}
		result, callErr := handle(ctx, module, projectID, in)
		out := response{JSONRPC: "2.0", ID: in.ID, Result: result}
		if callErr != nil {
			out.Result = nil
			out.Error = &rpcError{Code: -32603, Message: callErr.Error()}
		}
		if err = encoder.Encode(out); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handle(ctx context.Context, module *knowledge.Module, projectID string, in request) (any, error) {
	switch in.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(in.Params, &params)
		if params.ProtocolVersion == "" {
			params.ProtocolVersion = "2025-11-25"
		}
		return map[string]any{
			"protocolVersion": params.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "projectboard-knowledge", "version": "1"},
			"instructions":    "Search project knowledge when a task depends on project-specific decisions, conventions, or runbooks. Read relevant nodes before relying on them. Rewrite an unsuccessful query at most once.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools()}, nil
	case "tools/call":
		return callTool(ctx, module, projectID, in.Params)
	default:
		return nil, fmt.Errorf("unsupported method %s", in.Method)
	}
}

func tools() []map[string]any {
	readOnly := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	return []map[string]any{
		{
			"name": "knowledge_search", "title": "Search project knowledge",
			"description": "Search the current project's knowledge base. Use when work depends on project-specific decisions, conventions, architecture, release procedures, or runbooks; do not use for generic programming facts.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "A concise search query using project terms, identifiers, paths, or concepts."}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "default": 5}}, "required": []string{"query"}, "additionalProperties": false},
			"annotations": readOnly,
		},
		{
			"name": "knowledge_read", "title": "Read project knowledge",
			"description": "Read the complete current versions of knowledge nodes returned by knowledge_search. Node access is restricted to the current project.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"nodeIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 10}}, "required": []string{"nodeIds"}, "additionalProperties": false},
			"annotations": readOnly,
		},
	}
}

func callTool(ctx context.Context, module *knowledge.Module, projectID string, raw json.RawMessage) (any, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	var value any
	switch params.Name {
	case "knowledge_search":
		var arguments struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(params.Arguments, &arguments); err != nil {
			return nil, err
		}
		hits, err := module.Search(ctx, projectID, arguments.Query, arguments.Limit)
		if err != nil {
			return nil, err
		}
		value = map[string]any{"results": hits}
	case "knowledge_read":
		var arguments struct {
			NodeIDs []string `json:"nodeIds"`
		}
		if err := json.Unmarshal(params.Arguments, &arguments); err != nil {
			return nil, err
		}
		if len(arguments.NodeIDs) == 0 || len(arguments.NodeIDs) > 10 {
			return nil, fmt.Errorf("provide between 1 and 10 nodeIds")
		}
		nodes := []knowledge.Node{}
		for _, id := range arguments.NodeIDs {
			node, err := module.GetForProject(ctx, projectID, id)
			if err == nil {
				nodes = append(nodes, *node)
			} else if !knowledge.IsCode(err, "NOT_FOUND") {
				return nil, err
			}
		}
		value = map[string]any{"nodes": nodes}
	default:
		return nil, fmt.Errorf("unknown tool %s", params.Name)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(encoded)}}, "structuredContent": value}, nil
}

func openReadOnly(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(absolute) + "?mode=ro"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA query_only=ON", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = database.Exec(pragma); err != nil {
			database.Close()
			return nil, err
		}
	}
	var version int
	if err = database.QueryRow("SELECT version FROM schema_metadata WHERE id=1").Scan(&version); err != nil {
		database.Close()
		return nil, err
	}
	if version != store.SchemaVersion {
		database.Close()
		return nil, fmt.Errorf("database schema %d is incompatible with knowledge MCP", version)
	}
	return database, nil
}
