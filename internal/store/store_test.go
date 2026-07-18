package store

import (
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProjectLifecycle(t *testing.T) {
	s := openTest(t)
	p, err := s.CreateProject("Client A", model.TypeBillable, "ACME")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != model.StatusActive {
		t.Fatalf("new project should be active")
	}
	// A rule on the project deactivates when the project is marked done.
	r, err := s.CreateRule(model.Rule{ProjectID: p.ID, Field: model.FieldApp, Match: model.MatchContains, Pattern: "Code"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.MarkDone(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != model.StatusDone || done.DoneAt == nil {
		t.Fatalf("expected done with timestamp, got %+v", done)
	}
	got, _ := s.GetRule(r.ID)
	if got.Active {
		t.Error("rule should deactivate when project is done")
	}
	active, _ := s.ListRules(true)
	if len(active) != 0 {
		t.Errorf("no active rules expected, got %d", len(active))
	}
}

func TestEventUpsertDedup(t *testing.T) {
	s := openTest(t)
	e := model.Event{Start: time.Now(), Duration: 100, App: "Code", Title: "x", SourceKey: "aw:w:1"}
	created, err := s.UpsertEvent(e)
	if err != nil || !created {
		t.Fatalf("first upsert should create: created=%v err=%v", created, err)
	}
	e.Duration = 250 // same source key, updated capture
	created, err = s.UpsertEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second upsert with same source key must not create a new row")
	}
	all, _ := s.ListEvents(EventFilter{})
	if len(all) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(all))
	}
	if all[0].Duration != 250 {
		t.Errorf("expected duration updated to 250, got %v", all[0].Duration)
	}
}

func TestHoursByProject(t *testing.T) {
	s := openTest(t)
	p, _ := s.CreateProject("P", model.TypeBillable, "")
	now := time.Now()
	e1 := model.Event{Start: now, Duration: 3600, ProjectID: &p.ID, SourceKey: "a"}
	e2 := model.Event{Start: now, Duration: 1800, ProjectID: &p.ID, SourceKey: "b"}
	e3 := model.Event{Start: now, Duration: 900, SourceKey: "c"} // unclassified
	for _, e := range []model.Event{e1, e2, e3} {
		if _, err := s.UpsertEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	hours, err := s.HoursByProject(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].Hours != 1.5 {
		t.Fatalf("expected 1.5h for project, got %+v", hours)
	}
	un, _ := s.UnclassifiedSeconds(time.Time{}, time.Time{})
	if un != 900 {
		t.Errorf("unclassified seconds = %v, want 900", un)
	}
	total, _ := s.TotalSeconds(time.Time{}, time.Time{})
	if total != 6300 {
		t.Errorf("total seconds = %v, want 6300", total)
	}
}

func TestRegexRuleValidation(t *testing.T) {
	s := openTest(t)
	p, _ := s.CreateProject("P", model.TypeInternal, "")
	_, err := s.CreateRule(model.Rule{ProjectID: p.ID, Field: model.FieldTitle, Match: model.MatchRegex, Pattern: "([bad"})
	if err == nil {
		t.Error("expected invalid regex to be rejected")
	}
}
