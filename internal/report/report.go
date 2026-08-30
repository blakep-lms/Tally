// Package report renders exact generic work-item summaries plus optional billing projections.
package report

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

type BillingLine struct {
	ItemID              int64                `json:"item_id"`
	ItemName            string               `json:"item_name"`
	ExactMilliseconds   int64                `json:"exact_milliseconds"`
	RoundedMilliseconds int64                `json:"rounded_milliseconds"`
	ExactSeconds        int64                `json:"exact_seconds"`
	AdjustedSeconds     int64                `json:"adjusted_seconds"`
	RateMinor           int64                `json:"rate_minor"`
	Currency            string               `json:"currency"`
	AmountMinor         int64                `json:"amount_minor"`
	Policy              model.BillingProfile `json:"policy"`
}

type Line struct {
	WorkItem          model.WorkItem `json:"work_item"`
	ExactMilliseconds int64          `json:"exact_milliseconds"`
	ExactSeconds      int64          `json:"exact_seconds"`
	ExactHours        float64        `json:"exact_hours"`
	Billing           *BillingLine   `json:"billing,omitempty"`
}

// Report is the computed exact hours summary for a window.
type Report struct {
	From                   time.Time            `json:"from"`
	To                     time.Time            `json:"to"`
	Items                  []Line               `json:"items"`
	Projects               []model.ProjectHours `json:"projects,omitempty"` // legacy JSON compatibility
	BillableHours          float64              `json:"billable_hours"`
	InternalHours          float64              `json:"internal_hours"`
	UnclassifiedHrs        float64              `json:"unclassified_hours"`
	TotalHours             float64              `json:"total_hours"`
	UnclassifiedSeconds    int64                `json:"unclassified_seconds"`
	TotalExactSeconds      int64                `json:"total_exact_seconds"`
	TotalExactMilliseconds int64                `json:"total_exact_milliseconds"`
	TotalAmountMinor       int64                `json:"total_amount_minor,omitempty"`
	TotalsByCurrency       map[string]int64     `json:"totals_by_currency,omitempty"`
}

func Build(from, to time.Time, hours []model.ProjectHours, unclassifiedSeconds, totalSeconds float64) Report {
	lines := make([]model.WorkItemHours, 0, len(hours))
	for _, ph := range hours {
		lines = append(lines, model.WorkItemHours{WorkItem: model.WorkItemFromProject(ph.Project), Seconds: ph.Seconds, Hours: ph.Hours})
	}
	return BuildWorkItems(from, to, lines, unclassifiedSeconds, totalSeconds, nil)
}

func BuildWorkItems(from, to time.Time, hours []model.WorkItemHours, unclassifiedSeconds, totalSeconds float64, profiles map[int64]model.BillingProfile) Report {
	rep, _ := BuildWorkItemsChecked(from, to, hours, unclassifiedSeconds, totalSeconds, profiles)
	return rep
}

func BuildWorkItemsChecked(from, to time.Time, hours []model.WorkItemHours, unclassifiedSeconds, totalSeconds float64, profiles map[int64]model.BillingProfile) (Report, error) {
	if !validSeconds(unclassifiedSeconds) || !validSeconds(totalSeconds) {
		return Report{}, errors.New("report seconds are outside the supported range")
	}
	r := Report{From: from, To: to, Items: make([]Line, 0), UnclassifiedSeconds: int64(math.Round(unclassifiedSeconds)), TotalExactSeconds: int64(math.Round(totalSeconds)), TotalExactMilliseconds: int64(math.Round(totalSeconds * 1000)), UnclassifiedHrs: unclassifiedSeconds / 3600, TotalHours: totalSeconds / 3600, TotalsByCurrency: map[string]int64{}}
	for _, wh := range hours {
		if !validSeconds(wh.Seconds) {
			return Report{}, fmt.Errorf("work item %d seconds are outside the supported range", wh.WorkItem.ID)
		}
		exactMS := int64(math.Round(wh.Seconds * 1000))
		line := Line{WorkItem: wh.WorkItem, ExactMilliseconds: exactMS, ExactSeconds: int64(math.Round(wh.Seconds)), ExactHours: wh.Seconds / 3600}
		if profiles != nil {
			p, ok := profiles[wh.WorkItem.ID]
			if ok && p.Enabled {
				r.BillableHours += wh.Hours
			} else {
				r.InternalHours += wh.Hours
			}
			if ok && p.Enabled && p.HourlyRateMinor > 0 {
				roundedMS := RoundMillisecondsUp(exactMS, p.RoundingIncrementMinutes)
				amount, err := billingAmount(roundedMS, p.HourlyRateMinor)
				if err != nil {
					return Report{}, fmt.Errorf("work item %d: %w", wh.WorkItem.ID, err)
				}
				line.Billing = &BillingLine{ItemID: wh.WorkItem.ID, ItemName: wh.WorkItem.Name, ExactMilliseconds: exactMS, RoundedMilliseconds: roundedMS, ExactSeconds: line.ExactSeconds, AdjustedSeconds: roundedMS / 1000, RateMinor: p.HourlyRateMinor, Currency: p.Currency, AmountMinor: amount, Policy: p}
				current := r.TotalsByCurrency[p.Currency]
				if amount > math.MaxInt64-current {
					return Report{}, fmt.Errorf("billing total for %s overflows minor units", p.Currency)
				}
				r.TotalsByCurrency[p.Currency] = current + amount
			}
		}
		r.Items = append(r.Items, line)
		if profiles == nil && wh.WorkItem.Kind == model.KindProject {
			p := model.ProjectFromWorkItem(wh.WorkItem, "")
			r.Projects = append(r.Projects, model.ProjectHours{Project: p, Seconds: wh.Seconds, Hours: wh.Hours})
			if p.Type == model.TypeInternal {
				r.InternalHours += wh.Hours
			} else {
				r.BillableHours += wh.Hours
			}
		} else if profiles == nil {
			r.InternalHours += wh.Hours
		}
	}
	if len(r.TotalsByCurrency) == 1 {
		for _, amount := range r.TotalsByCurrency {
			r.TotalAmountMinor = amount
		}
	}
	return r, nil
}

