package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/agentexec"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/workqueue"
	"golang.org/x/crypto/ssh"
)

const agentInstructions = "Continue processing ProjectBoard tasks until the user cancels. Wait for work, atomically claim one task, maintain its lease, work only inside returned project/task permissions, record environment and progress, submit or block/release it, then wait for the next task. Never use local Git credentials or print credentials. Stop only for revoked identity, host verification failure, or a systemic security error."

type sshGateway struct {
	server      *Server
	listener    net.Listener
	config      *ssh.ServerConfig
	hostKey     ssh.PublicKey
	mu          sync.Mutex
	closeOnce   sync.Once
	done        chan struct{}
	primary     map[string]*activeSSHSession
	credentials map[string]*issuedGitCredential
}

type activeSSHSession struct {
	id, agentID, keyID string
	conn               *ssh.ServerConn
	ctx                context.Context
	cancel             context.CancelFunc
}

type issuedGitCredential struct {
	agentID, token, expiresAt string
}

func (s *Server) startSSH() error {
	if s.ssh != nil {
		return nil
	}
	signer, err := loadOrCreateSSHHostKey(s.config.SSHHostKeyPath)
	if err != nil {
		return fmt.Errorf("load SSH host key: %w", err)
	}
	gateway := &sshGateway{server: s, hostKey: signer.PublicKey(), done: make(chan struct{}), primary: map[string]*activeSSHSession{}, credentials: map[string]*issuedGitCredential{}}
	configuration := &ssh.ServerConfig{PublicKeyCallback: gateway.authenticatePublicKey, ServerVersion: "SSH-2.0-ProjectBoard"}
	configuration.AddHostKey(signer)
	listener, err := net.Listen("tcp", s.config.SSHListenAddress)
	if err != nil {
		return fmt.Errorf("listen for Agent SSH on %s: %w", s.config.SSHListenAddress, err)
	}
	gateway.listener, gateway.config = listener, configuration
	s.ssh = gateway
	go gateway.acceptLoop()
	go gateway.credentialSweepLoop()
	return nil
}

func (g *sshGateway) close() {
	g.closeOnce.Do(func() { close(g.done) })
	g.mu.Lock()
	if g.listener != nil {
		_ = g.listener.Close()
	}
	for _, session := range g.primary {
		_ = session.conn.Close()
	}
	g.primary = map[string]*activeSSHSession{}
	g.mu.Unlock()
}

func (g *sshGateway) credentialSweepLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-g.done:
			return
		case <-ticker.C:
			g.sweepCredentials(context.Background())
		}
	}
}

func (g *sshGateway) sweepCredentials(ctx context.Context) {
	type candidate struct{ executionID, token string }
	candidates := []candidate{}
	g.mu.Lock()
	for executionID, credential := range g.credentials {
		candidates = append(candidates, candidate{executionID: executionID, token: credential.token})
	}
	g.mu.Unlock()
	for _, value := range candidates {
		var valid int
		err := g.server.store.DB.QueryRowContext(ctx, `SELECT 1 FROM agent_executions e JOIN leases l ON l.id=e.lease_id
			WHERE e.id=? AND e.ended_at IS NULL AND l.released_at IS NULL AND l.expires_at>?`, value.executionID, now()).Scan(&valid)
		if err == nil {
			continue
		}
		g.mu.Lock()
		current := g.credentials[value.executionID]
		if current != nil && current.token == value.token {
			delete(g.credentials, value.executionID)
		}
		g.mu.Unlock()
		if current != nil && current.token == value.token {
			_ = g.server.gitClient().RevokeExecutionCredential(ctx, value.token)
		}
	}
}

