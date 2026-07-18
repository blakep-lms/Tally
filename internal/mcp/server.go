// Package mcp exposes Tally's core service as a Model Context Protocol server
// over stdio. Every tool here mirrors a CLI/UI capability so an agent has the
// same reach as a human operator (PRD F6 — full parity, no read-only crippling).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
)

const protocolVersion = "2024-11-05"

// Server is a minimal MCP stdio server over the core application service.
type Server struct {
	app   *core.App
	tools map[string]tool
	order []string
}

type tool struct {
	desc   string
	schema map[string]any
	handle func(ctx context.Context, args map[string]any) (any, error)
}

// New builds an MCP server bound to app.
func New(app *core.App) *Server {
	s := &Server{app: app, tools: map[string]tool{}}
	s.register()
	return s
}

// Serve runs the JSON-RPC loop reading newline-delimited messages from in and
// writing responses to out until in is exhausted.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		resp, notif := s.dispatch(ctx, req)
		if notif {
			continue // notifications get no response
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "tally", "version": "0.1.0"},
		}
	case "notifications/initialized", "notifications/cancelled":
		return resp, true // notification, no id
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.toolList()}
	case "tools/call":
		resp.Result, resp.Error = s.callTool(ctx, req.Params)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	if req.ID == nil {
		return resp, true
	}
	return resp, false
}

func (s *Server) toolList() []map[string]any {
	out := make([]map[string]any, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		out = append(out, map[string]any{
			"name":        name,
			"description": t.desc,
			"inputSchema": t.schema,
		})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	t, ok := s.tools[call.Name]
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + call.Name}
	}
	if call.Arguments == nil {
		call.Arguments = map[string]any{}
	}
	result, err := t.handle(ctx, call.Arguments)
	if err != nil {
		return toolContent(err.Error(), true), nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return toolContent(string(b), false), nil
}

func toolContent(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}

// --- argument helpers ---------------------------------------------------

