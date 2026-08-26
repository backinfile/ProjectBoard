package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Profile struct {
	Model, Effort string
	Network       bool
}
type Request struct {
	CWD, Prompt, ThreadID string
	MCPURL, MCPToken      string
	Profile               Profile
}
type Event struct {
	Method string
	Params json.RawMessage
}
type Approval struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}
type ApprovalDecision string

const (
	ApproveOnce    ApprovalDecision = "accept"
	ApproveSession ApprovalDecision = "acceptForSession"
	Decline        ApprovalDecision = "decline"
	Cancel         ApprovalDecision = "cancel"
)

type Handler interface {
	OnEvent(Event)
	OnApproval(context.Context, Approval) (ApprovalDecision, error)
}

type Result struct {
	ThreadID string
	TurnID   string
	Status   string
}

type AppServer struct {
	Executable string
	Version    string
}

func (a AppServer) Check(ctx context.Context) error {
	exe := a.Executable
	if exe == "" {
		exe = "codex"
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, exe, "app-server", "--help")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Codex app-server unavailable: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (a AppServer) Run(ctx context.Context, req Request, handler Handler) (Result, error) {
	exe := a.Executable
	if exe == "" {
		exe = "codex"
	}
	args := []string{}
	if req.MCPURL != "" && req.MCPToken != "" {
		args = append(args, "-c", fmt.Sprintf(`mcp_servers.projectboard.url=%q`, req.MCPURL), "-c", `mcp_servers.projectboard.bearer_token_env_var="PROJECTBOARD_MCP_TOKEN"`)
	}
	args = append(args, "app-server", "--listen", "stdio://")
	cmd := exec.CommandContext(ctx, exe, args...)
	if req.MCPToken != "" {
		cmd.Env = append(os.Environ(), "PROJECTBOARD_MCP_TOKEN="+req.MCPToken)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	var writeMu sync.Mutex
	send := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		_, err = stdin.Write(raw)
		return err
	}
	go drain(stderr, handler)
	if err := send(map[string]any{"method": "initialize", "id": 1, "params": map[string]any{"clientInfo": map[string]string{"name": "projectboard", "title": "ProjectBoard", "version": "0.1.0"}}}); err != nil {
		return Result{}, err
	}
	threadMethod := "thread/start"
	params := map[string]any{"cwd": req.CWD, "approvalPolicy": "unlessTrusted", "sandbox": "workspaceWrite", "serviceName": "projectboard"}
	if req.Profile.Model != "" {
		params["model"] = req.Profile.Model
	}
	if req.ThreadID != "" {
		threadMethod = "thread/resume"
		params["threadId"] = req.ThreadID
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	result := Result{}
	turnStarted := false
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var msg wireMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			decision, approvalErr := handler.OnApproval(ctx, Approval{ID: msg.ID, Method: msg.Method, Params: msg.Params})
			if approvalErr != nil {
				decision = Decline
			}
			if err := send(map[string]any{"id": json.RawMessage(msg.ID), "result": map[string]any{"decision": decision}}); err != nil {
				return result, err
			}
			continue
		}
		if string(msg.ID) == "1" {
			if msg.Error != nil {
				return result, errors.New(msg.Error.Message)
			}
			if err := send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
				return result, err
			}
			if err := send(map[string]any{"method": threadMethod, "id": 2, "params": params}); err != nil {
				return result, err
			}
			continue
		}
		if string(msg.ID) == "2" && !turnStarted {
			if msg.Error != nil {
				return result, errors.New(msg.Error.Message)
			}
			var response struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			}
			if err := json.Unmarshal(msg.Result, &response); err != nil {
				return result, err
			}
			result.ThreadID = response.Thread.ID
			turnParams := map[string]any{"threadId": result.ThreadID, "input": []map[string]string{{"type": "text", "text": req.Prompt}}, "cwd": req.CWD, "approvalPolicy": "unlessTrusted", "sandboxPolicy": map[string]any{"type": "workspaceWrite", "writableRoots": []string{req.CWD}, "networkAccess": req.Profile.Network}}
			if req.Profile.Model != "" {
				turnParams["model"] = req.Profile.Model
			}
			if req.Profile.Effort != "" {
				turnParams["effort"] = req.Profile.Effort
			}
			if err := send(map[string]any{"method": "turn/start", "id": 3, "params": turnParams}); err != nil {
				return result, err
			}
			turnStarted = true
			continue
		}
		if msg.Method != "" {
			handler.OnEvent(Event{Method: msg.Method, Params: msg.Params})
			if msg.Method == "turn/started" {
				var p struct {
					Turn struct {
						ID string `json:"id"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				result.TurnID = p.Turn.ID
			}
			if msg.Method == "turn/completed" {
				var p struct {
					Turn struct {
						Status string `json:"status"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(msg.Params, &p)
				result.Status = p.Turn.Status
				_ = stdin.Close()
				_ = cmd.Wait()
				return result, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if err := cmd.Wait(); err != nil {
		return result, err
	}
	return result, io.EOF
}

type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func drain(r io.Reader, h Handler) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		h.OnEvent(Event{Method: "process/stderr", Params: json.RawMessage(strconvQuote(scanner.Text()))})
	}
}
func strconvQuote(v string) string { raw, _ := json.Marshal(v); return string(raw) }
