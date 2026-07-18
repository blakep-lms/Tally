package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

// browserApps are macOS application names treated as web browsers, so their
// window events can be enriched with the focused tab URL from a web watcher.
var browserApps = map[string]bool{
	"google chrome": true, "chrome": true, "safari": true,
	"firefox": true, "arc": true, "brave browser": true,
	"brave": true, "microsoft edge": true, "vivaldi": true, "chromium": true,
}

// AW is an ActivityWatch capture provider talking to the local REST API.
type AW struct {
	BaseURL string
	client  *http.Client
}

// NewAW builds a provider for the given base URL (e.g. http://localhost:5600).
func NewAW(baseURL string) *AW {
	return &AW{
		BaseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Name implements Provider.
func (a *AW) Name() string { return "activitywatch" }

// Available implements Provider by hitting the server info endpoint.
func (a *AW) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/api/0/info", nil)
	if err != nil {
		return false
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type awEvent struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Duration  float64        `json:"duration"`
	Data      map[string]any `json:"data"`
}

func (e awEvent) end() time.Time {
	return e.Timestamp.Add(time.Duration(e.Duration * float64(time.Second)))
}

func (e awEvent) str(key string) string {
	if v, ok := e.Data[key].(string); ok {
		return v
	}
	return ""
}

type bucket struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func (a *AW) buckets(ctx context.Context) (map[string]bucket, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/api/0/buckets/", nil)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activitywatch buckets: status %d", resp.StatusCode)
	}
	var out map[string]bucket
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *AW) events(ctx context.Context, bucketID string, from, to time.Time) ([]awEvent, error) {
	u := fmt.Sprintf("%s/api/0/buckets/%s/events?start=%s&end=%s",
		a.BaseURL, bucketID,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("activitywatch events %s: status %d", bucketID, resp.StatusCode)
	}
	var out []awEvent
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// interval is a half-open time span [start, end).
type interval struct{ start, end time.Time }

// notAFK builds the set of active (not-afk) intervals from afk events.
func notAFK(afk []awEvent) []interval {
	var out []interval
	for _, e := range afk {
		if e.str("status") == "not-afk" {
			out = append(out, interval{e.Timestamp, e.end()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start.Before(out[j].start) })
	return out
}

// activeOverlap returns how many seconds of [s,e) fall within active spans.
// When there are no afk events at all, the time is assumed active.
func activeOverlap(s, e time.Time, active []interval, haveAFK bool) float64 {
	if !haveAFK {
		return e.Sub(s).Seconds()
	}
	var total float64
	for _, iv := range active {
		os := maxTime(s, iv.start)
		oe := minTime(e, iv.end)
		if oe.After(os) {
			total += oe.Sub(os).Seconds()
		}
	}
	return total
}

// Pull implements Provider: it discovers window/afk/web buckets, subtracts AFK
// time, enriches browser events with tab URLs, and extracts repo/domain
// signals. Each returned event has a stable SourceKey for idempotent syncs.
func (a *AW) Pull(ctx context.Context, from, to time.Time) ([]model.Event, error) {
	bks, err := a.buckets(ctx)
	if err != nil {
		return nil, err
	}
	var windowBucket, afkBucket string
	var webBuckets []string
	for id, b := range bks {
		switch {
		case strings.HasPrefix(id, "aw-watcher-window") || b.Type == "currentwindow":
			windowBucket = id
		case strings.HasPrefix(id, "aw-watcher-afk") || b.Type == "afkstatus":
			afkBucket = id
		case strings.HasPrefix(id, "aw-watcher-web") || b.Type == "web.tab.current":
			webBuckets = append(webBuckets, id)
		}
	}
	if windowBucket == "" {
		return nil, fmt.Errorf("no aw-watcher-window bucket found; is the window watcher running?")
	}

	windowEvents, err := a.events(ctx, windowBucket, from, to)
	if err != nil {
		return nil, err
	}
	var afkEvents []awEvent
	if afkBucket != "" {
		if afkEvents, err = a.events(ctx, afkBucket, from, to); err != nil {
			return nil, err
		}
	}
	var webEvents []awEvent
	for _, wb := range webBuckets {
		evs, err := a.events(ctx, wb, from, to)
		if err != nil {
			return nil, err
		}
		webEvents = append(webEvents, evs...)
	}

	active := notAFK(afkEvents)
	haveAFK := len(afkEvents) > 0
	sort.Slice(webEvents, func(i, j int) bool {
		return webEvents[i].Timestamp.Before(webEvents[j].Timestamp)
	})

	out := make([]model.Event, 0, len(windowEvents))
	for _, w := range windowEvents {
		secs := activeOverlap(w.Timestamp, w.end(), active, haveAFK)
		if secs < 1 {
			continue // fully idle or negligible
		}
		app := w.str("app")
		title := w.str("title")
		ev := model.Event{
			Start:     w.Timestamp,
			Duration:  secs,
			App:       app,
			Title:     title,
			SourceKey: fmt.Sprintf("aw:%s:%d", windowBucket, w.ID),
		}
		if browserApps[strings.ToLower(strings.TrimSpace(app))] {
			if web := bestWebOverlap(w, webEvents); web != nil {
				ev.URL = web.str("url")
				if t := web.str("title"); t != "" {
					ev.Title = t
				}
			}
		}
		ev.Repo = Repo(ev.Title)
		if ev.URL != "" {
			// Store the domain in Repo's sibling: rules match url on domain-ish
			// substrings, so keep the full URL but ensure domain is derivable.
			// The rule engine matches against the URL field directly.
		}
		out = append(out, ev)
	}
	return out, nil
}

// bestWebOverlap returns the web event with the greatest temporal overlap with
// the window event, or nil when none overlap.
func bestWebOverlap(w awEvent, web []awEvent) *awEvent {
	var best *awEvent
	var bestOverlap float64
	ws, we := w.Timestamp, w.end()
	for i := range web {
		os := maxTime(ws, web[i].Timestamp)
		oe := minTime(we, web[i].end())
		if oe.After(os) {
			if ov := oe.Sub(os).Seconds(); ov > bestOverlap {
				bestOverlap = ov
				best = &web[i]
			}
		}
	}
	return best
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
