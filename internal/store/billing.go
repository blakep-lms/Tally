package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

const billingCols = `id, scope_type, scope_key, enabled, currency, hourly_rate_minor, rounding_mode, rounding_increment_minutes, rounding_scope, period_mode, period_anchor, COALESCE(legacy_type, ''), created_at, updated_at`

func scanBillingProfile(row interface{ Scan(...any) error }) (model.BillingProfile, error) {
	var p model.BillingProfile
	var scopeType, roundingMode, roundingScope, periodMode, legacyType string
	var enabled int
	if err := row.Scan(&p.ID, &scopeType, &p.ScopeKey, &enabled, &p.Currency, &p.HourlyRateMinor, &roundingMode, &p.RoundingIncrementMinutes, &roundingScope, &periodMode, &p.PeriodAnchor, &legacyType, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return model.BillingProfile{}, err
	}
	p.ScopeType = model.BillingScopeType(scopeType)
	p.RoundingMode = model.RoundingMode(roundingMode)
	p.RoundingScope = model.RoundingScope(roundingScope)
	p.PeriodMode = model.PeriodMode(periodMode)
	p.LegacyType = model.ProjectType(legacyType)
	p.Enabled = enabled != 0
	return p, nil
}

func (s *Store) SetBillingProfile(p model.BillingProfile) (model.BillingProfile, error) {
	if !p.ScopeType.Valid() {
		return model.BillingProfile{}, fmt.Errorf("invalid billing scope %q", p.ScopeType)
	}
	if !p.RoundingMode.Valid() {
		return model.BillingProfile{}, fmt.Errorf("invalid rounding mode %q", p.RoundingMode)
	}
	if !p.RoundingScope.Valid() {
		return model.BillingProfile{}, fmt.Errorf("invalid rounding scope %q", p.RoundingScope)
	}
	if !p.PeriodMode.Valid() {
		return model.BillingProfile{}, fmt.Errorf("invalid period mode %q", p.PeriodMode)
	}
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Currency == "" {
		p.Currency = "USD"
	}
	if len(p.Currency) != 3 || p.Currency[0] < 'A' || p.Currency[0] > 'Z' || p.Currency[1] < 'A' || p.Currency[1] > 'Z' || p.Currency[2] < 'A' || p.Currency[2] > 'Z' {
		return model.BillingProfile{}, errors.New("currency must be a three-letter code")
	}
	if p.HourlyRateMinor < 0 {
		return model.BillingProfile{}, errors.New("hourly rate minor must be >= 0")
	}
	if p.RoundingIncrementMinutes <= 0 || p.RoundingIncrementMinutes > 1440 {
		return model.BillingProfile{}, errors.New("rounding increment minutes must be from 1 to 1440")
	}
	if p.ScopeType == model.BillingScopeGlobal {
		p.ScopeKey = ""
	}
	if p.ScopeType != model.BillingScopeGlobal && p.ScopeKey == "" {
		return model.BillingProfile{}, errors.New("scope key is required")
	}
	now := time.Now().UTC()
	workItemID := any(nil)
	if p.ScopeType == model.BillingScopeWorkItem {
		id, parseErr := strconv.ParseInt(p.ScopeKey, 10, 64)
		if parseErr != nil || id <= 0 {
			return model.BillingProfile{}, errors.New("work_item scope key must be a positive item ID")
		}
		if _, itemErr := s.GetWorkItem(id); itemErr != nil {
			return model.BillingProfile{}, itemErr
		}
		workItemID = id
	}
	_, err := s.db.Exec(`INSERT INTO billing_profiles (work_item_id, scope_type, scope_key, enabled, currency, hourly_rate_minor, rounding_mode, rounding_increment_minutes, rounding_scope, period_mode, period_anchor, legacy_type, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)
ON CONFLICT(scope_type, scope_key) DO UPDATE SET work_item_id=excluded.work_item_id, enabled=excluded.enabled, currency=excluded.currency, hourly_rate_minor=excluded.hourly_rate_minor, rounding_mode=excluded.rounding_mode, rounding_increment_minutes=excluded.rounding_increment_minutes, rounding_scope=excluded.rounding_scope, period_mode=excluded.period_mode, period_anchor=excluded.period_anchor, legacy_type=COALESCE(excluded.legacy_type, billing_profiles.legacy_type), updated_at=excluded.updated_at`,
		workItemID, string(p.ScopeType), p.ScopeKey, p.Enabled, p.Currency, p.HourlyRateMinor, string(p.RoundingMode), p.RoundingIncrementMinutes, string(p.RoundingScope), string(p.PeriodMode), p.PeriodAnchor, string(p.LegacyType), now, now)
	if err != nil {
		return model.BillingProfile{}, err
	}
	return s.GetBillingProfile(p.ScopeType, p.ScopeKey)
}

