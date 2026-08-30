package capture

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAW stands up an ActivityWatch-shaped REST server for tests.
func fakeAW(t *testing.T) *httptest.Server {
	t.Helper()
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	ev := func(id int64, ts time.Time, dur float64, data map[string]any) map[string]any {
		return map[string]any{"id": id, "timestamp": ts.Format(time.RFC3339Nano), "duration": dur, "data": data}
	}
	window := []map[string]any{
		ev(1, base, 600, map[string]any{"app": "Code", "title": "tally — main"}),
		ev(2, base.Add(10*time.Minute), 600, map[string]any{"app": "Google Chrome", "title": "GitHub"}),
	}
	afk := []map[string]any{
		ev(1, base, 900, map[string]any{"status": "not-afk"}),
		ev(2, base.Add(15*time.Minute), 300, map[string]any{"status": "afk"}),
	}
	web := []map[string]any{
		ev(1, base.Add(10*time.Minute), 600, map[string]any{"url": "https://github.com/blakep-lms/tally", "title": "tally: PR #1"}),
	}
	buckets := map[string]bucket{
		"aw-watcher-window_host": {ID: "aw-watcher-window_host", Type: "currentwindow"},
		"aw-watcher-afk_host":    {ID: "aw-watcher-afk_host", Type: "afkstatus"},
		"aw-watcher-web-chrome":  {ID: "aw-watcher-web-chrome", Type: "web.tab.current"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/0/info", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/0/buckets/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/0/buckets/")
		if path == "" {
			json.NewEncoder(w).Encode(buckets)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "-1" {
			t.Errorf("events limit=%q want -1", got)
		}
		id := strings.TrimSuffix(path, "/events")
		var out []map[string]any
		switch {
		case strings.HasPrefix(id, "aw-watcher-window"):
			out = window
		case strings.HasPrefix(id, "aw-watcher-afk"):
			out = afk
		case strings.HasPrefix(id, "aw-watcher-web"):
			out = web
		}
		json.NewEncoder(w).Encode(out)
	})
	return httptest.NewServer(mux)
}

func TestAWPull(t *testing.T) {
	srv := fakeAW(t)
	defer srv.Close()
	aw := NewAW(srv.URL)
	ctx := context.Background()

	if !aw.Available(ctx) {
		t.Fatal("expected provider available")
	}
	from := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	events, err := aw.Pull(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}

	e1, e2 := events[0], events[1]
	if e1.Duration != 600 {
		t.Errorf("e1 duration = %v, want 600", e1.Duration)
	}
	if e1.Repo != "tally" {
		t.Errorf("e1 repo = %q, want tally", e1.Repo)
	}
	// Second window is AFK-clipped from 600s to 300s (only 10:10–10:15 is active).
	if e2.Duration != 300 {
		t.Errorf("e2 duration = %v, want 300 (AFK-subtracted)", e2.Duration)
	}
	// Browser window enriched from the web watcher.
	if e2.URL != "https://github.com" {
		t.Errorf("e2 url = %q", e2.URL)
	}
	if e2.Title != "tally: PR #1" {
		t.Errorf("e2 title = %q, want enriched web title", e2.Title)
	}
	if e2.SourceKey == "" || e1.SourceKey == e2.SourceKey {
		t.Errorf("expected distinct stable source keys, got %q / %q", e1.SourceKey, e2.SourceKey)
	}

	// Idempotency: a second pull yields identical source keys.
	again, err := aw.Pull(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].SourceKey != e1.SourceKey || again[1].SourceKey != e2.SourceKey {
		t.Error("source keys not stable across pulls")
	}
	if e1.SourceGroup == "" || e2.SourceGroup == "" || !e1.CaptureComplete || !e2.CaptureComplete {
		t.Fatalf("capture groups must be complete: %+v %+v", e1, e2)
	}
}

func TestActiveFragmentsPreserveTimelineGaps(t *testing.T) {
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	active := []interval{
		{start: base, end: base.Add(5 * time.Minute)},
		{start: base.Add(10 * time.Minute), end: base.Add(15 * time.Minute)},
	}
	got := activeFragments(base, base.Add(20*time.Minute), active, true)
	if len(got) != 2 || !got[0].start.Equal(base) || !got[1].start.Equal(base.Add(10*time.Minute)) {
		t.Fatalf("timeline fragments = %+v", got)
	}
}

