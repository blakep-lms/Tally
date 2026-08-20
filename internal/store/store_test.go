package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/model"
	_ "modernc.org/sqlite"
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

func TestWorkItemKindsAndLifecycle(t *testing.T) {
	s := openTest(t)
	for _, k := range []model.WorkItemKind{model.KindProject, model.KindProduct, model.KindGoal, model.KindOther} {
		w, err := s.CreateWorkItem(string(k)+" item", k, "ctx", "desc")
		if err != nil {
			t.Fatalf("kind %s: %v", k, err)
		}
		if w.Kind != k || w.Context != "ctx" || w.Description != "desc" {
			t.Fatalf("bad work item: %+v", w)
		}
	}
	if _, err := s.CreateWorkItem("bad", model.WorkItemKind("task"), "", ""); err == nil {
		t.Fatal("expected invalid kind error")
	}
}

func TestMarkWorkItemDoneIdempotentNoDeadlock(t *testing.T) {
	s := openTest(t)
	w, err := s.CreateWorkItem("done twice", model.KindGoal, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		go func() { _, err := s.MarkWorkItemDone(w.ID); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("MarkWorkItemDone timed out; possible max-open-conns=1 self-deadlock")
		}
	}
}

func TestDoneWorkItemCannotReceiveNewClassifications(t *testing.T) {
	s := openTest(t)
	w, _ := s.CreateWorkItem("archive", model.KindProduct, "", "")
	_, _ = s.MarkWorkItemDone(w.ID)
	_, err := s.UpsertEvent(model.Event{Start: time.Now(), Duration: 12, SourceKey: "done-class"})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := s.ListEvents(EventFilter{})
	if err := s.ClassifyEvent(events[0].ID, &w.ID, nil, "manual"); err == nil {
		t.Fatal("expected classification to done item to fail")
	}
}

