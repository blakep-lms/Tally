package model

import (
	"encoding/json"
	"time"
)

// RoundingMode is intentionally single-policy in v1: billable item-period
// subtotals always round upward. Exact tracked time is never modified.
type RoundingMode string

const RoundingUp RoundingMode = "up"

func (m RoundingMode) Valid() bool { return m == RoundingUp }

type RoundingScope string

const RoundingScopePeriodWorkItem RoundingScope = "period_work_item"

func (s RoundingScope) Valid() bool { return s == RoundingScopePeriodWorkItem }

type PeriodMode string

const (
	PeriodWeekly      PeriodMode = "weekly"
	PeriodBiweekly    PeriodMode = "biweekly"
	PeriodSemimonthly PeriodMode = "semimonthly"
	PeriodMonthly     PeriodMode = "monthly"
	PeriodFinal       PeriodMode = "final"
	PeriodCustom      PeriodMode = "custom"
)

func (m PeriodMode) Valid() bool {
	switch m {
	case PeriodWeekly, PeriodBiweekly, PeriodSemimonthly, PeriodMonthly, PeriodFinal, PeriodCustom:
		return true
	}
	return false
}

type BillingScopeType string

const (
	BillingScopeGlobal   BillingScopeType = "global"
	BillingScopeClient   BillingScopeType = "client"
	BillingScopeWorkItem BillingScopeType = "work_item"
)

func (s BillingScopeType) Valid() bool {
	return s == BillingScopeGlobal || s == BillingScopeClient || s == BillingScopeWorkItem
}

type BillingProfile struct {
	ID                       int64            `json:"id"`
	ScopeType                BillingScopeType `json:"scope_type"`
	ScopeKey                 string           `json:"scope_key"`
	Enabled                  bool             `json:"enabled"`
	Currency                 string           `json:"currency"`
	HourlyRateMinor          int64            `json:"hourly_rate_minor"`
	RoundingMode             RoundingMode     `json:"rounding_mode"`
	RoundingIncrementMinutes int              `json:"rounding_increment_minutes"`
	RoundingScope            RoundingScope    `json:"rounding_scope"`
	PeriodMode               PeriodMode       `json:"period_mode"`
	PeriodAnchor             string           `json:"period_anchor"`
	LegacyType               ProjectType      `json:"legacy_type,omitempty"`
	CreatedAt                time.Time        `json:"created_at"`
	UpdatedAt                time.Time        `json:"updated_at"`
}

// BillingProfilePatch distinguishes omitted values from explicit zero/false
// values so HTTP and MCP clients can update one policy field without resetting
// every other field.
type BillingProfilePatch struct {
	ScopeType                BillingScopeType `json:"scope_type"`
	ScopeKey                 string           `json:"scope_key"`
	Enabled                  *bool            `json:"enabled,omitempty"`
	Currency                 *string          `json:"currency,omitempty"`
	HourlyRateMinor          *int64           `json:"hourly_rate_minor,omitempty"`
	RoundingMode             *RoundingMode    `json:"rounding_mode,omitempty"`
	RoundingIncrementMinutes *int             `json:"rounding_increment_minutes,omitempty"`
	RoundingScope            *RoundingScope   `json:"rounding_scope,omitempty"`
	PeriodMode               *PeriodMode      `json:"period_mode,omitempty"`
	PeriodAnchor             *string          `json:"period_anchor,omitempty"`
	LegacyType               *ProjectType     `json:"legacy_type,omitempty"`
}

func (patch BillingProfilePatch) Apply(profile BillingProfile) BillingProfile {
	profile.ScopeType, profile.ScopeKey = patch.ScopeType, patch.ScopeKey
	if patch.Enabled != nil {
		profile.Enabled = *patch.Enabled
	}
	if patch.Currency != nil {
		profile.Currency = *patch.Currency
	}
	if patch.HourlyRateMinor != nil {
		profile.HourlyRateMinor = *patch.HourlyRateMinor
	}
	if patch.RoundingMode != nil {
		profile.RoundingMode = *patch.RoundingMode
	}
	if patch.RoundingIncrementMinutes != nil {
		profile.RoundingIncrementMinutes = *patch.RoundingIncrementMinutes
	}
	if patch.RoundingScope != nil {
		profile.RoundingScope = *patch.RoundingScope
	}
	if patch.PeriodMode != nil {
		profile.PeriodMode = *patch.PeriodMode
	}
	if patch.PeriodAnchor != nil {
		profile.PeriodAnchor = *patch.PeriodAnchor
	}
	if patch.LegacyType != nil {
		profile.LegacyType = *patch.LegacyType
	}
	return profile
}

func DefaultBillingProfile() BillingProfile {
	return BillingProfile{
		ScopeType: BillingScopeGlobal, Enabled: false, Currency: "USD",
		RoundingMode: RoundingUp, RoundingIncrementMinutes: 15,
		RoundingScope: RoundingScopePeriodWorkItem, PeriodMode: PeriodCustom,
	}
}

// ReportSnapshot freezes the exact range, effective billing policies, and
// computed projection as JSON. It is an audit artifact, not an invoice.
type ReportSnapshot struct {
	ID         int64           `json:"id"`
	Label      string          `json:"label"`
	PeriodMode PeriodMode      `json:"period_mode"`
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Timezone   string          `json:"timezone"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}
