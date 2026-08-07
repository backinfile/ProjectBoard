package server

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/workqueue"
	"golang.org/x/crypto/ssh"
)

func TestAgentSSHAuthenticatesDiscoversAndClaimsTask(t *testing.T) {
	app, clientConfig, agentID, keyID, itemID := newSSHAgentFixture(t)
	defer app.Close()

	client, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	input, _ := session.StdinPipe()
	output, _ := session.StdoutPipe()
	if err := session.Start("projectboard-mcp"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(output)
	callMCP := func(value any) map[string]any {
		t.Helper()
		if err := json.NewEncoder(input).Encode(value); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		if response["error"] != nil {
			t.Fatalf("MCP error: %#v", response["error"])
		}
		return response
	}
	initialized := callMCP(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	result := initialized["result"].(map[string]any)
	if result["instructions"] == "" {
		t.Fatal("initialize did not return Agent workflow instructions")
	}
	listed := callMCP(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "list_tasks", "arguments": map[string]any{}}})
	if listed["result"] == nil {
		t.Fatal("list_tasks returned no result")
	}
	claimed := callMCP(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "claim_task", "arguments": map[string]any{"workItemId": itemID, "expectedVersion": 1}}})
	if claimed["result"] == nil {
		t.Fatal("claim_task returned no result")
	}

	second, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	secondSession, _ := second.NewSession()
	if err := secondSession.Run("projectboard-mcp"); err == nil {
		t.Fatal("second concurrent Agent MCP session was accepted")
	}
	second.Close()

	if _, err := app.server.store.DB.Exec("UPDATE agent_ssh_keys SET revoked_at=? WHERE id=?", now(), keyID); err != nil {
		t.Fatal(err)
	}
	app.server.ssh.disconnectAgent(agentID, "ssh_key_revoked", true)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var active int
		err := app.server.store.DB.QueryRow("SELECT 1 FROM leases WHERE agent_id=? AND released_at IS NULL", agentID).Scan(&active)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revoking the SSH key did not release the active lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentSSHGitCredentialProtocolKeepsTokenOutOfPersistenceAndRevokes(t *testing.T) {
	app, config, agentID, _, itemID := newSSHAgentFixture(t)
	defer app.Close()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	revoked := make(chan struct{}, 1)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/app/installations/1/access_tokens" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"credential-secret","expires_at":"2099-01-01T00:00:00Z"}`)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/installation/token" {
			revoked <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer github.Close()
	app.server.git = providers.NewGitConnector(providers.GitConnectConfig{GitHubAppID: "42", GitHubPrivateKey: string(privatePEM), GitHubAPIURL: github.URL})
	execution, err := app.server.exec.Claim(t.Context(), agentID, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	primaryClient, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	primary, _ := primaryClient.NewSession()
	primaryInput, _ := primary.StdinPipe()
	if err = primary.Start("projectboard-mcp"); err != nil {
		t.Fatal(err)
	}
	credentialClient, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer credentialClient.Close()
	credentialSession, _ := credentialClient.NewSession()
	input, _ := credentialSession.StdinPipe()
	output, _ := credentialSession.StdoutPipe()
	stderr, _ := credentialSession.StderrPipe()
	if err = credentialSession.Start("projectboard-git-credential " + execution.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(input, "protocol=https\nhost=github.com\n\n")
	_ = input.Close()
	data, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	errorData, _ := io.ReadAll(stderr)
	waitErr := credentialSession.Wait()
	if !strings.Contains(string(data), "password=credential-secret") {
		t.Fatalf("credential protocol output=%q stderr=%q wait=%v", data, errorData, waitErr)
	}
	for _, table := range []string{"activity_events", "conversation_entries", "agent_environment_steps"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE CAST(" + map[string]string{"activity_events": "payload_json", "conversation_entries": "payload_json", "agent_environment_steps": "summary"}[table] + " AS TEXT) LIKE '%credential-secret%'"
		if err = app.server.store.DB.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("credential leaked to %s", table)
		}
	}
	_ = primaryInput.Close()
	_ = primary.Close()
	_ = primaryClient.Close()
	select {
	case <-revoked:
	case <-time.After(2 * time.Second):
		t.Fatal("credential was not revoked after MCP disconnect")
	}
}

func TestAgentSSHRejectsUnknownKey(t *testing.T) {
	app, config, _, _, _ := newSSHAgentFixture(t)
	defer app.Close()
	_, unknownPrivate, _ := ed25519.GenerateKey(rand.Reader)
	unknownSigner, _ := ssh.NewSignerFromKey(unknownPrivate)
	config.Auth = []ssh.AuthMethod{ssh.PublicKeys(unknownSigner)}
	if client, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), config); err == nil {
		client.Close()
		t.Fatal("unknown Agent SSH key was accepted")
	}
}

