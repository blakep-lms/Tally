// Package mcp exposes Tally's core service through the official MCP Go SDK.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/report"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the official SDK server and registers every mutating/read CLI capability as tools.
type Server struct {
	app *core.App
	sdk *sdk.Server
}

func New(app *core.App) *Server {
	s := &Server{app: app, sdk: sdk.NewServer(&sdk.Implementation{Name: "tally", Version: "0.1.0"}, nil)}
	s.register()
	return s
}

// SDK exposes the official MCP server for tests/advanced embedders.
func (s *Server) SDK() *sdk.Server { return s.sdk }

// Serve runs stdio MCP using the official SDK transport.
func (s *Server) Serve(ctx context.Context) error { return s.sdk.Run(ctx, &sdk.StdioTransport{}) }

type toolSpec struct {
	name, desc string
	schema     map[string]any
	handle     func(context.Context, map[string]any) (any, error)
}

func (s *Server) add(spec toolSpec) {
	s.sdk.AddTool(&sdk.Tool{Name: spec.name, Description: spec.desc, InputSchema: spec.schema, Annotations: toolAnnotations(spec.name)}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return textResult("invalid arguments: "+err.Error(), true), nil
			}
		}
		out, err := spec.handle(ctx, args)
		if err != nil {
			return textResult(err.Error(), true), nil
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, StructuredContent: out}, nil
	})
}

func toolAnnotations(name string) *sdk.ToolAnnotations {
	readOnly := map[string]bool{
		"status": true, "list_work_items": true, "list_projects": true,
		"list_rules": true, "test_rule": true, "list_unclassified": true,
		"report": true, "get_billing_profile": true, "resolve_billing_profile": true,
		"list_report_snapshots": true, "get_report_snapshot": true,
	}[name]
	destructive := name == "delete_rule"
	openWorld := name == "sync" || name == "classify"
	idempotent := map[string]bool{
		"update_work_item": true, "mark_work_item_done": true,
		"reactivate_work_item": true, "mark_project_done": true,
		"sync": true, "classify": true, "set_billing_profile": true,
	}[name]
	return &sdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: idempotent, OpenWorldHint: &openWorld}
}

func textResult(text string, isErr bool) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}, IsError: isErr}
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
func argStr(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}
func argBool(a map[string]any, k string) bool { v, _ := a[k].(bool); return v }
func argInt(a map[string]any, k string) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func argInt64(a map[string]any, k string) int64 { return int64(argInt(a, k)) }

