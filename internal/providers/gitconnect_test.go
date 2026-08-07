package providers

import (
	"context"
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
)

func TestRecentGitHubCommitsTreatsEmptyRepositoryAsNoCommits(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	providerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/77/access_tokens":
			_, _ = io.WriteString(w, `{"token":"installation-token"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/101/commits":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"Git Repository is empty.","status":"409"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerAPI.Close()

	connector := NewGitConnector(GitConnectConfig{
		GitHubAppID:      "42",
		GitHubPrivateKey: string(privatePEM),
		GitHubAPIURL:     providerAPI.URL,
	})
	commits, err := connector.RecentGitHubCommits(context.Background(), "77", "101", "main", "", "")
	if err != nil {
		t.Fatalf("empty repository returned an error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("empty repository returned %d commits", len(commits))
	}
}

func TestExecutionCredentialIsRepositoryScopedAndRevocable(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	revoked := false
	providerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/77/access_tokens":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Error("installation request did not use an App JWT")
			}
			var body struct {
				RepositoryIDs []int64           `json:"repository_ids"`
				Permissions   map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.RepositoryIDs) != 1 || body.RepositoryIDs[0] != 101 || len(body.Permissions) != 1 || body.Permissions["contents"] != "write" {
				t.Errorf("credential was not narrowly scoped: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"short-lived-secret","expires_at":"2026-08-06T12:00:00Z"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			revoked = r.Header.Get("Authorization") == "Bearer short-lived-secret"
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerAPI.Close()
	connector := NewGitConnector(GitConnectConfig{GitHubAppID: "42", GitHubPrivateKey: string(privatePEM), GitHubAPIURL: providerAPI.URL})
	credential, err := connector.IssueExecutionCredential(t.Context(), "77", "101")
	if err != nil || credential.Token != "short-lived-secret" {
		t.Fatalf("credential issue failed: %#v %v", credential, err)
	}
	if err = connector.RevokeExecutionCredential(t.Context(), credential.Token); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("execution credential was not revoked through GitHub")
	}
}