func TestAgentSSHRejectsShellPTYAndForwarding(t *testing.T) {
	app, config, _, _, _ := newSSHAgentFixture(t)
	defer app.Close()
	client, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.RequestPty("xterm", 80, 24, ssh.TerminalModes{}); err == nil {
		t.Fatal("PTY request was accepted")
	}
	_ = session.Close()
	session, err = client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Shell(); err == nil {
		t.Fatal("shell request was accepted")
	}
	_ = session.Close()
	if connection, err := client.Dial("tcp", "127.0.0.1:1"); err == nil {
		connection.Close()
		t.Fatal("SSH port forwarding was accepted")
	}
}

func TestAgentSSHNetworkDisconnectRetainsLeaseForResume(t *testing.T) {
	app, config, agentID, _, itemID := newSSHAgentFixture(t)
	defer app.Close()
	execution, err := app.server.exec.Claim(t.Context(), agentID, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ssh.Dial("tcp", app.server.ssh.listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Start("projectboard-mcp"); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.server.ssh.mu.Lock()
		active := app.server.ssh.primary[agentID]
		app.server.ssh.mu.Unlock()
		if active == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected SSH session was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var leaseActive int
	if err = app.server.store.DB.QueryRow("SELECT 1 FROM leases WHERE id=? AND released_at IS NULL", execution.LeaseID).Scan(&leaseActive); err != nil {
		t.Fatalf("ordinary disconnect released the lease: %v", err)
	}
	resumed, err := app.server.exec.Active(t.Context(), agentID)
	if err != nil || resumed == nil || resumed.ID != execution.ID {
		t.Fatalf("execution was not resumable: %#v, %v", resumed, err)
	}
}

func newSSHAgentFixture(t *testing.T) (*Application, *ssh.ClientConfig, string, string, string) {
	t.Helper()
	app, err := New(Config{DataDir: t.TempDir(), BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123", SSHListenAddress: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StartSSH(); err != nil {
		app.Close()
		t.Fatal(err)
	}
	agentID, projectID, stamp := security.Token(18), security.Token(18), now()
	if _, err := app.server.store.DB.Exec("INSERT INTO agents(id,name,purpose,status,created_at) VALUES(?,?,'tests','active',?)", agentID, "ssh-agent-"+agentID[:6], stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := app.server.store.DB.Exec(`INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES(?,'ssh','SSH','https://github.com/acme/ssh.git',?,?)`, projectID, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := app.server.store.DB.Exec("INSERT INTO agent_project_grants(agent_id,project_id,allow_unassigned_claim,created_at) VALUES(?,?,0,?)", agentID, projectID, stamp); err != nil {
		t.Fatal(err)
	}
	authorizationID := security.Token(18)
	if _, err := app.server.store.DB.Exec(`INSERT INTO provider_authorizations(id,provider,name,base_url,installation_id,encrypted_secret,status,created_at,updated_at)
		VALUES(?,'github','test','https://github.com','1','not-used','active',?,?)`, authorizationID, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := app.server.store.DB.Exec(`INSERT INTO project_repository_grants(id,project_id,authorization_id,repository_id,repository_name,clone_url,default_branch,access_level,approved_by,approved_at)
		VALUES(?,?,?,'1','acme/ssh','https://github.com/acme/ssh.git','main','write','admin',?)`, security.Token(18), projectID, authorizationID, stamp); err != nil {
		t.Fatal(err)
	}
	item, err := app.server.queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "admin"}, workqueue.CreateInput{ProjectID: projectID, Title: "SSH task", AcceptanceCriteriaMarkdown: "done", AssigneeKind: "agent", AssigneeID: agentID})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	privateSigner, _ := ssh.NewSignerFromKey(privateKey)
	sshPublic, _ := ssh.NewPublicKey(publicKey)
	keyID := security.Token(18)
	if _, err := app.server.store.DB.Exec("INSERT INTO agent_ssh_keys(id,agent_id,label,public_key,fingerprint,created_at) VALUES(?,?,?,?,?,?)", keyID, agentID, "test", string(ssh.MarshalAuthorizedKey(sshPublic)), ssh.FingerprintSHA256(sshPublic), stamp); err != nil {
		t.Fatal(err)
	}
	clientConfig := &ssh.ClientConfig{User: agentID, Auth: []ssh.AuthMethod{ssh.PublicKeys(privateSigner)}, HostKeyCallback: ssh.FixedHostKey(app.server.ssh.hostKey), Timeout: 3 * time.Second}
	return app, clientConfig, agentID, keyID, item.ID
}
