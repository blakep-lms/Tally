package report

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

func enabledProfile(currency string, rate int64) model.BillingProfile {
	return model.BillingProfile{Enabled: true, Currency: currency, HourlyRateMinor: rate, RoundingMode: model.RoundingUp, RoundingIncrementMinutes: 15, RoundingScope: model.RoundingScopePeriodWorkItem, PeriodMode: model.PeriodWeekly}
}

func TestRoundMillisecondsAlwaysUpOncePerItemPeriod(t *testing.T) {
	cases := []struct{ in, want int64 }{
		{0, 0}, {1, 900000}, {61250, 900000}, {900000, 900000}, {900001, 1800000},
	}
	for _, c := range cases {
		if got := RoundMillisecondsUp(c.in, 15); got != c.want {
			t.Fatalf("round %d = %d want %d", c.in, got, c.want)
		}
	}
}

func TestBuildWorkItemsPreservesExactMillisecondsAndRoundsMoneyHalfUp(t *testing.T) {
	item := model.WorkItem{ID: 3, Name: "Launch", Kind: model.KindProduct, Context: "LMS"}
	rep := BuildWorkItems(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), []model.WorkItemHours{{WorkItem: item, Seconds: 61.25}}, 30, 91.25, map[int64]model.BillingProfile{3: enabledProfile("USD", 10002)})
	if len(rep.Items) != 1 || rep.Items[0].Billing == nil {
		t.Fatalf("bad report: %+v", rep)
	}
	line := rep.Items[0]
	if line.ExactMilliseconds != 61250 || line.Billing.RoundedMilliseconds != 900000 || line.Billing.AmountMinor != 2501 {
		t.Fatalf("billing line = %+v", line)
	}
	if rep.TotalExactMilliseconds != 91250 || rep.TotalsByCurrency["USD"] != 2501 {
		t.Fatalf("totals = %+v", rep)
	}
	csv, err := rep.CSV()
	if err != nil || !strings.Contains(csv, "Launch,product,LMS") {
		t.Fatalf("csv=%q err=%v", csv, err)
	}
	if md := rep.Markdown(); !strings.Contains(md, "Adjusted hours") || !strings.Contains(md, "USD 25.01") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestDisabledBillingAndMixedCurrencies(t *testing.T) {
	items := []model.WorkItemHours{
		{WorkItem: model.WorkItem{ID: 1, Name: "USD", Kind: model.KindProject}, Seconds: 60},
		{WorkItem: model.WorkItem{ID: 2, Name: "EUR", Kind: model.KindGoal}, Seconds: 60},
		{WorkItem: model.WorkItem{ID: 3, Name: "Exact only", Kind: model.KindOther}, Seconds: 60},
	}
	profiles := map[int64]model.BillingProfile{1: enabledProfile("USD", 10000), 2: enabledProfile("EUR", 10000), 3: {Enabled: false, Currency: "USD", HourlyRateMinor: 10000}}
	rep := BuildWorkItems(time.Now(), time.Now().Add(time.Hour), items, 0, 180, profiles)
	if rep.TotalAmountMinor != 0 || rep.TotalsByCurrency["USD"] != 2500 || rep.TotalsByCurrency["EUR"] != 2500 || rep.Items[2].Billing != nil {
		t.Fatalf("mixed/disabled report = %+v", rep)
	}
}

func TestEffectiveBillingProfilesDriveLegacyHourBuckets(t *testing.T) {
	items := []model.WorkItemHours{
		{WorkItem: model.WorkItem{ID: 1, Name: "Internal project", Kind: model.KindProject}, Seconds: 3600, Hours: 1},
		{WorkItem: model.WorkItem{ID: 2, Name: "Billable product", Kind: model.KindProduct}, Seconds: 7200, Hours: 2},
	}
	profiles := map[int64]model.BillingProfile{
		1: {Enabled: false, Currency: "USD", HourlyRateMinor: 10000},
		2: enabledProfile("USD", 10000),
	}
	rep := BuildWorkItems(time.Now(), time.Now().Add(time.Hour), items, 0, 10800, profiles)
	if rep.BillableHours != 2 || rep.InternalHours != 1 {
		t.Fatalf("effective billing buckets: billable=%v internal=%v", rep.BillableHours, rep.InternalHours)
	}
}

func TestBillingAmountOverflowIsRejected(t *testing.T) {
	item := model.WorkItem{ID: 1, Name: "Overflow", Kind: model.KindProject}
	_, err := BuildWorkItemsChecked(time.Now(), time.Now().Add(2*time.Hour), []model.WorkItemHours{{WorkItem: item, Seconds: 7200}}, 0, 7200, map[int64]model.BillingProfile{1: enabledProfile("USD", math.MaxInt64)})
	if err == nil {
		t.Fatal("expected billing amount overflow to be rejected")
	}
}

func TestMoneyFormattingUsesExactMinorUnits(t *testing.T) {
	if got := money("USD", 10000000000000001); got != "USD 100000000000000.01" {
		t.Fatalf("money=%q", got)
	}
	if got := money("USD", -50); got != "USD -0.50" {
		t.Fatalf("negative money=%q", got)
	}
}

func TestEmptyReportUsesJSONArray(t *testing.T) {
	rep := BuildWorkItems(time.Now(), time.Now().Add(time.Hour), nil, 0, 0, nil)
	out, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"items": null`) || !strings.Contains(out, `"items": []`) {
		t.Fatalf("empty items must be an array: %s", out)
	}
}
