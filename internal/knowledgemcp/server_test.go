package knowledgemcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectboard/projectboard/internal/knowledge"
	"github.com/projectboard/projectboard/internal/store"
)

func TestServerSearchesAndReadsOnlyItsBoundProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projectboard.db")
	database, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES
		('p1','one','One','C:/one','now','now'),('p2','two','Two','C:/two','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := knowledge.New(database)
	wanted, _ := module.Create(t.Context(), knowledge.Actor{Type: "human", ID: "u"}, knowledge.CreateInput{ProjectID: "p1", Title: "Release", Summary: "Production release checklist", TriggerDescription: "Use before publishing", Markdown: "Run tests before release."})
	other, _ := module.Create(t.Context(), knowledge.Actor{Type: "human", ID: "u"}, knowledge.CreateInput{ProjectID: "p2", Title: "Secret", Summary: "production secret", Markdown: "must not leak"})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"knowledge_search","arguments":{"query":"production release"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"knowledge_read","arguments":{"nodeIds":["` + wanted.ID + `","` + other.ID + `"]}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err = Serve(t.Context(), path, "p1", strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("responses=%d output=%s", len(lines), output.String())
	}
	var searchResponse map[string]any
	if err = json.Unmarshal([]byte(lines[2]), &searchResponse); err != nil {
		t.Fatal(err)
	}
	searchJSON, _ := json.Marshal(searchResponse)
	if !bytes.Contains(searchJSON, []byte(wanted.ID)) || bytes.Contains(searchJSON, []byte(other.ID)) {
		t.Fatalf("search response leaked project scope: %s", searchJSON)
	}
	var readResponse map[string]any
	if err = json.Unmarshal([]byte(lines[3]), &readResponse); err != nil {
		t.Fatal(err)
	}
	readJSON, _ := json.Marshal(readResponse)
	if !bytes.Contains(readJSON, []byte("Run tests before release")) || bytes.Contains(readJSON, []byte("must not leak")) {
		t.Fatalf("read response leaked project scope: %s", readJSON)
	}
}
