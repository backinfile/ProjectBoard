package server

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/projectboard/projectboard/internal/security"
	"golang.org/x/crypto/ssh"
)

func (s *Server) listAgentSSHKeys(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	agentID := r.PathValue("id")
	rows, err := s.store.DB.Query(`SELECT k.id,k.label,k.public_key,k.fingerprint,k.created_at,k.last_used_at,k.revoked_at,
		CASE WHEN EXISTS(SELECT 1 FROM agent_ssh_sessions x WHERE x.key_id=k.id AND x.disconnected_at IS NULL) THEN 1 ELSE 0 END
		FROM agent_ssh_keys k WHERE k.agent_id=? ORDER BY k.created_at DESC`, agentID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, label, publicKey, fingerprint, created string
		var lastUsed, revoked *string
		var connected int
		if err := rows.Scan(&id, &label, &publicKey, &fingerprint, &created, &lastUsed, &revoked, &connected); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "label": label, "publicKey": publicKey, "fingerprint": fingerprint, "createdAt": created, "lastUsedAt": lastUsed, "revokedAt": revoked, "connected": connected != 0})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addAgentSSHKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var input struct {
		Label     string `json:"label"`
		PublicKey string `json:"publicKey"`
	}
	decode(r, &input)
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" || len(input.Label) > 120 {
		writeError(w, &domainError{422, "SSH_KEY_LABEL_REQUIRED", "SSH key label must be 1-120 characters", nil})
		return
	}
	publicKey, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(input.PublicKey)))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		writeError(w, &domainError{422, "SSH_PUBLIC_KEY_INVALID", "Enter one valid OpenSSH public key", nil})
		return
	}
	if publicKey.Type() != ssh.KeyAlgoED25519 {
		writeError(w, &domainError{422, "SSH_KEY_TYPE_UNSUPPORTED", "ProjectBoard accepts Ed25519 Agent keys only", nil})
		return
	}
	var agentExists int
	if s.store.DB.QueryRow("SELECT 1 FROM agents WHERE id=? AND revoked_at IS NULL", r.PathValue("id")).Scan(&agentExists) != nil {
		writeError(w, &domainError{404, "AGENT_NOT_FOUND", "Agent was not found", nil})
		return
	}
	id, stamp := security.Token(18), now()
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	fingerprint := ssh.FingerprintSHA256(publicKey)
	_, err = s.store.DB.Exec("INSERT INTO agent_ssh_keys(id,agent_id,label,public_key,fingerprint,created_at) VALUES(?,?,?,?,?,?)", id, r.PathValue("id"), input.Label, normalized, fingerprint, stamp)
	if err != nil {
		writeError(w, &domainError{409, "SSH_KEY_ALREADY_REGISTERED", "This SSH public key is already registered", nil})
		return
	}
	s.audit(a, "agent.ssh_key_added", "agent_ssh_key", id, "", map[string]any{"agentId": r.PathValue("id"), "fingerprint": fingerprint, "label": input.Label})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "label": input.Label, "publicKey": normalized, "fingerprint": fingerprint, "createdAt": stamp, "connected": false})
}

func (s *Server) deleteAgentSSHKey(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	stamp := now()
	result, err := s.store.DB.Exec("UPDATE agent_ssh_keys SET revoked_at=? WHERE id=? AND agent_id=? AND revoked_at IS NULL", stamp, r.PathValue("keyId"), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count != 1 {
		writeError(w, &domainError{404, "SSH_KEY_NOT_FOUND", "Active SSH key was not found", nil})
		return
	}
	if s.ssh != nil {
		s.ssh.disconnectKey(r.PathValue("id"), r.PathValue("keyId"), "ssh_key_revoked", true)
	}
	s.audit(a, "agent.ssh_key_revoked", "agent_ssh_key", r.PathValue("keyId"), "", map[string]any{"agentId": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) disconnectAgentSSH(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	if s.ssh != nil {
		s.ssh.disconnectAgent(r.PathValue("id"), "administrator_disconnected", false)
	}
	s.audit(a, "agent.ssh_disconnected", "agent", r.PathValue("id"), "", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"disconnected": true})
}

func (s *Server) getSSHSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	host := strings.TrimSpace(s.config.SSHPublicHost)
	if host == "" && s.config.GitConnect.PublicURL != "" {
		if parsed, err := url.Parse(s.config.GitConnect.PublicURL); err == nil {
			host = parsed.Hostname()
		}
	}
	if host == "" {
		host = strings.Split(r.Host, ":")[0]
	}
	if s.ssh == nil {
		writeError(w, &domainError{503, "SSH_NOT_RUNNING", "Agent SSH service is not running", nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"host": host, "port": s.config.SSHPublicPort, "hostKey": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.ssh.hostKey))), "hostKeyFingerprint": s.ssh.hostFingerprint(), "command": fmt.Sprintf("ssh -T -p %s <agent-id>@%s projectboard-mcp", s.config.SSHPublicPort, host)})
}

func (s *Server) serveAgentGuide(w http.ResponseWriter, _ *http.Request) {
	data, err := fs.ReadFile(s.static, "agent-execution.md")
	if err != nil {
		http.Error(w, "guide not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=projectboard-agent-execution.md")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