func loadOrCreateSSHHostKey(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		if err := protectSSHHostKey(path); err != nil {
			return nil, err
		}
		return ssh.ParsePrivateKey(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	if err := protectSSHHostKey(path); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(privateKey)
}

func (g *sshGateway) hostFingerprint() string { return ssh.FingerprintSHA256(g.hostKey) }

func (g *sshGateway) authenticatePublicKey(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if key.Type() != ssh.KeyAlgoED25519 {
		return nil, errors.New("ProjectBoard accepts Ed25519 Agent keys only")
	}
	rows, err := g.server.store.DB.Query(`SELECT k.id,k.public_key FROM agent_ssh_keys k JOIN agents a ON a.id=k.agent_id
		WHERE k.agent_id=? AND k.revoked_at IS NULL AND a.revoked_at IS NULL AND a.status='active'`, metadata.User())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID, encoded string
		if err := rows.Scan(&keyID, &encoded); err != nil {
			return nil, err
		}
		stored, _, _, _, err := ssh.ParseAuthorizedKey([]byte(encoded))
		if err == nil && bytes.Equal(stored.Marshal(), key.Marshal()) {
			return &ssh.Permissions{Extensions: map[string]string{"agent_id": metadata.User(), "key_id": keyID}}, nil
		}
	}
	return nil, errors.New("Agent SSH key is not authorized")
}

func (g *sshGateway) acceptLoop() {
	for {
		connection, err := g.listener.Accept()
		if err != nil {
			return
		}
		go g.serveConnection(connection)
	}
}

func (g *sshGateway) serveConnection(raw net.Conn) {
	connection, channels, requests, err := ssh.NewServerConn(raw, g.config)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer connection.Close()
	go ssh.DiscardRequests(requests)
	for request := range channels {
		if request.ChannelType() != "session" {
			_ = request.Reject(ssh.Prohibited, "only ProjectBoard commands are allowed")
			continue
		}
		channel, channelRequests, err := request.Accept()
		if err != nil {
			continue
		}
		g.serveChannel(connection, channel, channelRequests)
	}
}

func (g *sshGateway) serveChannel(connection *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			return
		}
		command := strings.TrimSpace(payload.Command)
		agentID := connection.Permissions.Extensions["agent_id"]
		keyID := connection.Permissions.Extensions["key_id"]
		if command == "projectboard-mcp" {
			session, err := g.registerPrimary(connection, agentID, keyID)
			if err != nil {
				_, _ = io.WriteString(channel.Stderr(), err.Error()+"\n")
				_ = request.Reply(false, nil)
				return
			}
			_ = request.Reply(true, nil)
			g.serveMCP(channel, session)
			g.unregisterPrimary(session, "disconnected")
			sendSSHExitStatus(channel, 0)
			return
		}
		if strings.HasPrefix(command, "projectboard-git-credential ") {
			executionID := strings.TrimSpace(strings.TrimPrefix(command, "projectboard-git-credential "))
			_ = request.Reply(true, nil)
			if err := g.serveGitCredential(channel, agentID, keyID, executionID); err != nil {
				_, _ = io.WriteString(channel.Stderr(), err.Error()+"\n")
				sendSSHExitStatus(channel, 1)
			} else {
				sendSSHExitStatus(channel, 0)
			}
			return
		}
		_ = request.Reply(false, nil)
		return
	}
}

func sendSSHExitStatus(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
}

func (g *sshGateway) registerPrimary(connection *ssh.ServerConn, agentID, keyID string) (*activeSSHSession, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.primary[agentID]; exists {
		return nil, errors.New("this Agent already has an active SSH session")
	}
	stamp, sessionID := now(), security.Token(18)
	clientVersion := strings.TrimPrefix(string(connection.ClientVersion()), "SSH-2.0-")
	if _, err := g.server.store.DB.Exec(`INSERT INTO agent_ssh_sessions(id,agent_id,key_id,remote_addr,client_version,connected_at,last_seen_at)
		VALUES(?,?,?,?,?,?,?)`, sessionID, agentID, keyID, connection.RemoteAddr().String(), clientVersion, stamp, stamp); err != nil {
		return nil, err
	}
	_, _ = g.server.store.DB.Exec("UPDATE agent_ssh_keys SET last_used_at=? WHERE id=?", stamp, keyID)
	ctx, cancel := context.WithCancel(context.Background())
	session := &activeSSHSession{id: sessionID, agentID: agentID, keyID: keyID, conn: connection, ctx: ctx, cancel: cancel}
	g.primary[agentID] = session
	return session, nil
}