func argStr(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}
func argInt(a map[string]any, k string) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func argBool(a map[string]any, k string) bool {
	b, _ := a[k].(bool)
	return b
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func str(desc string) map[string]any   { return map[string]any{"type": "string", "description": desc} }
func boolp(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func intp(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }

func (s *Server) add(name, desc string, schema map[string]any, h func(context.Context, map[string]any) (any, error)) {
	s.tools[name] = tool{desc: desc, schema: schema, handle: h}
	s.order = append(s.order, name)
}

// --- tools --------------------------------------------------------------

func (s *Server) register() {
	s.add("status", "System snapshot: provider connectivity, project/rule counts, tracked hours.",
		obj(map[string]any{}), func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.Status(ctx)
		})

	s.add("list_projects", "List projects. Optional status filter: active | done.",
		obj(map[string]any{"status": str("active, done, or empty for all")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.ListProjects(model.ProjectStatus(argStr(a, "status")))
		})

	s.add("add_project", "Create a project (type billable | internal).",
		obj(map[string]any{"name": str("project name"), "type": str("billable or internal"), "client": str("optional client label")}, "name", "type"),
		func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.AddProject(argStr(a, "name"), model.ProjectType(argStr(a, "type")), argStr(a, "client"))
		})

	s.add("mark_project_done", "Archive a project by id or name; its rules deactivate, history is kept.",
		obj(map[string]any{"project": str("project id or name")}, "project"),
		func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.MarkDone(argStr(a, "project"))
		})

	s.add("list_rules", "List classification rules. Set active_only to limit to live rules.",
		obj(map[string]any{"active_only": boolp("only active rules of active projects")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.ListRules(argBool(a, "active_only"))
		})

	s.add("add_rule", "Add a rule mapping events to a project. field: app|title|url|repo; match: contains|equals|regex.",
		obj(map[string]any{
			"project": str("project id or name"), "field": str("app, title, url, or repo"),
			"match": str("contains, equals, or regex"), "pattern": str("pattern to match"),
			"priority": intp("lower = evaluated first (default 100)"),
		}, "project", "field", "pattern"),
		func(ctx context.Context, a map[string]any) (any, error) {
			match := argStr(a, "match")
			if match == "" {
				match = string(model.MatchContains)
			}
			return s.app.AddRule(argStr(a, "project"), model.RuleField(argStr(a, "field")),
				model.MatchKind(match), argStr(a, "pattern"), argInt(a, "priority"))
		})

	s.add("test_rule", "Dry-run a rule against captured events; returns the events it would match.",
		obj(map[string]any{
			"field": str("app, title, url, or repo"), "match": str("contains, equals, or regex"),
			"pattern": str("pattern"), "limit": intp("max events to return"),
		}, "field", "pattern"),
		func(ctx context.Context, a map[string]any) (any, error) {
			match := argStr(a, "match")
			if match == "" {
				match = string(model.MatchContains)
			}
			limit := argInt(a, "limit")
			if limit == 0 {
				limit = 25
			}
			return s.app.TestRule(model.RuleField(argStr(a, "field")), model.MatchKind(match), argStr(a, "pattern"), limit)
		})

	s.add("delete_rule", "Delete a rule by id.",
		obj(map[string]any{"id": intp("rule id")}, "id"),
		func(ctx context.Context, a map[string]any) (any, error) {
			if err := s.app.DeleteRule(int64(argInt(a, "id"))); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": argInt(a, "id")}, nil
		})

	s.add("list_unclassified", "List the unclassified triage queue (most recent first).",
		obj(map[string]any{"limit": intp("max events (default 50)")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			limit := argInt(a, "limit")
			if limit == 0 {
				limit = 50
			}
			return s.app.ListUnclassified(limit)
		})

	s.add("assign_event", "Attribute one event to a project. Optionally make_rule to auto-classify similar events.",
		obj(map[string]any{
			"event_id": intp("event id"), "project": str("project id or name; empty clears it"),
			"make_rule":  boolp("also create a rule from this event"),
			"rule_field": str("field to base the rule on: app|title|url|repo (default title)"),
		}, "event_id"),
		func(ctx context.Context, a map[string]any) (any, error) {
			field := model.RuleField(argStr(a, "rule_field"))
			if field == "" {
				field = model.FieldTitle
			}
			rule, created, err := s.app.AssignEvent(int64(argInt(a, "event_id")), argStr(a, "project"), argBool(a, "make_rule"), field)
			if err != nil {
				return nil, err
			}
			res := map[string]any{"assigned": true, "rule_created": created}
			if created {
				res["rule"] = rule
			}
			return res, nil
		})

	s.add("classify", "Run the rule engine (and optional LLM fallback) over unclassified events.",
		obj(map[string]any{"llm": boolp("use the LLM fallback if enabled")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			return s.app.Classify(ctx, argBool(a, "llm"))
		})

	s.add("sync", "Pull events from the capture provider. Optional RFC3339 from/to; defaults to the last 24h.",
		obj(map[string]any{"from": str("RFC3339 start"), "to": str("RFC3339 end")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			to := time.Now()
			from := to.AddDate(0, 0, -1)
			if v := argStr(a, "from"); v != "" {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					from = t
				}
			}
			if v := argStr(a, "to"); v != "" {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					to = t
				}
			}
			return s.app.Sync(ctx, from, to)
		})

	s.add("report", "Per-project hours report. range: today | week | all, or explicit from/to (YYYY-MM-DD).",
		obj(map[string]any{"range": str("today, week, or all"), "from": str("YYYY-MM-DD"), "to": str("YYYY-MM-DD")}),
		func(ctx context.Context, a map[string]any) (any, error) {
			from, to := rangeWindow(argStr(a, "range"))
			if v := argStr(a, "from"); v != "" {
				if t, err := time.Parse("2006-01-02", v); err == nil {
					from = t
				}
			}
			if v := argStr(a, "to"); v != "" {
				if t, err := time.Parse("2006-01-02", v); err == nil {
					to = t.AddDate(0, 0, 1)
				}
			}
			return s.app.Report(from, to)
		})
}

func rangeWindow(name string) (time.Time, time.Time) {
	now := time.Now()
	switch name {
	case "today":
		return core.TodayRange(now)
	case "all":
		return time.Time{}, now.AddDate(0, 0, 1)
	default:
		return core.WeekRange(now)
	}
}

var _ = fmt.Sprintf