func (s *Store) GetBillingProfile(scope model.BillingScopeType, key string) (model.BillingProfile, error) {
	if scope == model.BillingScopeGlobal {
		key = ""
	}
	p, err := scanBillingProfile(s.db.QueryRow(`SELECT `+billingCols+` FROM billing_profiles WHERE scope_type = ? AND scope_key = ?`, string(scope), key))
	if errors.Is(err, sql.ErrNoRows) {
		return model.BillingProfile{}, ErrNotFound
	}
	return p, err
}

type ResolvedBillingProfile struct {
	Profile       model.BillingProfile `json:"profile"`
	InheritedFrom string               `json:"inherited_from"`
}

func (s *Store) ResolveBillingProfile(item model.WorkItem) (ResolvedBillingProfile, error) {
	if p, err := s.GetBillingProfile(model.BillingScopeWorkItem, strconv.FormatInt(item.ID, 10)); err == nil {
		return ResolvedBillingProfile{p, "work_item"}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ResolvedBillingProfile{}, err
	}
	if item.Context != "" {
		if p, err := s.GetBillingProfile(model.BillingScopeClient, item.Context); err == nil {
			return ResolvedBillingProfile{p, "client"}, nil
		} else if !errors.Is(err, ErrNotFound) {
			return ResolvedBillingProfile{}, err
		}
	}
	if p, err := s.GetBillingProfile(model.BillingScopeGlobal, ""); err == nil {
		return ResolvedBillingProfile{p, "global"}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ResolvedBillingProfile{}, err
	}
	p := model.DefaultBillingProfile()
	return ResolvedBillingProfile{p, "default"}, nil
}

func (s *Store) SaveReportSnapshot(snapshot model.ReportSnapshot) (model.ReportSnapshot, error) {
	if !snapshot.PeriodMode.Valid() || !snapshot.To.After(snapshot.From) {
		return model.ReportSnapshot{}, errors.New("valid period and half-open range are required")
	}
	snapshot.Label = strings.TrimSpace(snapshot.Label)
	if snapshot.Label == "" {
		return model.ReportSnapshot{}, errors.New("snapshot label is required")
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(snapshot.Timezone); err != nil {
		return model.ReportSnapshot{}, fmt.Errorf("invalid snapshot timezone: %w", err)
	}
	if !json.Valid(snapshot.Payload) {
		return model.ReportSnapshot{}, errors.New("snapshot payload must be valid JSON")
	}
	res, err := s.db.Exec(`INSERT INTO report_snapshots(label, period_mode, from_time, to_time, timezone, payload) VALUES (?, ?, ?, ?, ?, ?)`, snapshot.Label, string(snapshot.PeriodMode), snapshot.From.UTC(), snapshot.To.UTC(), snapshot.Timezone, string(snapshot.Payload))
	if err != nil {
		return model.ReportSnapshot{}, err
	}
	snapshot.ID, _ = res.LastInsertId()
	return s.GetReportSnapshot(snapshot.ID)
}

func (s *Store) GetReportSnapshot(id int64) (model.ReportSnapshot, error) {
	var snapshot model.ReportSnapshot
	var period, payload string
	err := s.db.QueryRow(`SELECT id,label,period_mode,from_time,to_time,timezone,payload,created_at FROM report_snapshots WHERE id=?`, id).Scan(&snapshot.ID, &snapshot.Label, &period, &snapshot.From, &snapshot.To, &snapshot.Timezone, &payload, &snapshot.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ReportSnapshot{}, ErrNotFound
	}
	if err != nil {
		return model.ReportSnapshot{}, err
	}
	snapshot.PeriodMode = model.PeriodMode(period)
	snapshot.Payload = json.RawMessage(payload)
	return snapshot, nil
}

func (s *Store) ListReportSnapshots() ([]model.ReportSnapshot, error) {
	rows, err := s.db.Query(`SELECT id,label,period_mode,from_time,to_time,timezone,payload,created_at FROM report_snapshots ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ReportSnapshot
	for rows.Next() {
		var snapshot model.ReportSnapshot
		var period, payload string
		if err := rows.Scan(&snapshot.ID, &snapshot.Label, &period, &snapshot.From, &snapshot.To, &snapshot.Timezone, &payload, &snapshot.CreatedAt); err != nil {
			return nil, err
		}
		snapshot.PeriodMode = model.PeriodMode(period)
		snapshot.Payload = json.RawMessage(payload)
		out = append(out, snapshot)
	}
	return out, rows.Err()
}
