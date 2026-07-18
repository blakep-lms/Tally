package core

import (
	"context"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/store"
)

func newApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, config.Defaults())
}

func seed(t *testing.T, a *App) {
	t.Helper()
	now := time.Now()
	events := []model.Event{
		{Start: now, Duration: 3600, App: "Code", Title: "secureai-backend — main", Repo: "secureai-backend", SourceKey: "1"},
		{Start: now, Duration: 1800, App: "iTerm2", Title: "installprosos deploy", SourceKey: "2"},
		{Start: now, Duration: 600, App: "Slack", Title: "team chat", SourceKey: "3"},
	}
	if _, err := a.IngestEvents(events); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyAndReport(t *testing.T) {
	a := newApp(t)
	seed(t, a)

	sec, _ := a.AddProject("SecureAI", model.TypeBillable, "SecureAI Inc")
	ipos, _ := a.AddProject("InstallProsOS", model.TypeBillable, "")
	if _, err := a.AddRule("SecureAI", model.FieldRepo, model.MatchContains, "secureai", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddRule("InstallProsOS", model.FieldTitle, model.MatchContains, "installprosos", 20); err != nil {
		t.Fatal(err)
	}

	res, err := a.Classify(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.MatchedByRule != 2 {
		t.Fatalf("expected 2 matched by rule, got %d", res.MatchedByRule)
	}
	if res.StillUnclassified != 1 {
		t.Fatalf("expected 1 still unclassified, got %d", res.StillUnclassified)
	}

	rep, err := a.Report(time.Time{}, time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Projects) != 2 {
		t.Fatalf("expected 2 projects with hours, got %d", len(rep.Projects))
	}
	if rep.BillableHours != 1.5 { // 3600 + 1800 = 5400s = 1.5h
		t.Errorf("billable hours = %v, want 1.5", rep.BillableHours)
	}
	if rep.UnclassifiedHrs == 0 {
		t.Error("expected some unclassified hours (Slack)")
	}

	// Marking a project done freezes it but keeps its hours in the report.
	if _, err := a.MarkDone("SecureAI"); err != nil {
		t.Fatal(err)
	}
	rep2, _ := a.Report(time.Time{}, time.Now().AddDate(0, 0, 1))
	var found bool
	for _, ph := range rep2.Projects {
		if ph.Project.ID == sec.ID && ph.Hours == 1.0 {
			found = true
		}
	}
	if !found {
		t.Error("archived project hours should remain in historical report")
	}
	_ = ipos
}

func TestManualAssignMakesRule(t *testing.T) {
	a := newApp(t)
	seed(t, a)
	a.AddProject("Internal", model.TypeInternal, "")

	un, _ := a.ListUnclassified(0)
	if len(un) != 3 {
		t.Fatalf("expected 3 unclassified before rules, got %d", len(un))
	}
	// Find the Slack event and assign it, generating an app rule.
	var slackID int64
	for _, e := range un {
		if e.App == "Slack" {
			slackID = e.ID
		}
	}
	rule, created, err := a.AssignEvent(slackID, "Internal", true, model.FieldApp)
	if err != nil || !created {
		t.Fatalf("expected rule created: created=%v err=%v", created, err)
	}
	if rule.Pattern != "Slack" {
		t.Errorf("rule pattern = %q, want Slack", rule.Pattern)
	}

	un2, _ := a.ListUnclassified(0)
	if len(un2) != 2 {
		t.Fatalf("Slack event should now be classified, %d remain", len(un2))
	}
}

func TestWeekRangeMonday(t *testing.T) {
	// 2026-07-18 is a Saturday; the Monday-anchored week starts 2026-07-13.
	sat := time.Date(2026, 7, 18, 15, 0, 0, 0, time.UTC)
	from, to := WeekRange(sat)
	if from.Weekday() != time.Monday {
		t.Errorf("week should start Monday, got %s", from.Weekday())
	}
	if to.Sub(from) != 7*24*time.Hour {
		t.Errorf("week should span 7 days")
	}
	if from.Day() != 13 {
		t.Errorf("expected week start on the 13th, got %d", from.Day())
	}
}
