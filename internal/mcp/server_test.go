package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newMCP(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(core.New(st, config.Defaults()))
}

func connectMCP(t *testing.T, s *Server) (*sdk.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := s.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "tally-test", Version: "0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return clientSession, func() { _ = clientSession.Close(); _ = serverSession.Close() }
}

func TestOfficialSDKToolsList(t *testing.T) {
	cs, cleanup := connectMCP(t, newMCP(t))
	defer cleanup()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) < 24 {
		t.Fatalf("expected full parity tool set, got %d", len(res.Tools))
	}
	seen := map[string]bool{}
	for _, tool := range res.Tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{"add_work_item", "update_work_item", "set_billing_profile", "finalize_report", "add_project", "sync", "report"} {
		if !seen[name] {
			t.Fatalf("missing tool %s", name)
		}
	}
}

func TestOfficialSDKToolCallParity(t *testing.T) {
	cs, cleanup := connectMCP(t, newMCP(t))
	defer cleanup()
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "add_work_item", Arguments: map[string]any{"name": "Alpha", "kind": "project", "context": "ACME"}})
	if err != nil || res.IsError {
		t.Fatalf("add_work_item err=%v result=%+v", err, res)
	}
	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "add_rule", Arguments: map[string]any{"project": "Alpha", "field": "app", "pattern": "Code"}})
	if err != nil || res.IsError {
		t.Fatalf("add_rule err=%v result=%+v", err, res)
	}
	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_work_items", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list_work_items err=%v result=%+v", err, res)
	}
	if !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "Alpha") {
		t.Fatalf("list missing Alpha: %+v", res.Content)
	}
}

func TestOfficialSDKToolErrorIsToolError(t *testing.T) {
	cs, cleanup := connectMCP(t, newMCP(t))
	defer cleanup()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "mark_work_item_done", Arguments: map[string]any{"work_item": "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got %+v", res)
	}
}

func TestReportResolverCustomTimezoneAndFinalPeriod(t *testing.T) {
	s := newMCP(t)
	rep, mode, timezone, err := s.resolveReport(map[string]any{"period": "custom", "from": "2026-03-08", "to": "2026-03-08", "timezone": "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	if mode != model.PeriodCustom || timezone != "America/New_York" || rep.To.Sub(rep.From) != 23*time.Hour {
		t.Fatalf("custom report mode=%s timezone=%s range=%s", mode, timezone, rep.To.Sub(rep.From))
	}
	item, err := s.app.AddWorkItem("Final item", model.KindProduct, "LMS", "")
	if err != nil {
		t.Fatal(err)
	}
	rep, mode, _, err = s.resolveReport(map[string]any{"period": "final", "item": "Final item", "timezone": "UTC", "billing": true})
	if err != nil || mode != model.PeriodFinal || rep.From.Before(item.CreatedAt.Add(-time.Second)) || !rep.To.After(rep.From) {
		t.Fatalf("final report=%+v mode=%s err=%v", rep, mode, err)
	}
	if _, _, _, err := s.resolveReport(map[string]any{"period": "custom", "from": "2026-01-01", "timezone": "UTC"}); err == nil {
		t.Fatal("expected incomplete custom range error")
	}
	if _, _, _, err := s.resolveReport(map[string]any{"period": "weekly", "timezone": "Not/A_Zone"}); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}

func TestOfficialSDKFinalizeReportUsesParityResolver(t *testing.T) {
	s := newMCP(t)
	cs, cleanup := connectMCP(t, s)
	defer cleanup()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "finalize_report", Arguments: map[string]any{"label": "DST day", "period": "custom", "from": "2026-03-08", "to": "2026-03-08", "timezone": "America/New_York", "billing": true}})
	if err != nil || res.IsError {
		t.Fatalf("finalize err=%v result=%+v", err, res)
	}
	snapshots, err := s.app.Store.ListReportSnapshots()
	if err != nil || len(snapshots) != 1 || snapshots[0].PeriodMode != model.PeriodCustom || snapshots[0].Timezone != "America/New_York" {
		t.Fatalf("snapshots=%+v err=%v", snapshots, err)
	}
}

func TestUpdateWorkItemCanClearOptionalTextFields(t *testing.T) {
	s := newMCP(t)
	item, err := s.app.AddWorkItem("Clearable", model.KindProject, "ACME", "description")
	if err != nil {
		t.Fatal(err)
	}
	cs, cleanup := connectMCP(t, s)
	defer cleanup()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "update_work_item", Arguments: map[string]any{"id": item.ID, "context": "", "description": ""}})
	if err != nil || res.IsError {
		t.Fatalf("update err=%v result=%+v", err, res)
	}
	got, err := s.app.Store.GetWorkItem(item.ID)
	if err != nil || got.Context != "" || got.Description != "" {
		t.Fatalf("cleared item=%+v err=%v", got, err)
	}
}

func TestSetBillingProfilePreservesOmittedFields(t *testing.T) {
	s := newMCP(t)
	item, _ := s.app.AddWorkItem("Billing", model.KindProject, "", "")
	original := model.DefaultBillingProfile()
	original.ScopeType = model.BillingScopeWorkItem
	original.ScopeKey = fmt.Sprint(item.ID)
	original.Enabled = true
	original.Currency = "EUR"
	original.HourlyRateMinor = 27500
	original.RoundingIncrementMinutes = 30
	original.PeriodMode = model.PeriodMonthly
	if _, err := s.app.SetBillingProfile(original); err != nil {
		t.Fatal(err)
	}
	cs, cleanup := connectMCP(t, s)
	defer cleanup()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "set_billing_profile", Arguments: map[string]any{"scope_type": "work_item", "scope_key": fmt.Sprint(item.ID), "enabled": false}})
	if err != nil || res.IsError {
		t.Fatalf("set billing err=%v result=%+v", err, res)
	}
	got, err := s.app.Store.GetBillingProfile(model.BillingScopeWorkItem, fmt.Sprint(item.ID))
	if err != nil || got.Enabled || got.Currency != "EUR" || got.HourlyRateMinor != 27500 || got.RoundingIncrementMinutes != 30 || got.PeriodMode != model.PeriodMonthly {
		t.Fatalf("partial MCP billing=%+v err=%v", got, err)
	}
}

func TestToolsPublishSafetyAnnotations(t *testing.T) {
	cs, cleanup := connectMCP(t, newMCP(t))
	defer cleanup()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*sdk.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	if tools["status"].Annotations == nil || !tools["status"].Annotations.ReadOnlyHint {
		t.Fatalf("status annotations=%+v", tools["status"].Annotations)
	}
	if tools["delete_rule"].Annotations == nil || tools["delete_rule"].Annotations.DestructiveHint == nil || !*tools["delete_rule"].Annotations.DestructiveHint {
		t.Fatalf("delete_rule annotations=%+v", tools["delete_rule"].Annotations)
	}
	if tools["set_billing_profile"].Annotations == nil || !tools["set_billing_profile"].Annotations.IdempotentHint {
		t.Fatalf("set_billing_profile annotations=%+v", tools["set_billing_profile"].Annotations)
	}
}
