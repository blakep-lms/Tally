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
	if e2.URL != "https://github.com/blakep-lms/tally" {
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
}
