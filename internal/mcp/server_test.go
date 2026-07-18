package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/store"
)

func newMCP(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(core.New(st, config.Defaults()))
}

// roundtrip feeds newline-delimited requests through Serve and returns the
// decoded responses in order.
func roundtrip(t *testing.T, s *Server, reqs ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(reqs, "\n") + "\n")
	var out strings.Builder
	if err := s.Serve(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	var results []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		results = append(results, m)
	}
	return results
}

func TestInitializeAndToolsList(t *testing.T) {
	s := newMCP(t)
	res := roundtrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	// The notification produces no response, so we expect 2 responses.
	if len(res) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(res))
	}
	initRes := res[0]["result"].(map[string]any)
	if initRes["protocolVersion"] != protocolVersion {
		t.Errorf("bad protocol version: %v", initRes["protocolVersion"])
	}
	tools := res[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 12 {
		t.Errorf("expected the full tool set, got %d", len(tools))
	}
}

func TestToolCallParity(t *testing.T) {
	s := newMCP(t)
	res := roundtrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_project","arguments":{"name":"Alpha","type":"billable","client":"ACME"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_rule","arguments":{"project":"Alpha","field":"app","pattern":"Code"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"report","arguments":{"range":"all"}}}`,
	)
	if len(res) != 4 {
		t.Fatalf("expected 4 responses, got %d", len(res))
	}
	// add_project should not be an error and should echo the project name.
	call := res[0]["result"].(map[string]any)
	if call["isError"].(bool) {
		t.Fatalf("add_project errored: %v", call)
	}
	text := call["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Alpha") {
		t.Errorf("expected project echo, got %s", text)
	}
	// list_projects should include Alpha.
	listText := res[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(listText, "Alpha") {
		t.Errorf("list_projects missing Alpha: %s", listText)
	}
}

func TestToolCallError(t *testing.T) {
	s := newMCP(t)
	res := roundtrip(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mark_project_done","arguments":{"project":"nonexistent"}}}`,
	)
	call := res[0]["result"].(map[string]any)
	if !call["isError"].(bool) {
		t.Error("expected isError=true for unknown project")
	}
}
