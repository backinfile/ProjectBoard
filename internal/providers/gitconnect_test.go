package providers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
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
