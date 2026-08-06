package mcpstdio

import (
	"bytes"
	"strings"
	"testing"
)

func TestInitializeAndToolDiscovery(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	var output bytes.Buffer
	if err := Run(input, &output, "http://unused", "token"); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "poll_assignments") || !strings.Contains(text, "create_subtask") || !strings.Contains(text, "move_task_stage") || !strings.Contains(text, "projectboard") {
		t.Fatalf("unexpected MCP output: %s", text)
	}
}