func (s *Server) register() {
	s.add(toolSpec{"status", "System snapshot and provider connectivity.", obj(map[string]any{}), func(ctx context.Context, a map[string]any) (any, error) { return s.app.Status(ctx) }})
	s.add(toolSpec{"list_work_items", "List work items; optional status active|done.", obj(map[string]any{"status": str("active, done, or empty")}), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.ListWorkItems(model.WorkItemStatus(argStr(a, "status")))
	}})
	s.add(toolSpec{"add_work_item", "Create a work item.", obj(map[string]any{"name": str("name"), "kind": str("project|product|goal|other"), "context": str("client/product context"), "description": str("description")}, "name", "kind"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.AddWorkItem(argStr(a, "name"), model.WorkItemKind(argStr(a, "kind")), argStr(a, "context"), argStr(a, "description"))
	}})
	s.add(toolSpec{"update_work_item", "Update a work item by id.", obj(map[string]any{"id": intp("id"), "name": str("name"), "kind": str("kind"), "context": str("context"), "description": str("description")}, "id"), func(ctx context.Context, a map[string]any) (any, error) {
		cur, err := s.app.Store.GetWorkItem(argInt64(a, "id"))
		if err != nil {
			return nil, err
		}
		name, kind, context, desc := cur.Name, string(cur.Kind), cur.Context, cur.Description
		if v, ok := a["name"].(string); ok {
			name = v
		}
		if v, ok := a["kind"].(string); ok {
			kind = v
		}
		if v, ok := a["context"].(string); ok {
			context = v
		}
		if v, ok := a["description"].(string); ok {
			desc = v
		}
		return s.app.UpdateWorkItem(cur.ID, name, model.WorkItemKind(kind), context, desc)
	}})
	s.add(toolSpec{"mark_work_item_done", "Archive a work item by id or name.", obj(map[string]any{"work_item": str("id or name")}, "work_item"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.MarkWorkItemDone(argStr(a, "work_item"))
	}})
	s.add(toolSpec{"reactivate_work_item", "Reactivate a done work item.", obj(map[string]any{"work_item": str("id or name")}, "work_item"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.ReactivateWorkItem(argStr(a, "work_item"))
	}})

	// Legacy project parity aliases.
	s.add(toolSpec{"list_projects", "List legacy project view.", obj(map[string]any{"status": str("active, done, or empty")}), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.ListProjects(model.ProjectStatus(argStr(a, "status")))
	}})
	s.add(toolSpec{"add_project", "Create project compatibility item.", obj(map[string]any{"name": str("name"), "type": str("billable|internal"), "client": str("client")}, "name", "type"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.AddProject(argStr(a, "name"), model.ProjectType(argStr(a, "type")), argStr(a, "client"))
	}})
	s.add(toolSpec{"mark_project_done", "Archive project by id/name.", obj(map[string]any{"project": str("id or name")}, "project"), func(ctx context.Context, a map[string]any) (any, error) { return s.app.MarkDone(argStr(a, "project")) }})

	s.add(toolSpec{"list_rules", "List classification rules.", obj(map[string]any{"active_only": boolp("only active")}), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.ListRules(argBool(a, "active_only"))
	}})
	s.add(toolSpec{"add_rule", "Add rule field app|title|url|repo match contains|equals|regex.", obj(map[string]any{"project": str("work item id/name"), "field": str("field"), "match": str("match"), "pattern": str("pattern"), "priority": intp("priority")}, "project", "field", "pattern"), func(ctx context.Context, a map[string]any) (any, error) {
		match := argStr(a, "match")
		if match == "" {
			match = string(model.MatchContains)
		}
		return s.app.AddRule(argStr(a, "project"), model.RuleField(argStr(a, "field")), model.MatchKind(match), argStr(a, "pattern"), argInt(a, "priority"))
	}})
	s.add(toolSpec{"test_rule", "Dry-run a rule.", obj(map[string]any{"field": str("field"), "match": str("match"), "pattern": str("pattern"), "limit": intp("limit")}, "field", "pattern"), func(ctx context.Context, a map[string]any) (any, error) {
		limit := argInt(a, "limit")
		if limit == 0 {
			limit = 25
		}
		match := argStr(a, "match")
		if match == "" {
			match = string(model.MatchContains)
		}
		return s.app.TestRule(model.RuleField(argStr(a, "field")), model.MatchKind(match), argStr(a, "pattern"), limit)
	}})
	s.add(toolSpec{"delete_rule", "Delete a rule.", obj(map[string]any{"id": intp("rule id")}, "id"), func(ctx context.Context, a map[string]any) (any, error) {
		return map[string]any{"deleted": argInt(a, "id")}, s.app.DeleteRule(argInt64(a, "id"))
	}})
	s.add(toolSpec{"list_unclassified", "List unclassified events.", obj(map[string]any{"limit": intp("limit")}), func(ctx context.Context, a map[string]any) (any, error) {
		limit := argInt(a, "limit")
		if limit == 0 {
			limit = 50
		}
		return s.app.ListUnclassified(limit)
	}})
	s.add(toolSpec{"assign_event", "Assign event to work item and optionally make rule.", obj(map[string]any{"event_id": intp("event id"), "project": str("work item"), "make_rule": boolp("make rule"), "rule_field": str("rule field")}, "event_id"), func(ctx context.Context, a map[string]any) (any, error) {
		field := model.RuleField(argStr(a, "rule_field"))
		if field == "" {
			field = model.FieldTitle
		}
		rule, created, err := s.app.AssignEvent(argInt64(a, "event_id"), argStr(a, "project"), argBool(a, "make_rule"), field)
		res := map[string]any{"assigned": err == nil, "rule_created": created}
		if created {
			res["rule"] = rule
		}
		return res, err
	}})
	s.add(toolSpec{"classify", "Run rule engine and optional LLM.", obj(map[string]any{"llm": boolp("use llm")}), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.Classify(ctx, argBool(a, "llm"))
	}})
	s.add(toolSpec{"sync", "Pull captured events. Optional RFC3339 from/to.", obj(map[string]any{"from": str("RFC3339"), "to": str("RFC3339")}), func(ctx context.Context, a map[string]any) (any, error) {
		to := time.Now()
		from := to.AddDate(0, 0, -1)
		if v := argStr(a, "from"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Errorf("invalid from: %w", err)
			}
			from = t
		}
		if v := argStr(a, "to"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Errorf("invalid to: %w", err)
			}
			to = t
		}
		if !to.After(from) {
			return nil, errors.New("to must be after from")
		}
		return s.app.Sync(ctx, from, to)
	}})
	reportSchema := obj(map[string]any{"range": str("today|week|month|all"), "period": str("weekly|biweekly|semimonthly|monthly|final|custom"), "timezone": str("IANA timezone"), "item": str("work item ID/name for final"), "from": str("YYYY-MM-DD"), "to": str("YYYY-MM-DD inclusive"), "billing": boolp("include billing")})
	s.add(toolSpec{"report", "Exact work-item report with optional billing projection and deterministic period boundaries.", reportSchema, func(ctx context.Context, a map[string]any) (any, error) {
		rep, _, _, err := s.resolveReport(a)
		return rep, err
	}})
	s.add(toolSpec{"set_billing_profile", "Set global/client/work-item billing profile.", obj(map[string]any{"scope_type": str("global|client|work_item"), "scope_key": str("key"), "enabled": boolp("enabled"), "currency": str("USD"), "hourly_rate_minor": intp("rate"), "rounding_increment_minutes": intp("minutes"), "period_mode": str("period")}, "scope_type"), func(ctx context.Context, a map[string]any) (any, error) {
		patch := model.BillingProfilePatch{ScopeType: model.BillingScopeType(argStr(a, "scope_type")), ScopeKey: argStr(a, "scope_key")}
		if v, ok := a["enabled"].(bool); ok {
			patch.Enabled = &v
		}
		if v, ok := a["currency"].(string); ok {
			patch.Currency = &v
		}
		if _, ok := a["hourly_rate_minor"]; ok {
			v := argInt64(a, "hourly_rate_minor")
			patch.HourlyRateMinor = &v
		}
		if _, ok := a["rounding_increment_minutes"]; ok {
			v := argInt(a, "rounding_increment_minutes")
			patch.RoundingIncrementMinutes = &v
		}
		if v, ok := a["period_mode"].(string); ok {
			mode := model.PeriodMode(v)
			patch.PeriodMode = &mode
		}
		return s.app.PatchBillingProfile(patch)
	}})
	s.add(toolSpec{"get_billing_profile", "Get an explicit global/client/work-item billing profile.", obj(map[string]any{"scope_type": str("global|client|work_item"), "scope_key": str("key")}), func(ctx context.Context, a map[string]any) (any, error) {
		scope := model.BillingScopeType(argStr(a, "scope_type"))
		if scope == "" {
			scope = model.BillingScopeGlobal
		}
		return s.app.Store.GetBillingProfile(scope, argStr(a, "scope_key"))
	}})
	s.add(toolSpec{"resolve_billing_profile", "Resolve effective billing profile for work item.", obj(map[string]any{"work_item": str("id/name")}, "work_item"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.ResolveBillingProfile(argStr(a, "work_item"))
	}})
	s.add(toolSpec{"list_report_snapshots", "List finalized report snapshots.", obj(map[string]any{}), func(ctx context.Context, a map[string]any) (any, error) { return s.app.Store.ListReportSnapshots() }})
	s.add(toolSpec{"get_report_snapshot", "Get a finalized report snapshot.", obj(map[string]any{"id": intp("snapshot id")}, "id"), func(ctx context.Context, a map[string]any) (any, error) {
		return s.app.Store.GetReportSnapshot(argInt64(a, "id"))
	}})
	s.add(toolSpec{"finalize_report", "Freeze an immutable report snapshot.", obj(map[string]any{"range": str("today|week|month|all"), "label": str("label"), "period": str("weekly|biweekly|semimonthly|monthly|final|custom"), "timezone": str("IANA timezone"), "item": str("work item ID/name for final"), "from": str("YYYY-MM-DD"), "to": str("YYYY-MM-DD inclusive"), "billing": boolp("include billing")}, "label"), func(ctx context.Context, a map[string]any) (any, error) {
		rep, period, timezone, err := s.resolveReport(a)
		if err != nil {
			return nil, err
		}
		return s.app.FinalizeReport(rep, argStr(a, "label"), period, timezone)
	}})
}