func TestPrivacySanitizationBeforePersistence(t *testing.T) {
	if got := sanitizedURL("https://user:secret@example.com/client/path?token=abc#frag", false); got != "https://example.com" {
		t.Fatalf("origin-only URL = %q", got)
	}
	if got := sanitizedURL("https://example.com/client/path?token=abc#frag", true); got != "https://example.com/client/path" {
		t.Fatalf("path URL = %q", got)
	}
	ignored := map[string]bool{"1password": true}
	if !sensitiveWindow("1Password", "Vault", ignored) || !sensitiveWindow("Safari", "Private Browsing", ignored) {
		t.Fatal("sensitive windows must be filtered")
	}
}

func TestAWPullClipsToExactRequestedRange(t *testing.T) {
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	ev := func(id int64, ts time.Time, dur float64, data map[string]any) map[string]any {
		return map[string]any{"id": id, "timestamp": ts.Format(time.RFC3339Nano), "duration": dur, "data": data}
	}
	buckets := map[string]bucket{"aw-watcher-window_host": {ID: "aw-watcher-window_host", Type: "currentwindow"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/0/info", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/0/buckets/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/0/buckets/")
		if path == "" {
			json.NewEncoder(w).Encode(buckets)
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			ev(1, base.Add(-5*time.Minute), 10*60, map[string]any{"app": "Code", "title": "before overlaps"}),
			ev(2, base.Add(20*time.Minute), 5*60, map[string]any{"app": "Code", "title": "outside"}),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	events, err := NewAW(srv.URL).Pull(context.Background(), base, base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 clipped event, got %+v", events)
	}
	if !events[0].Start.Equal(base) || events[0].Duration != 300 {
		t.Fatalf("event not clipped to exact range: start=%s duration=%v", events[0].Start, events[0].Duration)
	}
}

func TestBucketDiscoveryUsesDeclaredType(t *testing.T) {
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/0/buckets/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/0/buckets/")
		if path == "" {
			_ = json.NewEncoder(w).Encode(map[string]bucket{"custom window": {ID: "custom window", Type: "currentwindow"}})
			return
		}
		if path != "custom window/events" {
			t.Errorf("event path=%q", path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "timestamp": base.Format(time.RFC3339Nano), "duration": 60, "data": map[string]any{"app": "Code", "title": "Tally"}}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	events, err := NewAW(srv.URL).Pull(context.Background(), base, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Duration != 60 {
		t.Fatalf("events=%+v", events)
	}
}

func TestAWPullIncludesAllHostnameWindowBuckets(t *testing.T) {
	base := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	buckets := map[string]bucket{
		"aw-watcher-window-old.local": {Type: "currentwindow", Hostname: "old.local"},
		"aw-watcher-window-new.local": {Type: "currentwindow", Hostname: "new.local"},
		"aw-watcher-afk-new.local":    {Type: "afkstatus", Hostname: "new.local"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/0/buckets/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/0/buckets/")
		if path == "" {
			_ = json.NewEncoder(w).Encode(buckets)
			return
		}
		id := strings.TrimSuffix(path, "/events")
		if strings.Contains(id, "afk") {
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 1, "timestamp": base.Add(time.Minute).Format(time.RFC3339Nano),
				"duration": 60, "data": map[string]any{"status": "not-afk"},
			}})
			return
		}
		title := "old event"
		minute := 0
		if strings.Contains(id, "new.local") {
			title = "new event"
			minute = 1
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 1, "timestamp": base.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339Nano),
			"duration": 60, "data": map[string]any{"app": "Code", "title": title},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	events, err := NewAW(srv.URL).Pull(context.Background(), base, base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want both hostname buckets, got %+v", events)
	}
	if events[0].Title != "old event" || events[1].Title != "new event" {
		t.Fatalf("events not stable across hostname buckets: %+v", events)
	}
	if events[0].SourceKey == events[1].SourceKey {
		t.Fatalf("source keys collided: %q", events[0].SourceKey)
	}
}
