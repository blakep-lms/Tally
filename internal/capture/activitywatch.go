package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	BaseURL      string
	client       *http.Client
	ignoredApps  map[string]bool
	storeURLPath bool
}

// NewAW builds a provider for the given base URL (e.g. http://localhost:5600).
func NewAW(baseURL string) *AW {
	return NewAWWithPrivacy(baseURL, nil, false)
}

// NewAWWithPrivacy configures pre-persistence filtering. App matching is
// case-insensitive. URL queries, fragments, and user info are always removed.
func NewAWWithPrivacy(baseURL string, ignoredApps []string, storeURLPath bool) *AW {
	ignored := make(map[string]bool, len(ignoredApps))
	for _, app := range ignoredApps {
		ignored[strings.ToLower(strings.TrimSpace(app))] = true
	}
	return &AW{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		client:       &http.Client{Timeout: 15 * time.Second},
		ignoredApps:  ignored,
		storeURLPath: storeURLPath,
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
	query := url.Values{}
	query.Set("start", from.UTC().Format(time.RFC3339))
	query.Set("end", to.UTC().Format(time.RFC3339))
	query.Set("limit", "-1")
	u := fmt.Sprintf("%s/api/0/buckets/%s/events?%s", a.BaseURL, url.PathEscape(bucketID), query.Encode())
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

func activeFragments(s, e time.Time, active []interval, haveAFK bool) []interval {
	if !haveAFK {
		return []interval{{start: s, end: e}}
	}
	var out []interval
	for _, iv := range active {
		start := maxTime(s, iv.start)
		end := minTime(e, iv.end)
		if !end.After(start) {
			continue
		}
		if len(out) > 0 && !start.After(out[len(out)-1].end) {
			if end.After(out[len(out)-1].end) {
				out[len(out)-1].end = end
			}
			continue
		}
		out = append(out, interval{start: start, end: end})
	}
	return out
}

func sanitizedURL(raw string, storePath bool) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	if !storePath {
		u.Path = ""
		u.RawPath = ""
	}
	return u.String()
}

func sensitiveWindow(app, title string, ignored map[string]bool) bool {
	if ignored[strings.ToLower(strings.TrimSpace(app))] {
		return true
	}
	t := strings.ToLower(title)
	return strings.Contains(t, "incognito") || strings.Contains(t, "private browsing") || strings.Contains(t, "private window")
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
		switch b.Type {
		case "currentwindow":
			windowBucket = id
		case "afkstatus":
			afkBucket = id
		case "web.tab.current":
			webBuckets = append(webBuckets, id)
		}
	}
	// Older ActivityWatch versions may omit bucket types. Use ID prefixes only
	// as a compatibility fallback, never in preference to declared types.
	for id, b := range bks {
		if b.Type != "" {
			continue
		}
		switch {
		case windowBucket == "" && strings.HasPrefix(id, "aw-watcher-window"):
			windowBucket = id
		case afkBucket == "" && strings.HasPrefix(id, "aw-watcher-afk"):
			afkBucket = id
		case strings.HasPrefix(id, "aw-watcher-web"):
			webBuckets = append(webBuckets, id)
		}
	}
	sort.Strings(webBuckets)
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
		clippedStart := maxTime(w.Timestamp, from)
		clippedEnd := minTime(w.end(), to)
		if !clippedEnd.After(clippedStart) {
			continue
		}
		app := w.str("app")
		title := w.str("title")
		group := fmt.Sprintf("aw:%s:%d", windowBucket, w.ID)
		fragments := activeFragments(clippedStart, clippedEnd, active, haveAFK)
		if sensitiveWindow(app, title, a.ignoredApps) || len(fragments) == 0 {
			out = append(out, model.Event{SourceGroup: group, CaptureComplete: true})
			continue
		}
		groupStart := len(out)
		for i, fragment := range fragments {
			if fragment.end.Sub(fragment.start) < time.Second {
				continue
			}
			key := group
			if len(fragments) > 1 {
				key = fmt.Sprintf("%s:part:%d", group, i)
			}
			ev := model.Event{
				Start: fragment.start, Duration: fragment.end.Sub(fragment.start).Seconds(),
				App: app, Title: title, SourceKey: key, SourceGroup: group,
			}
			segment := awEvent{Timestamp: fragment.start, Duration: fragment.end.Sub(fragment.start).Seconds()}
			if browserApps[strings.ToLower(strings.TrimSpace(app))] {
				if web := bestWebOverlap(segment, webEvents); web != nil {
					ev.URL = sanitizedURL(web.str("url"), a.storeURLPath)
					if t := web.str("title"); t != "" {
						ev.Title = t
					}
				}
			}
			if sensitiveWindow(ev.App, ev.Title, a.ignoredApps) {
				continue
			}
			ev.Repo = Repo(ev.Title)
			out = append(out, ev)
		}
		if len(out) == groupStart {
			out = append(out, model.Event{SourceGroup: group, CaptureComplete: true})
		} else {
			out[len(out)-1].CaptureComplete = true
		}
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