func (g *sshGateway) unregisterPrimary(session *activeSSHSession, reason string) {
	session.cancel()
	g.mu.Lock()
	if current := g.primary[session.agentID]; current != nil && current.id == session.id {
		delete(g.primary, session.agentID)
	}
	g.mu.Unlock()
	_, _ = g.server.store.DB.Exec("UPDATE agent_ssh_sessions SET disconnected_at=?,disconnect_reason=? WHERE id=? AND disconnected_at IS NULL", now(), reason, session.id)
	g.revokeAgentCredentials(context.Background(), session.agentID)
}

func (g *sshGateway) disconnectAgent(agentID, reason string, release bool) {
	g.mu.Lock()
	session := g.primary[agentID]
	g.mu.Unlock()
	if session != nil {
		session.cancel()
		_ = session.conn.Close()
		g.unregisterPrimary(session, reason)
	}
	if release {
		active, _ := g.server.exec.Active(context.Background(), agentID)
		if active != nil {
			_ = g.server.exec.Release(context.Background(), agentID, active.ID, active.LeaseID, reason, false)
		}
	}
}

func (g *sshGateway) disconnectKey(agentID, keyID, reason string, release bool) {
	g.mu.Lock()
	session := g.primary[agentID]
	g.mu.Unlock()
	if session == nil || session.keyID != keyID {
		return
	}
	g.disconnectAgent(agentID, reason, release)
}

func (g *sshGateway) serveGitCredential(channel ssh.Channel, agentID, keyID, executionID string) error {
	if err := g.server.exec.Authorize(agentID, keyID); err != nil {
		return err
	}
	g.mu.Lock()
	primary := g.primary[agentID]
	g.mu.Unlock()
	if primary == nil || primary.keyID != keyID {
		return errors.New("an active MCP session using the same key is required")
	}
	credential, err := g.issueGitCredential(context.Background(), agentID, executionID)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(io.LimitReader(channel, 64<<10))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			break
		}
	}
	_, err = fmt.Fprintf(channel, "username=x-access-token\npassword=%s\n\n", credential.token)
	return err
}

func (g *sshGateway) issueGitCredential(ctx context.Context, agentID, executionID string) (*issuedGitCredential, error) {
	g.mu.Lock()
	if existing := g.credentials[executionID]; existing != nil && existing.agentID == agentID && existing.expiresAt > time.Now().UTC().Add(5*time.Minute).Format(time.RFC3339Nano) {
		g.mu.Unlock()
		return existing, nil
	}
	g.mu.Unlock()
	var installationID, repositoryID, provider string
	err := g.server.store.DB.QueryRowContext(ctx, `SELECT a.installation_id,rg.repository_id,a.provider FROM agent_executions e
		JOIN leases l ON l.id=e.lease_id JOIN work_items w ON w.id=e.work_item_id
		JOIN agent_project_grants ag ON ag.project_id=w.project_id AND ag.agent_id=e.agent_id
		JOIN project_repository_grants rg ON rg.project_id=w.project_id AND rg.revoked_at IS NULL AND rg.access_level='write'
		JOIN provider_authorizations a ON a.id=rg.authorization_id AND a.status='active'
		WHERE e.id=? AND e.agent_id=? AND e.ended_at IS NULL AND l.released_at IS NULL AND l.expires_at>?`, executionID, agentID, now()).Scan(&installationID, &repositoryID, &provider)
	if err != nil {
		return nil, errors.New("active execution with a GitHub repository grant is required")
	}
	if provider != "github" {
		return nil, errors.New("Agent Git execution supports GitHub only")
	}
	issued, err := g.server.gitClient().IssueExecutionCredential(ctx, installationID, repositoryID)
	if err != nil {
		return nil, err
	}
	credential := &issuedGitCredential{agentID: agentID, token: issued.Token, expiresAt: issued.ExpiresAt}
	g.mu.Lock()
	g.credentials[executionID] = credential
	g.mu.Unlock()
	return credential, nil
}

