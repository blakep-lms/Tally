package core

import (
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

func TestPeriodRangePresetsAndDST(t *testing.T) {
	utcNow := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	from, to := PeriodRange(model.PeriodMonthly, "", utcNow)
	if from.Format("2006-01-02") != "2026-07-01" || to.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("monthly = %s %s", from, to)
	}
	from, to = PeriodRange(model.PeriodSemimonthly, "", utcNow)
	if from.Format("2006-01-02") != "2026-07-16" || to.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("semimonthly = %s %s", from, to)
	}
	from, to = PeriodRange(model.PeriodBiweekly, "", utcNow)
	if from.Weekday() != time.Monday || to.Format("2006-01-02") != from.AddDate(0, 0, 14).Format("2006-01-02") {
		t.Fatalf("biweekly = %s %s", from, to)
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	spring := time.Date(2026, 3, 8, 12, 0, 0, 0, loc)
	from, to = PeriodRange(model.PeriodWeekly, "", spring)
	if from.Format("2006-01-02") != "2026-03-02" || to.Format("2006-01-02") != "2026-03-09" || to.Sub(from) != 167*time.Hour {
		t.Fatalf("DST week = %s %s duration=%s", from, to, to.Sub(from))
	}
}

func TestFinalRangeUsesCompletionIndependentlyFromBilling(t *testing.T) {
	created := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	done := created.Add(72 * time.Hour)
	item := model.WorkItem{CreatedAt: created, DoneAt: &done}
	from, to := FinalRange(item, done.Add(24*time.Hour))
	if !from.Equal(created) || !to.Equal(done) {
		t.Fatalf("completed final range = %s %s", from, to)
	}
	item.DoneAt = nil
	now := done.Add(24 * time.Hour)
	_, to = FinalRange(item, now)
	if !to.Equal(now) {
		t.Fatalf("active final range end = %s", to)
	}
}
