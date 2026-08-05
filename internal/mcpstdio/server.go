package mcpstdio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func Run(in io.Reader, out io.Writer, serverURL, token string) error {
	if token == "" {
		return fmt.Errorf("PROJECTBOARD_AGENT_TOKEN is required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		result, rpcErr := dispatch(req, serverURL, token)
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			response["error"] = map[string]any{"code": -32000, "message": rpcErr.Error()}
		} else {
			response["result"] = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func dispatch(req request, serverURL, token string) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "projectboard", "version": "go-rewrite"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{
			map[string]any{"name": "poll_assignments", "description": "List unblocked work assigned to this Agent.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			map[string]any{"name": "claim_assignment", "description": "Claim one assigned work item and create a lease.", "inputSchema": map[string]any{"type": "object", "required": []string{"workItemId", "expectedVersion"}, "properties": map[string]any{"workItemId": map[string]any{"type": "string"}, "expectedVersion": map[string]any{"type": "integer"}}}},
		}}, nil
	case "tools/call":
		var call toolCall
		if err := json.Unmarshal(req.Params, &call); err != nil {
			return nil, err
		}
		var endpoint string
		var input any = map[string]any{}
		switch call.Name {
		case "poll_assignments":
			endpoint = "/api/agent/poll"
		case "claim_assignment":
			id, _ := call.Arguments["workItemId"].(string)
			if id == "" {
				return nil, fmt.Errorf("workItemId is required")
			}
			endpoint = "/api/agent/assignments/" + id + "/accept"
			input = map[string]any{"expectedVersion": call.Arguments["expectedVersion"]}
		default:
			return nil, fmt.Errorf("unknown tool %q", call.Name)
		}
		value, err := post(serverURL, token, endpoint, input)
		if err != nil {
			return nil, err
		}
		text, _ := json.MarshalIndent(value, "", "  ")
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}}, nil
	default:
		return nil, fmt.Errorf("method not found")
	}
}

func post(serverURL, token, path string, input any) (any, error) {
	body, _ := json.Marshal(input)
	req, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("ProjectBoard returned %s: %s", res.Status, data)
	}
	var value any
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func RunEnvironment() error {
	return Run(os.Stdin, os.Stdout, env("PROJECTBOARD_URL", "http://127.0.0.1:3333"), os.Getenv("PROJECTBOARD_AGENT_TOKEN"))
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