func (g *sshGateway) revokeAgentCredentials(ctx context.Context, agentID string) {
	var tokens []string
	g.mu.Lock()
	for executionID, credential := range g.credentials {
		if credential.agentID == agentID {
			tokens = append(tokens, credential.token)
			delete(g.credentials, executionID)
		}
	}
	g.mu.Unlock()
	for _, token := range tokens {
		_ = g.server.gitClient().RevokeExecutionCredential(ctx, token)
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (g *sshGateway) serveMCP(stream io.ReadWriter, session *activeSSHSession) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 4096), 32<<20)
	encoder := json.NewEncoder(stream)
	for scanner.Scan() {
		var request mcpRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		if err := g.server.exec.Authorize(session.agentID, session.keyID); err != nil {
			response["error"] = mcpError(err)
			_ = encoder.Encode(response)
			_ = session.conn.Close()
			return
		}
		_, _ = g.server.store.DB.Exec("UPDATE agent_ssh_sessions SET last_seen_at=? WHERE id=?", now(), session.id)
		result, err := g.dispatchMCP(request, session)
		if err != nil {
			response["error"] = mcpError(err)
		} else {
			response["result"] = result
		}
		if encoder.Encode(response) != nil {
			return
		}
	}
}

func (g *sshGateway) dispatchMCP(request mcpRequest, session *activeSSHSession) (any, error) {
	switch request.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "projectboard", "version": "ssh-agent-v1"}, "instructions": agentInstructions}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": agentMCPTools()}, nil
	case "tools/call":
		var call mcpToolCall
		if err := json.Unmarshal(request.Params, &call); err != nil {
			return nil, err
		}
		value, err := g.callAgentTool(session.ctx, session.agentID, call)
		if err != nil {
			return nil, err
		}
		text, _ := json.MarshalIndent(value, "", "  ")
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}}, nil
	default:
		return nil, errors.New("method not found")
	}
}

