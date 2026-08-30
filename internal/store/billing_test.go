package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

func billingProfile(scope model.BillingScopeType, key, currency string, rate int64) model.BillingProfile {
	return model.BillingProfile{ScopeType: scope, ScopeKey: key, Enabled: true, Currency: currency, HourlyRateMinor: rate, RoundingMode: model.RoundingUp, RoundingIncrementMinutes: 15, RoundingScope: model.RoundingScopePeriodWorkItem, PeriodMode: model.PeriodMonthly}
}

func TestBillingProfileInheritancePrecedence(t *testing.T) {
	s := openTest(t)
	w, _ := s.CreateWorkItem("Client thing", model.KindProduct, "ACME", "")
	if _, err := s.SetBillingProfile(billingProfile(model.BillingScopeGlobal, "", "USD", 10000)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBillingProfile(billingProfile(model.BillingScopeClient, "ACME", "USD", 20000)); err != nil {
		t.Fatal(err)
	}
	r, err := s.ResolveBillingProfile(w)
	if err != nil || r.InheritedFrom != "client" || r.Profile.HourlyRateMinor != 20000 {
		t.Fatalf("client resolved = %+v err=%v", r, err)
	}
	if _, err := s.SetBillingProfile(billingProfile(model.BillingScopeWorkItem, "1", "EUR", 30000)); err != nil {
		t.Fatal(err)
	}
	r, err = s.ResolveBillingProfile(w)
	if err != nil || r.InheritedFrom != "work_item" || r.Profile.Currency != "EUR" || r.Profile.HourlyRateMinor != 30000 {
		t.Fatalf("work item resolved = %+v err=%v", r, err)
	}
}

func TestBillingProfileValidation(t *testing.T) {
	s := openTest(t)
	if _, err := s.SetBillingProfile(billingProfile(model.BillingScopeWorkItem, "missing", "USD", 100)); err == nil {
		t.Fatal("expected invalid work item key")
	}
	if _, err := s.SetBillingProfile(billingProfile(model.BillingScopeGlobal, "", "US", 100)); err == nil {
		t.Fatal("expected invalid currency")
	}
}

func TestReportSnapshotRoundTripAndList(t *testing.T) {
	s := openTest(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	snapshot, err := s.SaveReportSnapshot(model.ReportSnapshot{Label: "July", PeriodMode: model.PeriodMonthly, From: from, To: to, Timezone: "America/New_York", Payload: json.RawMessage(`{"items":[]}`)})
	if err != nil || snapshot.ID == 0 || snapshot.Label != "July" {
		t.Fatalf("save snapshot = %+v err=%v", snapshot, err)
	}
	list, err := s.ListReportSnapshots()
	if err != nil || len(list) != 1 || string(list[0].Payload) != `{"items":[]}` {
		t.Fatalf("list snapshots = %+v err=%v", list, err)
	}
}

func TestReportSnapshotRequiresAuditMetadata(t *testing.T) {
	s := openTest(t)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	base := model.ReportSnapshot{PeriodMode: model.PeriodMonthly, From: from, To: from.AddDate(0, 1, 0), Timezone: "UTC", Payload: json.RawMessage(`{"items":[]}`)}
	if _, err := s.SaveReportSnapshot(base); err == nil {
		t.Fatal("expected missing label error")
	}
	base.Label = "July"
	base.Timezone = "Not/A_Zone"
	if _, err := s.SaveReportSnapshot(base); err == nil {
		t.Fatal("expected invalid timezone error")
	}
	base.Timezone = "UTC"
	base.Payload = json.RawMessage(`{`)
	if _, err := s.SaveReportSnapshot(base); err == nil {
		t.Fatal("expected invalid payload error")
	}
}

func TestHoursByWorkItemReportsAllGenericKinds(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	kinds := []model.WorkItemKind{model.KindProject, model.KindProduct, model.KindGoal, model.KindOther}
	for i, kind := range kinds {
		w, _ := s.CreateWorkItem(string(kind), kind, "", "")
		if _, err := s.UpsertEvent(model.Event{Start: base, Duration: float64((i + 1) * 60), WorkItemID: &w.ID, SourceKey: string(kind)}); err != nil {
			t.Fatal(err)
		}
	}
	hours, err := s.HoursByWorkItem(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[model.WorkItemKind]bool{}
	for _, line := range hours {
		seen[line.WorkItem.Kind] = true
	}
	for _, kind := range kinds {
		if !seen[kind] {
			t.Fatalf("missing kind %s in %+v", kind, hours)
		}
	}
}

func TestReportWindowClipsOverlapsAndAggregatesInOnePass(t *testing.T) {
	s := openTest(t)
	item, _ := s.CreateWorkItem("Window", model.KindProject, "", "")
	from := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	_, _ = s.UpsertEvent(model.Event{Start: from.Add(-time.Minute), Duration: 120, WorkItemID: &item.ID, SourceKey: "overlap-before"})
	_, _ = s.UpsertEvent(model.Event{Start: from.Add(2 * time.Minute), Duration: 30, SourceKey: "unclassified-inside"})
	_, _ = s.UpsertEvent(model.Event{Start: to.Add(time.Minute), Duration: 60, WorkItemID: &item.ID, SourceKey: "outside"})
	hours, unclassified, total, err := s.ReportWindow(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].Seconds != 60 || unclassified != 30 || total != 90 {
		t.Fatalf("window hours=%+v unclassified=%v total=%v", hours, unclassified, total)
	}
}