func (s *Server) resolveReport(a map[string]any) (report.Report, model.PeriodMode, string, error) {
	timezone := argStr(a, "timezone")
	if timezone == "" {
		timezone = time.Local.String()
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return report.Report{}, "", timezone, fmt.Errorf("invalid timezone: %w", err)
	}
	now := time.Now().In(loc)
	rangeName := argStr(a, "range")
	if rangeName != "" && rangeName != "today" && rangeName != "week" && rangeName != "month" && rangeName != "all" {
		return report.Report{}, "", timezone, fmt.Errorf("invalid range %q", rangeName)
	}
	mode := model.PeriodMode(argStr(a, "period"))
	if mode == "" {
		switch rangeName {
		case "today", "all":
			mode = model.PeriodCustom
		case "month":
			mode = model.PeriodMonthly
		default:
			mode = model.PeriodWeekly
		}
	}
	if !mode.Valid() {
		return report.Report{}, mode, timezone, fmt.Errorf("invalid period %q", mode)
	}
	from, to := core.PeriodRange(mode, "", now)
	if rangeName == "today" {
		from, to = core.TodayRange(now)
	} else if rangeName == "all" {
		from, to = time.Time{}, now.AddDate(0, 0, 1)
	}
	fromText, toText := argStr(a, "from"), argStr(a, "to")
	if fromText != "" || toText != "" {
		if fromText == "" || toText == "" {
			return report.Report{}, mode, timezone, errors.New("from and to are required together")
		}
		from, err = time.ParseInLocation("2006-01-02", fromText, loc)
		if err != nil {
			return report.Report{}, mode, timezone, fmt.Errorf("invalid from: %w", err)
		}
		endDate, parseErr := time.ParseInLocation("2006-01-02", toText, loc)
		if parseErr != nil {
			return report.Report{}, mode, timezone, fmt.Errorf("invalid to: %w", parseErr)
		}
		to = endDate.AddDate(0, 0, 1)
		mode = model.PeriodCustom
	}
	if mode == model.PeriodCustom && fromText == "" && rangeName != "today" && rangeName != "all" {
		return report.Report{}, mode, timezone, errors.New("custom period requires from and to")
	}
	if mode == model.PeriodFinal {
		itemRef := argStr(a, "item")
		if itemRef == "" {
			return report.Report{}, mode, timezone, errors.New("final period requires item")
		}
		item, resolveErr := s.app.ResolveWorkItem(itemRef)
		if resolveErr != nil {
			return report.Report{}, mode, timezone, resolveErr
		}
		from, to = core.FinalRange(item, now)
		rep, reportErr := s.app.ReportWorkItemWithBilling(item, from, to, argBool(a, "billing"))
		return rep, mode, timezone, reportErr
	}
	if !from.IsZero() && !to.After(from) {
		return report.Report{}, mode, timezone, errors.New("to must be after from")
	}
	rep, err := s.app.ReportWithBilling(from, to, argBool(a, "billing"))
	return rep, mode, timezone, err
}