func agentMCPTools() []any {
	tool := func(name, description string, required []string, properties map[string]any) map[string]any {
		if required == nil {
			required = []string{}
		}
		if properties == nil {
			properties = map[string]any{}
		}
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "required": required, "properties": properties}}
	}
	stringField := map[string]any{"type": "string"}
	integerField := map[string]any{"type": "integer"}
	return []any{
		tool("list_projects", "List projects granted to this Agent.", nil, nil),
		tool("list_tasks", "List claimable assigned and permitted unassigned tasks.", nil, nil),
		tool("get_task", "Read one task in a granted project.", []string{"workItemId"}, map[string]any{"workItemId": stringField}),
		tool("wait_for_task", "Long-poll for the next claimable task; call again after an idle result.", nil, map[string]any{"timeoutSeconds": integerField}),
		tool("claim_task", "Atomically claim a task and create an execution lease.", []string{"workItemId", "expectedVersion"}, map[string]any{"workItemId": stringField, "expectedVersion": integerField}),
		tool("get_active_execution", "Resume this Agent's active execution after reconnecting.", nil, nil),
		tool("heartbeat_execution", "Extend an active execution lease.", []string{"executionId", "leaseId"}, map[string]any{"executionId": stringField, "leaseId": stringField}),
		tool("release_task", "Release an execution, optionally marking the task blocked.", []string{"executionId", "leaseId", "reason"}, map[string]any{"executionId": stringField, "leaseId": stringField, "reason": stringField, "blocked": map[string]any{"type": "boolean"}}),
		tool("post_message", "Post progress to the task conversation.", []string{"executionId", "markdown", "expectedVersion"}, map[string]any{"executionId": stringField, "markdown": stringField, "expectedVersion": integerField}),
		tool("record_progress", "Record a durable progress update on the active task.", []string{"executionId", "markdown", "expectedVersion"}, map[string]any{"executionId": stringField, "markdown": stringField, "expectedVersion": integerField}),
		tool("record_environment_step", "Record an environment command and its outcome.", []string{"executionId", "command", "exitCode"}, map[string]any{"executionId": stringField, "command": stringField, "exitCode": integerField, "durationMs": integerField, "summary": stringField}),
		tool("freeze_discussion_conclusion", "Freeze the implementation conclusion and enter execution.", []string{"executionId", "expectedVersion", "goal", "plan", "criteria"}, map[string]any{"executionId": stringField, "expectedVersion": integerField, "goal": stringField, "scope": stringField, "outOfScope": stringField, "plan": stringField, "criteria": stringField, "risks": stringField}),
		tool("move_task_stage", "Move an actively leased task to an allowed stage.", []string{"executionId", "expectedVersion", "targetStage"}, map[string]any{"executionId": stringField, "expectedVersion": integerField, "targetStage": stringField, "noteMarkdown": stringField}),
		tool("mark_task_blocked", "Block the task and end the current execution.", []string{"executionId", "leaseId", "reason"}, map[string]any{"executionId": stringField, "leaseId": stringField, "reason": stringField}),
		tool("clear_task_blocked", "Clear a task blocker during an active execution.", []string{"executionId", "expectedVersion"}, map[string]any{"executionId": stringField, "expectedVersion": integerField}),
		tool("submit_acceptance_result", "Submit an Agent acceptance result when project policy allows it.", []string{"executionId", "expectedVersion", "outcome"}, map[string]any{"executionId": stringField, "expectedVersion": integerField, "outcome": stringField, "noteMarkdown": stringField, "criteriaResults": map[string]any{"type": "object"}}),
		tool("submit_execution_result", "Submit commits, validations, and final execution evidence.", []string{"executionId", "leaseId", "expectedVersion", "summaryMarkdown"}, map[string]any{"executionId": stringField, "leaseId": stringField, "expectedVersion": integerField, "summaryMarkdown": stringField, "pushed": map[string]any{"type": "boolean"}, "worktreeClean": map[string]any{"type": "boolean"}, "forbiddenPathsClean": map[string]any{"type": "boolean"}, "commits": map[string]any{"type": "array", "items": stringField}, "changedFiles": map[string]any{"type": "array", "items": stringField}, "validations": map[string]any{"type": "array"}, "remainingRisksMarkdown": stringField}),
		tool("sync_commits", "Ask ProjectBoard to synchronize GitHub commits for the active task project.", []string{"executionId"}, map[string]any{"executionId": stringField}),
		tool("upload_attachment", "Upload a base64-encoded task attachment (maximum 25 MiB).", []string{"executionId", "name", "contentBase64"}, map[string]any{"executionId": stringField, "name": stringField, "mime": stringField, "contentBase64": stringField}),
		tool("create_subtask", "Create an assigned child task when project policy allows it.", []string{"executionId", "title"}, map[string]any{"executionId": stringField, "title": stringField, "descriptionMarkdown": stringField, "acceptanceCriteriaMarkdown": stringField, "priority": stringField, "targetBranch": stringField}),
	}
}