func validSeconds(seconds float64) bool {
	return seconds >= 0 && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds <= float64(math.MaxInt64)/1000
}

func billingAmount(milliseconds, rateMinor int64) (int64, error) {
	if milliseconds < 0 || rateMinor < 0 {
		return 0, errors.New("billing inputs must be non-negative")
	}
	product := new(big.Int).Mul(big.NewInt(milliseconds), big.NewInt(rateMinor))
	product.Add(product, big.NewInt(1_800_000))
	product.Quo(product, big.NewInt(3_600_000))
	if !product.IsInt64() {
		return 0, errors.New("billing amount overflows minor units")
	}
	return product.Int64(), nil
}

func RoundMillisecondsUp(milliseconds int64, incrementMinutes int) int64 {
	if milliseconds <= 0 {
		return milliseconds
	}
	inc := int64(incrementMinutes) * 60 * 1000
	if inc <= 0 {
		return milliseconds
	}
	quotient := milliseconds / inc
	if milliseconds%inc != 0 {
		quotient++
	}
	if quotient > math.MaxInt64/inc {
		return math.MaxInt64
	}
	return quotient * inc
}

func RoundSeconds(seconds int64, _ model.RoundingMode, incrementMinutes int) int64 {
	return RoundMillisecondsUp(seconds*1000, incrementMinutes) / 1000
}

func (r Report) JSON() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	return string(b), err
}

func (r Report) Markdown() string {
	var b strings.Builder
	from := "beginning"
	if !r.From.IsZero() {
		from = r.From.Format("2006-01-02")
	}
	fmt.Fprintf(&b, "# Tally report — %s to %s\n\n", from, r.To.Format("2006-01-02"))
	b.WriteString("| Item | Kind | Context | Exact hours | Adjusted hours | Rate | Amount |\n|---|---|---|---:|---:|---:|---:|\n")
	for _, l := range r.Items {
		adj, rate, amount := "—", "—", "—"
		if l.Billing != nil {
			adj = fmt.Sprintf("%.2f", float64(l.Billing.AdjustedSeconds)/3600)
			rate = money(l.Billing.Currency, l.Billing.RateMinor)
			amount = money(l.Billing.Currency, l.Billing.AmountMinor)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.2f | %s | %s | %s |\n", l.WorkItem.Name, l.WorkItem.Kind, dash(l.WorkItem.Context), round2(l.ExactHours), adj, rate, amount)
	}
	fmt.Fprintf(&b, "\n- **Unclassified:** %.2f h\n", float64(r.UnclassifiedSeconds)/3600)
	fmt.Fprintf(&b, "- **Total tracked:** %.2f h\n", float64(r.TotalExactSeconds)/3600)
	currencies := make([]string, 0, len(r.TotalsByCurrency))
	for currency := range r.TotalsByCurrency {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		amount := r.TotalsByCurrency[currency]
		fmt.Fprintf(&b, "- **Projected total (%s):** %s\n", currency, money(currency, amount))
	}
	return b.String()
}

func (r Report) CSV() (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write([]string{"item", "kind", "context", "exact_seconds", "exact_hours", "adjusted_seconds", "adjusted_hours", "currency", "rate_minor", "amount_minor"}); err != nil {
		return "", err
	}
	for _, l := range r.Items {
		adjS, adjH, cur, rate, amt := "", "", "", "", ""
		if l.Billing != nil {
			adjS = strconv.FormatInt(l.Billing.AdjustedSeconds, 10)
			adjH = fmt.Sprintf("%.2f", float64(l.Billing.AdjustedSeconds)/3600)
			cur = l.Billing.Currency
			rate = strconv.FormatInt(l.Billing.RateMinor, 10)
			amt = strconv.FormatInt(l.Billing.AmountMinor, 10)
		}
		if err := w.Write([]string{l.WorkItem.Name, string(l.WorkItem.Kind), l.WorkItem.Context, strconv.FormatInt(l.ExactSeconds, 10), fmt.Sprintf("%.2f", round2(l.ExactHours)), adjS, adjH, cur, rate, amt}); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
func money(c string, minor int64) string {
	if c == "" {
		c = "USD"
	}
	sign := ""
	if minor < 0 {
		sign = "-"
	}
	major := minor / 100
	if major < 0 {
		major = -major
	}
	cents := minor % 100
	if cents < 0 {
		cents = -cents
	}
	return fmt.Sprintf("%s %s%d.%02d", c, sign, major, cents)
}