func TestReactivateWorkItemRestoresItsRules(t *testing.T) {
	s := openTest(t)
	w, err := s.CreateWorkItem("Paused", model.KindProject, "", "")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := s.CreateRule(model.Rule{WorkItemID: w.ID, Field: model.FieldApp, Match: model.MatchContains, Pattern: "Code"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkWorkItemDone(w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReactivateWorkItem(w.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := s.GetRule(rule.ID)
	if err != nil || !restored.Active {
		t.Fatalf("reactivated rule=%+v err=%v", restored, err)
	}
}

func TestClassificationAuditRecordsRuleAndManualCorrection(t *testing.T) {
	s := openTest(t)
	oldItem, _ := s.CreateWorkItem("old", model.KindProject, "", "")
	newItem, _ := s.CreateWorkItem("new", model.KindProject, "", "")
	rule, _ := s.CreateRule(model.Rule{WorkItemID: oldItem.ID, Field: model.FieldApp, Match: model.MatchContains, Pattern: "Code"})
	_, err := s.UpsertEvent(model.Event{Start: time.Now(), Duration: 60, App: "Code", SourceKey: "audit"})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := s.ListEvents(EventFilter{})
	rid := rule.ID
	if err := s.ClassifyEvent(events[0].ID, &oldItem.ID, &rid, "rule"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClassifyEvent(events[0].ID, &newItem.ID, nil, "manual"); err != nil {
		t.Fatal(err)
	}

	audit, err := s.ListClassificationAudit(events[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 2 {
		t.Fatalf("want 2 audit records, got %+v", audit)
	}
	if audit[0].OldWorkItemID != nil || audit[0].NewWorkItemID == nil || *audit[0].NewWorkItemID != oldItem.ID || audit[0].NewRuleID == nil || *audit[0].NewRuleID != rule.ID || audit[0].NewSource != "rule" {
		t.Fatalf("bad first audit row: %+v", audit[0])
	}
	if audit[1].OldWorkItemID == nil || *audit[1].OldWorkItemID != oldItem.ID || audit[1].NewWorkItemID == nil || *audit[1].NewWorkItemID != newItem.ID || audit[1].OldRuleID == nil || *audit[1].OldRuleID != rule.ID || audit[1].NewRuleID != nil || audit[1].OldSource != "rule" || audit[1].NewSource != "manual" {
		t.Fatalf("bad correction audit row: %+v", audit[1])
	}
}

func TestCannotCreateRuleForDoneWorkItem(t *testing.T) {
	s := openTest(t)
	w, _ := s.CreateWorkItem("done rule", model.KindGoal, "", "")
	_, _ = s.MarkWorkItemDone(w.ID)
	if _, err := s.CreateRule(model.Rule{WorkItemID: w.ID, Field: model.FieldApp, Match: model.MatchContains, Pattern: "Code"}); err == nil {
		t.Fatal("expected rule creation for done item to fail")
	}
}

func TestEventUpsertDedup(t *testing.T) {
	s := openTest(t)
	e := model.Event{Start: time.Now(), Duration: 100, App: "Code", Title: "x", SourceKey: "aw:w:1"}
	created, err := s.UpsertEvent(e)
	if err != nil || !created {
		t.Fatalf("first upsert should create: created=%v err=%v", created, err)
	}
	e.Duration = 250
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

func TestReplaceEventGroupRemovesStaleFragments(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	group := "aw:window:42"
	first := []model.Event{
		{Start: base, Duration: 300, SourceKey: group + ":part:0"},
		{Start: base.Add(10 * time.Minute), Duration: 300, SourceKey: group + ":part:1"},
	}
	created, _, deleted, _, err := s.ReplaceEventGroup(group, first)
	if err != nil || created != 2 || deleted != 0 {
		t.Fatalf("first replace: created=%d deleted=%d err=%v", created, deleted, err)
	}
	second := []model.Event{{Start: base, Duration: 900, SourceKey: group}}
	created, _, deleted, _, err = s.ReplaceEventGroup(group, second)
	if err != nil || created != 1 || deleted != 2 {
		t.Fatalf("second replace: created=%d deleted=%d err=%v", created, deleted, err)
	}
	events, _ := s.ListEvents(EventFilter{})
	if len(events) != 1 || events[0].SourceKey != group || events[0].SourceGroup != group {
		t.Fatalf("reconciled events = %+v", events)
	}
	_, _, deleted, _, err = s.ReplaceEventGroup(group, nil)
	if err != nil || deleted != 1 {
		t.Fatalf("tombstone replace: deleted=%d err=%v", deleted, err)
	}
}

func TestReplaceEventGroupPreservesCorrectionHistoryAndCarriesUnambiguousAssignment(t *testing.T) {
	s := openTest(t)
	item, err := s.CreateWorkItem("Tally", model.KindProject, "", "")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	group := "aw:window:preserve"
	fragments := []model.Event{
		{Start: base, Duration: 1200, SourceKey: group + ":part:0"},
		{Start: base.Add(40 * time.Minute), Duration: 1200, SourceKey: group + ":part:1"},
	}
	if _, _, _, _, err := s.ReplaceEventGroup(group, fragments); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEvents(EventFilter{})
	if err != nil || len(events) != 2 {
		t.Fatalf("initial events=%+v err=%v", events, err)
	}
	for _, event := range events {
		if err := s.ClassifyEvent(event.ID, &item.ID, nil, "manual"); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.ListClassificationAudit(0)
	if err != nil || len(before) != 2 {
		t.Fatalf("before audit=%+v err=%v", before, err)
	}
	if _, _, _, _, err := s.ReplaceEventGroup(group, []model.Event{{Start: base, Duration: 3600, SourceKey: group}}); err != nil {
		t.Fatal(err)
	}
	afterEvents, err := s.ListEvents(EventFilter{})
	if err != nil || len(afterEvents) != 1 {
		t.Fatalf("after events=%+v err=%v", afterEvents, err)
	}
	if afterEvents[0].WorkItemID == nil || *afterEvents[0].WorkItemID != item.ID {
		t.Fatalf("manual assignment was not carried forward: %+v", afterEvents[0])
	}
	afterAudit, err := s.ListClassificationAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAudit) < len(before) {
		t.Fatalf("audit history shrank: before=%d after=%d", len(before), len(afterAudit))
	}
	wantKeys := map[string]bool{fragments[0].SourceKey: true, fragments[1].SourceKey: true}
	for _, entry := range afterAudit[:2] {
		if entry.EventID != nil || !wantKeys[entry.SourceKey] {
			t.Fatalf("detached history lost attribution: entry=%+v", entry)
		}
		delete(wantKeys, entry.SourceKey)
	}
	if len(wantKeys) != 0 {
		t.Fatalf("missing detached source keys: %v", wantKeys)
	}
}

func TestReplaceEventGroupSurfacesConflictingAssignmentsWithoutGuessing(t *testing.T) {
	s := openTest(t)
	left, _ := s.CreateWorkItem("Left", model.KindProject, "", "")
	right, _ := s.CreateWorkItem("Right", model.KindProject, "", "")
	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	group := "aw:window:conflict"
	fragments := []model.Event{
		{Start: base, Duration: 1200, SourceKey: group + ":part:0"},
		{Start: base.Add(40 * time.Minute), Duration: 1200, SourceKey: group + ":part:1"},
	}
	if _, _, _, _, err := s.ReplaceEventGroup(group, fragments); err != nil {
		t.Fatal(err)
	}
	events, _ := s.ListEvents(EventFilter{})
	if err := s.ClassifyEvent(events[0].ID, &left.ID, nil, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClassifyEvent(events[1].ID, &right.ID, nil, "manual"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, conflicts, err := s.ReplaceEventGroup(group, []model.Event{{Start: base, Duration: 3600, SourceKey: group}}); err != nil {
		t.Fatal(err)
	} else if conflicts != 1 {
		t.Fatalf("conflicts=%d, want 1", conflicts)
	}
	merged, _ := s.ListEvents(EventFilter{})
	if len(merged) != 1 || merged[0].WorkItemID != nil {
		t.Fatalf("conflicting assignments must remain unresolved: %+v", merged)
	}
	audit, err := s.ListClassificationAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	foundConflict := false
	for _, entry := range audit {
		if entry.NewSource == "reconcile_conflict" {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatalf("reconciliation conflict was not surfaced: %+v", audit)
	}
}

func TestExactRangeAggregationClipsOverlap(t *testing.T) {
	s := openTest(t)
	w, _ := s.CreateWorkItem("range", model.KindGoal, "", "")
	base := time.Date(2026, 3, 8, 6, 55, 0, 0, time.UTC)
	_, err := s.UpsertEvent(model.Event{Start: base, Duration: 20 * 60, WorkItemID: &w.ID, SourceKey: "overlap"})
	if err != nil {
		t.Fatal(err)
	}
	from, to := base.Add(5*time.Minute), base.Add(10*time.Minute)
	hours, err := s.HoursByWorkItem(from, to)
	if err != nil || len(hours) != 1 || hours[0].Seconds != 300 {
		t.Fatalf("clipped work item hours = %+v err=%v", hours, err)
	}
	total, _ := s.TotalSeconds(from, to)
	if total != 300 {
		t.Fatalf("clipped total = %v", total)
	}
}

func TestLLMCacheRejectsDoneWorkItem(t *testing.T) {
	s := openTest(t)
	w, _ := s.CreateWorkItem("cached", model.KindOther, "", "")
	if err := s.LLMCachePut("sig-done", &w.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = s.MarkWorkItemDone(w.ID)
	if _, ok, err := s.LLMCacheGet("sig-done"); err != nil || ok {
		t.Fatalf("done cache: ok=%v err=%v", ok, err)
	}
}

func TestHoursByProject(t *testing.T) {
	s := openTest(t)
	p, _ := s.CreateProject("P", model.TypeBillable, "")
	now := time.Now()
	e1 := model.Event{Start: now, Duration: 3600, ProjectID: &p.ID, SourceKey: "a"}
	e2 := model.Event{Start: now, Duration: 1800, ProjectID: &p.ID, SourceKey: "b"}
	e3 := model.Event{Start: now, Duration: 900, SourceKey: "c"}
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

func TestMigration0002FromV1PreservesHistoryAndLegacyEvidence(t *testing.T) {
	path := t.TempDir() + "/v1.db"
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY); INSERT INTO schema_migrations(version) VALUES (1);`); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	doneAt := created.Add(time.Hour)
	if _, err := db.Exec(`INSERT INTO projects(id,name,type,client,status,created_at,done_at) VALUES (7,'Legacy','internal','ACME','done',?,?)`, created, doneAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects(id,name,type,client,status,created_at) VALUES (10,'Legacy Billable','billable','ACME','active',?)`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO rules(id,project_id,field,match,pattern,priority,active,created_at) VALUES (8,7,'app','contains','Code',5,1,?)`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events(id,start,duration,app,title,project_id,rule_id,source,source_key) VALUES (9,?,123.456,'Code','x',7,8,'rule','src')`, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO llm_cache(signature,project_id,created_at) VALUES ('sig',7,?)`, created); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err := s.GetWorkItem(7)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Legacy" || w.Kind != model.KindProject || w.Context != "ACME" || w.Status != model.StatusDone {
		t.Fatalf("bad migrated item: %+v", w)
	}
	var legacyType string
	var enabled int
	if err := s.DB().QueryRow(`SELECT legacy_type, enabled FROM billing_profiles WHERE work_item_id=7`).Scan(&legacyType, &enabled); err != nil || legacyType != "internal" || enabled != 0 {
		t.Fatalf("internal billing profile = %q enabled=%d err=%v", legacyType, enabled, err)
	}
	if err := s.DB().QueryRow(`SELECT legacy_type, enabled FROM billing_profiles WHERE work_item_id=10`).Scan(&legacyType, &enabled); err != nil || legacyType != "billable" || enabled != 1 {
		t.Fatalf("billable profile = %q enabled=%d err=%v", legacyType, enabled, err)
	}
	var legacyName string
	if err := s.DB().QueryRow(`SELECT name FROM legacy_projects WHERE id=7`).Scan(&legacyName); err != nil || legacyName != "Legacy" {
		t.Fatalf("legacy project missing: %q %v", legacyName, err)
	}
	events, err := s.ListEvents(EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].WorkItemID == nil || *events[0].WorkItemID != 7 || events[0].RuleID == nil || *events[0].RuleID != 8 || events[0].Duration != 123.456 || events[0].Source != "rule" {
		t.Fatalf("bad migrated event: %+v", events)
	}
	var fkErrors int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkErrors); err != nil || fkErrors != 0 {
		t.Fatalf("foreign key check: count=%d err=%v", fkErrors, err)
	}
	var cachedID int64
	if err := s.DB().QueryRow(`SELECT work_item_id FROM llm_cache WHERE signature='sig'`).Scan(&cachedID); err != nil || cachedID != 7 {
		t.Fatalf("migrated cache row: id=%d err=%v", cachedID, err)
	}
	if _, ok, err := s.LLMCacheGet("sig"); err != nil || ok {
		t.Fatalf("cache for completed item must be invalidated: ok=%v err=%v", ok, err)
	}
}

func TestMigration0006PreservesAuditAndDetachesDeletedEvent(t *testing.T) {
	path := t.TempDir() + "/v5.db"
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d setup: %v", i+1, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO work_items(id,name,kind) VALUES (1,'Migrated','project')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events(id,start,duration,work_item_id,source,source_key,source_group) VALUES (9,?,60,1,'manual','aw:migrate:part:0','aw:migrate')`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO classification_audit(event_id,new_work_item_id,new_source) VALUES (9,1,'manual')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	audit, err := s.ListClassificationAudit(0)
	if err != nil || len(audit) != 1 || audit[0].EventID == nil || audit[0].SourceKey != "aw:migrate:part:0" {
		t.Fatalf("migrated audit=%+v err=%v", audit, err)
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE id = 9`); err != nil {
		t.Fatal(err)
	}
	audit, err = s.ListClassificationAudit(0)
	if err != nil || len(audit) != 1 || audit[0].EventID != nil || audit[0].SourceKey != "aw:migrate:part:0" {
		t.Fatalf("detached audit=%+v err=%v", audit, err)
	}
}

func TestLegacyProjectTypePreservedThroughCompatibilityViews(t *testing.T) {
	s := openTest(t)
	project, err := s.CreateProject("Internal legacy", model.TypeInternal, "LMS")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(project.ID)
	if err != nil || got.Type != model.TypeInternal {
		t.Fatalf("GetProject=%+v err=%v", got, err)
	}
	list, err := s.ListProjects(model.StatusActive)
	if err != nil || len(list) != 1 || list[0].Type != model.TypeInternal {
		t.Fatalf("ListProjects=%+v err=%v", list, err)
	}
	if _, err := s.UpsertEvent(model.Event{Start: time.Now(), Duration: 60, WorkItemID: &project.ID, SourceKey: "legacy-type"}); err != nil {
		t.Fatal(err)
	}
	hours, err := s.HoursByProject(time.Time{}, time.Time{})
	if err != nil || len(hours) != 1 || hours[0].Project.Type != model.TypeInternal {
		t.Fatalf("HoursByProject=%+v err=%v", hours, err)
	}
}

func TestClearRuleClassificationsIsAudited(t *testing.T) {
	s := openTest(t)
	item, _ := s.CreateWorkItem("Audited", model.KindGoal, "", "")
	rule, err := s.CreateRule(model.Rule{WorkItemID: item.ID, Field: model.FieldApp, Match: model.MatchEquals, Pattern: "Code", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertEvent(model.Event{Start: time.Now(), Duration: 60, App: "Code", SourceKey: "audit-clear"}); err != nil {
		t.Fatal(err)
	}
	events, _ := s.ListEvents(EventFilter{})
	if err := s.ClassifyEvent(events[0].ID, &item.ID, &rule.ID, "rule"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ClearRuleClassifications(rule.ID); err != nil || n != 1 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
	audit, err := s.ListClassificationAudit(events[0].ID)
	if err != nil || len(audit) != 2 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	last := audit[len(audit)-1]
	if last.OldWorkItemID == nil || *last.OldWorkItemID != item.ID || last.NewWorkItemID != nil || last.NewSource != "rule_clear" {
		t.Fatalf("clear audit=%+v", last)
	}
}