func (g *sshGateway) callAgentTool(ctx context.Context, agentID string, call mcpToolCall) (any, error) {
	a := call.Arguments
	switch call.Name {
	case "list_projects":
		return g.server.exec.ListProjects(ctx, agentID)
	case "list_tasks":
		return g.server.exec.ListTasks(ctx, agentID)
	case "get_task":
		return g.server.exec.GetTask(ctx, agentID, stringArg(a, "workItemId"))
	case "wait_for_task":
		task, err := g.server.exec.WaitForTask(ctx, agentID, time.Duration(intArg(a, "timeoutSeconds", 55))*time.Second)
		if err != nil {
			return nil, err
		}
		if task == nil {
			return map[string]any{"status": "idle", "retry": true}, nil
		}
		return map[string]any{"status": "ready", "task": task}, nil
	case "claim_task":
		return g.server.exec.Claim(ctx, agentID, stringArg(a, "workItemId"), int64Arg(a, "expectedVersion"))
	case "get_active_execution":
		return g.server.exec.Active(ctx, agentID)
	case "heartbeat_execution":
		expires, err := g.server.exec.Heartbeat(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "leaseId"))
		return map[string]any{"valid": err == nil, "expiresAt": expires}, err
	case "release_task":
		err := g.server.exec.Release(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "leaseId"), stringArg(a, "reason"), boolArg(a, "blocked"))
		g.revokeAgentCredentials(ctx, agentID)
		return map[string]any{"released": err == nil}, err
	case "mark_task_blocked":
		err := g.server.exec.Release(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "leaseId"), stringArg(a, "reason"), true)
		g.revokeAgentCredentials(ctx, agentID)
		return map[string]any{"blocked": err == nil}, err
	case "post_message", "record_progress":
		return g.server.exec.RecordProgress(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "markdown"), int64Arg(a, "expectedVersion"))
	case "record_environment_step":
		err := g.server.exec.RecordEnvironment(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "command"), intArg(a, "exitCode", 0), intArg(a, "durationMs", 0), stringArg(a, "summary"))
		return map[string]any{"recorded": err == nil}, err
	case "freeze_discussion_conclusion":
		return g.server.exec.FreezeConclusion(ctx, agentID, stringArg(a, "executionId"), workqueue.ConclusionInput{ExpectedVersion: int64Arg(a, "expectedVersion"), Goal: stringArg(a, "goal"), Scope: stringArg(a, "scope"), OutOfScope: stringArg(a, "outOfScope"), Plan: stringArg(a, "plan"), Criteria: stringArg(a, "criteria"), Risks: stringArg(a, "risks")})
	case "move_task_stage":
		return g.server.exec.MoveStage(ctx, agentID, stringArg(a, "executionId"), int64Arg(a, "expectedVersion"), stringArg(a, "targetStage"), stringArg(a, "noteMarkdown"))
	case "clear_task_blocked":
		return g.server.exec.SetBlocked(ctx, agentID, stringArg(a, "executionId"), int64Arg(a, "expectedVersion"), "", false)
	case "submit_acceptance_result":
		return g.server.exec.SubmitAcceptance(ctx, agentID, stringArg(a, "executionId"), workqueue.AcceptanceInput{ExpectedVersion: int64Arg(a, "expectedVersion"), Outcome: stringArg(a, "outcome"), Note: stringArg(a, "noteMarkdown"), CriteriaResults: a["criteriaResults"]})
	case "submit_execution_result":
		validations := []workqueue.Validation{}
		raw, _ := json.Marshal(a["validations"])
		_ = json.Unmarshal(raw, &validations)
		item, err := g.server.exec.SubmitExecution(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "leaseId"), workqueue.ExecutionInput{ExpectedVersion: int64Arg(a, "expectedVersion"), Summary: stringArg(a, "summaryMarkdown"), Pushed: boolArg(a, "pushed"), WorktreeClean: boolArg(a, "worktreeClean"), ForbiddenPathsClean: boolArg(a, "forbiddenPathsClean"), Commits: stringSliceArg(a, "commits"), ChangedFiles: stringSliceArg(a, "changedFiles"), Validations: validations, RemainingRisks: stringArg(a, "remainingRisksMarkdown")})
		g.revokeAgentCredentials(ctx, agentID)
		return item, err
	case "sync_commits":
		itemID, err := g.server.exec.WorkItemID(ctx, agentID, stringArg(a, "executionId"))
		if err != nil {
			return nil, err
		}
		item, err := g.server.queue.Get(ctx, itemID)
		if err != nil {
			return nil, err
		}
		return g.server.syncProjectCommits(ctx, item.ProjectID, actor{Type: "agent", ID: agentID})
	case "upload_attachment":
		return g.server.uploadAgentAttachment(ctx, agentID, stringArg(a, "executionId"), stringArg(a, "name"), stringArg(a, "mime"), stringArg(a, "contentBase64"))
	case "create_subtask":
		return g.server.createAgentSubtaskDirect(ctx, agentID, stringArg(a, "executionId"), a)
	default:
		return nil, fmt.Errorf("unknown tool %q", call.Name)
	}
}

