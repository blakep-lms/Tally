package model

import (
	"testing"
	"time"
)

func TestDomainEnumsAndCompatibilityRoundTrip(t *testing.T) {
	for _, kind := range []WorkItemKind{KindProject, KindProduct, KindGoal, KindOther} {
		if !kind.Valid() {
			t.Fatalf("kind %q invalid", kind)
		}
	}
	if WorkItemKind("invoice").Valid() || !StatusActive.Valid() || !StatusDone.Valid() || WorkItemStatus("archived").Valid() {
		t.Fatal("domain validation accepted an invalid value")
	}
	for _, field := range []RuleField{FieldApp, FieldTitle, FieldURL, FieldRepo} {
		if !field.Valid() {
			t.Fatalf("field %q invalid", field)
		}
	}
	for _, match := range []MatchKind{MatchContains, MatchEquals, MatchRegex} {
		if !match.Valid() {
			t.Fatalf("match %q invalid", match)
		}
	}

	now := time.Now().UTC()
	item := WorkItem{ID: 7, Name: "Tally", Kind: KindProject, Context: "LMS", Status: StatusDone, CreatedAt: now, DoneAt: &now}
	project := ProjectFromWorkItem(item, "")
	if project.Type != TypeBillable || project.Client != "LMS" {
		t.Fatalf("project=%+v", project)
	}
	roundTrip := WorkItemFromProject(project)
	if roundTrip.ID != item.ID || roundTrip.Kind != KindProject || roundTrip.Status != StatusDone {
		t.Fatalf("round trip=%+v", roundTrip)
	}
}

func TestBillingPolicyEnumsAreLocked(t *testing.T) {
	if !RoundingUp.Valid() || RoundingMode("nearest").Valid() {
		t.Fatal("rounding policy is not locked to upward")
	}
	if !RoundingScopePeriodWorkItem.Valid() || RoundingScope("event").Valid() {
		t.Fatal("rounding scope is not locked to period work item")
	}
	for _, mode := range []PeriodMode{PeriodWeekly, PeriodBiweekly, PeriodSemimonthly, PeriodMonthly, PeriodFinal, PeriodCustom} {
		if !mode.Valid() {
			t.Fatalf("period %q invalid", mode)
		}
	}
	if PeriodMode("daily").Valid() {
		t.Fatal("daily must not be a billing period")
	}
	p := DefaultBillingProfile()
	if p.Enabled || p.RoundingMode != RoundingUp || p.RoundingIncrementMinutes != 15 || p.RoundingScope != RoundingScopePeriodWorkItem {
		t.Fatalf("default billing profile=%+v", p)
	}
}
