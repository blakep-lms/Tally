// Package report renders per-project hour summaries in the formats the
// operator drops into billing: markdown, CSV, and JSON.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

// Report is the computed hours summary for a window.
type Report struct {
	From            time.Time            `json:"from"`
	To              time.Time            `json:"to"`
	Projects        []model.ProjectHours `json:"projects"`
	BillableHours   float64              `json:"billable_hours"`
	InternalHours   float64              `json:"internal_hours"`
	UnclassifiedHrs float64              `json:"unclassified_hours"`
	TotalHours      float64              `json:"total_hours"`
}

// Build assembles a Report from aggregated hours and unclassified seconds.
func Build(from, to time.Time, hours []model.ProjectHours, unclassifiedSeconds, totalSeconds float64) Report {
	r := Report{From: from, To: to, Projects: hours}
	for _, ph := range hours {
		switch ph.Project.Type {
		case model.TypeBillable:
			r.BillableHours += ph.Hours
		case model.TypeInternal:
			r.InternalHours += ph.Hours
		}
	}
	r.UnclassifiedHrs = unclassifiedSeconds / 3600
	r.TotalHours = totalSeconds / 3600
	return r
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// JSON renders the report as indented JSON.
func (r Report) JSON() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	return string(b), err
}

// Markdown renders the report as a table suitable for pasting into notes.
func (r Report) Markdown() string {
	var b strings.Builder
	from := "beginning"
	if !r.From.IsZero() {
		from = r.From.Format("2006-01-02")
	}
	fmt.Fprintf(&b, "# Tally report — %s to %s\n\n", from, r.To.Format("2006-01-02"))
	b.WriteString("| Project | Type | Client | Hours |\n")
	b.WriteString("|---|---|---|---:|\n")
	for _, ph := range r.Projects {
		fmt.Fprintf(&b, "| %s | %s | %s | %.2f |\n",
			ph.Project.Name, ph.Project.Type, dash(ph.Project.Client), round2(ph.Hours))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- **Billable:** %.2f h\n", round2(r.BillableHours))
	fmt.Fprintf(&b, "- **Internal:** %.2f h\n", round2(r.InternalHours))
	fmt.Fprintf(&b, "- **Unclassified:** %.2f h\n", round2(r.UnclassifiedHrs))
	fmt.Fprintf(&b, "- **Total tracked:** %.2f h\n", round2(r.TotalHours))
	return b.String()
}

// CSV renders the report as CSV rows: project,type,client,hours.
func (r Report) CSV() (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write([]string{"project", "type", "client", "hours"}); err != nil {
		return "", err
	}
	for _, ph := range r.Projects {
		row := []string{
			ph.Project.Name,
			string(ph.Project.Type),
			ph.Project.Client,
			strconv.FormatFloat(round2(ph.Hours), 'f', 2, 64),
		}
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