func (s *Server) uploadAgentAttachment(ctx context.Context, agentID, executionID, name, mimeType, encoded string) (map[string]any, error) {
	itemID, err := s.exec.WorkItemID(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(content) == 0 || len(content) > 25<<20 {
		return nil, errors.New("attachment must be valid base64 between 1 byte and 25 MiB")
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		name = "attachment"
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	digest := security.HashOpaque(string(content))
	attachmentID := security.Token(18)
	storageKey := attachmentID + filepath.Ext(name)
	directory := filepath.Join(s.config.DataDir, "attachments")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, storageKey), content, 0o600); err != nil {
		return nil, err
	}
	stamp := now()
	_, err = s.store.DB.ExecContext(ctx, "INSERT INTO attachments(id,work_item_id,original_name,mime,size,sha256,storage_key,uploader_type,uploader_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", attachmentID, itemID, name, mimeType, len(content), digest, storageKey, "agent", agentID, stamp)
	if err != nil {
		_ = os.Remove(filepath.Join(directory, storageKey))
		return nil, err
	}
	return map[string]any{"id": attachmentID, "originalName": name, "mime": mimeType, "size": len(content), "createdAt": stamp}, nil
}

func (s *Server) createAgentSubtaskDirect(ctx context.Context, agentID, executionID string, input map[string]any) (*workqueue.WorkItem, error) {
	parentID, err := s.exec.WorkItemID(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	var projectID string
	var allowSubtasks, count int
	err = s.store.DB.QueryRowContext(ctx, `SELECT w.project_id,p.allow_subtasks,(SELECT COUNT(*) FROM work_items c WHERE c.parent_id=w.id AND c.stage NOT IN('completed','order_closed','abandoned'))
		FROM work_items w JOIN projects p ON p.id=w.project_id WHERE w.id=?`, parentID).Scan(&projectID, &allowSubtasks, &count)
	if err != nil {
		return nil, err
	}
	if allowSubtasks == 0 || count >= 20 {
		return nil, errors.New("project subtask policy does not allow another child task")
	}
	priority := stringArg(input, "priority")
	if priority == "" {
		priority = "medium"
	}
	return s.queue.Create(ctx, workqueue.Actor{Type: "agent", ID: agentID}, workqueue.CreateInput{ProjectID: projectID, ParentID: parentID, Title: stringArg(input, "title"), DescriptionMarkdown: stringArg(input, "descriptionMarkdown"), AcceptanceCriteriaMarkdown: stringArg(input, "acceptanceCriteriaMarkdown"), Priority: priority, TargetBranch: stringArg(input, "targetBranch"), AssigneeKind: "agent", AssigneeID: agentID, Stage: "todo"})
}

func mcpError(err error) map[string]any {
	code := "INTERNAL_ERROR"
	var domain *agentexec.Error
	if errors.As(err, &domain) {
		code = domain.Code
	}
	var queueError *workqueue.Error
	if errors.As(err, &queueError) {
		code = queueError.Code
	}
	return map[string]any{"code": -32000, "message": err.Error(), "data": map[string]any{"code": code}}
}

func stringArg(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}
func int64Arg(values map[string]any, key string) int64 { return int64(intArg(values, key, 0)) }
func intArg(values map[string]any, key string, fallback int) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case int:
		return value
	default:
		return fallback
	}
}
func boolArg(values map[string]any, key string) bool { value, _ := values[key].(bool); return value }
func stringSliceArg(values map[string]any, key string) []string {
	raw, _ := values[key].([]any)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
