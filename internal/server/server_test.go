package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/store"
)

type apiHarness struct {
	app    *core.App
	server *httptest.Server
	client *http.Client
	csrf   string
}

func newHarness(t *testing.T) *apiHarness {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	app := core.New(st, config.Defaults())
	srv := httptest.NewServer(New(app).Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(srv.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var session map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return &apiHarness{app: app, server: srv, client: client, csrf: session["csrf_token"]}
}

func (h *apiHarness) write(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tally-CSRF", h.csrf)
	req.Header.Set("Origin", h.server.URL)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestWritesRequireSessionCSRFAndSameOriginEvenWithoutConfiguredToken(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Post(h.server.URL+"/api/items", "application/json", strings.NewReader(`{"name":"blocked","kind":"project"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized write status=%d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/items", strings.NewReader(`{"name":"evil","kind":"project"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tally-CSRF", h.csrf)
	req.Header.Set("Origin", "https://evil.example")
	resp, err = h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin write status=%d", resp.StatusCode)
	}
	resp = h.write(t, http.MethodPost, "/api/items", `{"name":"allowed","kind":"project"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authorized write status=%d", resp.StatusCode)
	}
}

func TestConfiguredBearerAllowsLocalAPIClientWrite(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	app := core.New(st, config.Defaults())
	srv := httptest.NewServer(NewWithOptions(app, Options{Token: "secret"}).Handler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/items", strings.NewReader(`{"name":"Bearer","kind":"goal"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("bearer write status=%d", resp.StatusCode)
	}
}

func TestConfiguredTokenGatesSessionAndAllAPIReads(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	app := core.New(st, config.Defaults())
	srv := httptest.NewServer(NewWithOptions(app, Options{Token: "secret"}).Handler())
	defer srv.Close()

	for _, path := range []string{"/api/session", "/api/items", "/api/report?range=all", "/api/audit"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated GET %s status=%d, want 401", path, resp.StatusCode)
		}
	}
}

func TestBearerExchangeIssuesAuthenticatedSessionWithPerSessionCSRF(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	app := core.New(st, config.Defaults())
	srv := httptest.NewServer(NewWithOptions(app, Options{Token: "secret"}).Handler())
	defer srv.Close()

	issue := func() (*http.Client, string) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/session", nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session exchange status=%d", resp.StatusCode)
		}
		var session map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
			t.Fatal(err)
		}
		return client, session["csrf_token"]
	}

	clientA, csrfA := issue()
	_, csrfB := issue()
	if csrfA == "" || csrfA == csrfB {
		t.Fatalf("csrf tokens must be non-empty and per-session: a=%q b=%q", csrfA, csrfB)
	}
	resp, err := clientA.Get(srv.URL + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated session read status=%d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/items", strings.NewReader(`{"name":"Session","kind":"goal"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tally-CSRF", csrfA)
	resp, err = clientA.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated session write status=%d", resp.StatusCode)
	}
}

func TestFullAPIWorkItemsBillingReportsSnapshotsAndClearFields(t *testing.T) {
	h := newHarness(t)
	resp := h.write(t, http.MethodPost, "/api/items", `{"name":"Web","kind":"product","context":"ACME","description":"launch"}`)
	var item model.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if item.Kind != model.KindProduct {
		t.Fatalf("created item=%+v", item)
	}
	_, _ = h.app.Store.UpsertEvent(model.Event{Start: time.Now().Add(-time.Minute), Duration: 60, WorkItemID: &item.ID, SourceKey: "api-test"})
	resp = h.write(t, http.MethodPut, "/api/items/"+jsonNumber(item.ID), `{"context":"","description":""}`)
	var updated model.WorkItem
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.Context != "" || updated.Description != "" {
		t.Fatalf("fields not cleared: %+v", updated)
	}
	profile := `{"scope_type":"work_item","scope_key":"` + jsonNumber(item.ID) + `","enabled":true,"currency":"USD","hourly_rate_minor":15000,"rounding_mode":"up","rounding_increment_minutes":15,"rounding_scope":"period_work_item","period_mode":"custom"}`
	resp = h.write(t, http.MethodPut, "/api/billing/profile", profile)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("billing profile status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, err := h.client.Get(h.server.URL + "/api/report?range=all&billing=true")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&report)
	resp.Body.Close()
	if _, ok := report["totals_by_currency"]; !ok {
		t.Fatalf("billing report=%v", report)
	}
	resp = h.write(t, http.MethodPost, "/api/billing/snapshots", `{"label":"July","period":"custom","timezone":"UTC","from":"2026-07-01","to":"2026-07-31","billing":true}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("snapshot status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, _ = h.client.Get(h.server.URL + "/api/billing/snapshots")
	var snapshots []model.ReportSnapshot
	_ = json.NewDecoder(resp.Body).Decode(&snapshots)
	resp.Body.Close()
	if len(snapshots) != 1 || snapshots[0].Label != "July" {
		t.Fatalf("snapshots=%+v", snapshots)
	}
	resp, _ = h.client.Get(h.server.URL + "/api/audit")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit status=%d", resp.StatusCode)
	}
}

func TestBillingProfilePartialUpdatePreservesOmittedFields(t *testing.T) {
	h := newHarness(t)
	resp := h.write(t, http.MethodPost, "/api/items", `{"name":"Partial billing","kind":"project"}`)
	var item model.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	full := `{"scope_type":"work_item","scope_key":"` + jsonNumber(item.ID) + `","enabled":true,"currency":"EUR","hourly_rate_minor":27500,"rounding_mode":"up","rounding_increment_minutes":30,"rounding_scope":"period_work_item","period_mode":"monthly"}`
	resp = h.write(t, http.MethodPut, "/api/billing/profile", full)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create billing profile status=%d", resp.StatusCode)
	}
	patchBody := `{"scope_type":"work_item","scope_key":"` + jsonNumber(item.ID) + `","enabled":false}`
	resp = h.write(t, http.MethodPut, "/api/billing/profile", patchBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("partial billing profile status=%d", resp.StatusCode)
	}
	var got model.BillingProfile
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Currency != "EUR" || got.HourlyRateMinor != 27500 || got.RoundingIncrementMinutes != 30 || got.PeriodMode != model.PeriodMonthly {
		t.Fatalf("partial update lost fields: %+v", got)
	}
}

func TestMalformedMutationBodiesAndRangesReturnBadRequest(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/classify", `{`},
		{"/api/sync", `{`},
		{"/api/sync", `{"from":"not-a-time"}`},
		{"/api/sync", `{"from":"2026-07-25T00:00:00Z","to":"2026-07-24T00:00:00Z"}`},
	} {
		resp := h.write(t, http.MethodPost, tc.path, tc.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s body=%q status=%d, want 400", tc.path, tc.body, resp.StatusCode)
		}
	}
	for _, path := range []string{
		"/api/unclassified?limit=nope",
		"/api/unclassified?limit=-1",
		"/api/report?range=bogus",
		"/api/report?period=custom&from=2026-07-01",
		"/api/report?period=custom&to=2026-07-31",
	} {
		resp, err := h.client.Get(h.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d, want 400", path, resp.StatusCode)
		}
	}
}

func TestRejectsNonLoopbackAPIHost(t *testing.T) {
	harness := newHarness(t)
	h := New(harness.app).Handler()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/items", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status=%d", rec.Code)
	}
}

func TestServesSPA(t *testing.T) {
	h := newHarness(t)
	resp, err := h.client.Get(h.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	_, _ = resp.Body.Read(buf)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(buf), "<!DOCTYPE html>") {
		t.Fatalf("SPA status=%d body=%q", resp.StatusCode, string(buf))
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
