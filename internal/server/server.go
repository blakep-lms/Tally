// Package server hosts Tally's local dashboard: a JSON API over the core
// service plus an embedded static single-page app. It runs fully offline and
// binds to localhost only.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/report"
)

//go:embed web/*
var webFS embed.FS

// Server wires the core service to HTTP handlers.
type Server struct {
	app       *core.App
	mux       *http.ServeMux
	token     string
	sessions  map[string]browserSession
	sessionMu sync.Mutex
}

type browserSession struct {
	csrf      string
	expiresAt time.Time
}

const browserSessionLifetime = 12 * time.Hour

type Options struct{ Token string }

func NewWithOptions(app *core.App, opts Options) *Server {
	s := &Server{app: app, mux: http.NewServeMux(), token: opts.Token, sessions: map[string]browserSession{}}
	s.routes()
	return s
}

// New builds the HTTP server.
func New(app *core.App) *Server { return NewWithOptions(app, Options{Token: app.Cfg.APIToken()}) }

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !loopbackHost(r.Host) {
				writeErr(w, http.StatusForbidden, errors.New("API host must be loopback"))
				return
			}
			if r.URL.Path != "/api/session" && s.token != "" && !s.authorizedRead(r) {
				writeErr(w, http.StatusUnauthorized, errors.New("missing or invalid API authorization"))
				return
			}
			if isMutation(r.Method) {
				if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
					writeErr(w, http.StatusForbidden, errors.New("cross-origin write rejected"))
					return
				}
				if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
					writeErr(w, http.StatusUnsupportedMediaType, errors.New("writes require application/json"))
					return
				}
				if !s.authorizedWrite(r) {
					writeErr(w, http.StatusUnauthorized, errors.New("missing or invalid write authorization"))
					return
				}
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func sameOrigin(rawOrigin, host string) bool {
	u, err := url.Parse(rawOrigin)
	return err == nil && strings.EqualFold(u.Host, host) && (u.Scheme == "http" || u.Scheme == "https")
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func secureEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) authorizedBearer(r *http.Request) bool {
	return s.token != "" && secureEqual(r.Header.Get("Authorization"), "Bearer "+s.token)
}

func (s *Server) sessionCSRF(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("tally_session")
	if err != nil {
		return "", false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session, ok := s.sessions[cookie.Value]
	if !ok || time.Now().After(session.expiresAt) {
		delete(s.sessions, cookie.Value)
		return "", false
	}
	return session.csrf, true
}

func (s *Server) authorizedRead(r *http.Request) bool {
	if s.authorizedBearer(r) {
		return true
	}
	_, ok := s.sessionCSRF(r)
	return ok
}

func (s *Server) authorizedWrite(r *http.Request) bool {
	if s.authorizedBearer(r) {
		return true
	}
	csrf, ok := s.sessionCSRF(r)
	return ok && secureEqual(r.Header.Get("X-Tally-CSRF"), csrf)
}

func (s *Server) routes() {
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/items", s.handleListWorkItems)
	s.mux.HandleFunc("POST /api/items", s.handleCreateWorkItem)
	s.mux.HandleFunc("PUT /api/items/{id}", s.handleUpdateWorkItem)
	s.mux.HandleFunc("POST /api/items/{id}/done", s.handleMarkWorkItemDone)
	s.mux.HandleFunc("POST /api/items/{id}/reactivate", s.handleReactivateWorkItem)
	s.mux.HandleFunc("GET /api/work-items", s.handleListWorkItems)
	s.mux.HandleFunc("POST /api/work-items", s.handleCreateWorkItem)
	s.mux.HandleFunc("PUT /api/work-items/{id}", s.handleUpdateWorkItem)
	s.mux.HandleFunc("POST /api/work-items/{id}/done", s.handleMarkWorkItemDone)
	s.mux.HandleFunc("POST /api/work-items/{id}/reactivate", s.handleReactivateWorkItem)
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("POST /api/projects/{id}/done", s.handleMarkDone)
	s.mux.HandleFunc("GET /api/rules", s.handleListRules)
	s.mux.HandleFunc("POST /api/rules", s.handleCreateRule)
	s.mux.HandleFunc("DELETE /api/rules/{id}", s.handleDeleteRule)
	s.mux.HandleFunc("GET /api/unclassified", s.handleUnclassified)
	s.mux.HandleFunc("POST /api/events/{id}/assign", s.handleAssign)
	s.mux.HandleFunc("POST /api/classify", s.handleClassify)
	s.mux.HandleFunc("POST /api/sync", s.handleSync)
	s.mux.HandleFunc("GET /api/report", s.handleReport)
	s.mux.HandleFunc("GET /api/billing/profile", s.handleGetBillingProfile)
	s.mux.HandleFunc("PUT /api/billing/profile", s.handleSetBillingProfile)
	s.mux.HandleFunc("GET /api/billing/snapshots", s.handleListSnapshots)
	s.mux.HandleFunc("GET /api/billing/snapshots/{id}", s.handleGetSnapshot)
	s.mux.HandleFunc("POST /api/billing/snapshots", s.handleFinalizeSnapshot)
	s.mux.HandleFunc("GET /api/audit", s.handleAudit)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.token != "" && !s.authorizedBearer(r) {
		writeErr(w, http.StatusUnauthorized, errors.New("bearer token required to create a browser session"))
		return
	}
	sessionID := randomSecret()
	csrf := randomSecret()
	now := time.Now()
	s.sessionMu.Lock()
	for id, session := range s.sessions {
		if now.After(session.expiresAt) {
			delete(s.sessions, id)
		}
	}
	s.sessions[sessionID] = browserSession{csrf: csrf, expiresAt: now.Add(browserSessionLifetime)}
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "tally_session", Value: sessionID, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(browserSessionLifetime.Seconds())})
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": csrf})
}

func randomSecret() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic("generate Tally session secret: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(secret)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func (s *Server) handleListWorkItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListWorkItems(model.WorkItemStatus(r.URL.Query().Get("status")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) handleCreateWorkItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Context     string `json:"context"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.app.AddWorkItem(body.Name, model.WorkItemKind(body.Kind), body.Context, body.Description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (s *Server) handleUpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cur, err := s.app.Store.GetWorkItem(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Kind        *string `json:"kind"`
		Context     *string `json:"context"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name, kind, context, description := cur.Name, string(cur.Kind), cur.Context, cur.Description
	if body.Name != nil {
		name = *body.Name
	}
	if body.Kind != nil {
		kind = *body.Kind
	}
	if body.Context != nil {
		context = *body.Context
	}
	if body.Description != nil {
		description = *body.Description
	}
	item, err := s.app.UpdateWorkItem(id, name, model.WorkItemKind(kind), context, description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) handleMarkWorkItemDone(w http.ResponseWriter, r *http.Request) {
	item, err := s.app.MarkWorkItemDone(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) handleReactivateWorkItem(w http.ResponseWriter, r *http.Request) {
	item, err := s.app.ReactivateWorkItem(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.app.Status(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	status := model.ProjectStatus(r.URL.Query().Get("status"))
	projects, err := s.app.ListProjects(status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.app.AddProject(body.Name, model.ProjectType(body.Type), body.Client)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleMarkDone(w http.ResponseWriter, r *http.Request) {
	p, err := s.app.MarkDone(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.app.ListRules(r.URL.Query().Get("active") == "true")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Project  string `json:"project"`
		Field    string `json:"field"`
		Match    string `json:"match"`
		Pattern  string `json:"pattern"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Match == "" {
		body.Match = string(model.MatchContains)
	}
	rule, err := s.app.AddRule(body.Project, model.RuleField(body.Field),
		model.MatchKind(body.Match), body.Pattern, body.Priority)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.app.DeleteRule(id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUnclassified(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 1000 {
			writeErr(w, http.StatusBadRequest, errors.New("limit must be an integer from 1 to 1000"))
			return
		}
		limit = n
	}
	events, err := s.app.ListUnclassified(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Project   string `json:"project"`
		MakeRule  bool   `json:"make_rule"`
		RuleField string `json:"rule_field"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	field := model.RuleField(body.RuleField)
	if field == "" {
		field = model.FieldTitle
	}
	rule, created, err := s.app.AssignEvent(id, body.Project, body.MakeRule, field)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	resp := map[string]any{"ok": true, "rule_created": created}
	if created {
		resp["rule"] = rule
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleClassify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LLM bool `json:"llm"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.app.Classify(r.Context(), body.LLM)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	from, to := core.TodayRange(time.Now())
	// Default to the last 24h if the window is empty; allow overrides.
	from = to.AddDate(0, 0, -1)
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.From != "" {
		t, err := time.Parse(time.RFC3339, body.From)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid from: %w", err))
			return
		}
		from = t
	}
	if body.To != "" {
		t, err := time.Parse(time.RFC3339, body.To)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid to: %w", err))
			return
		}
		to = t
	}
	if !to.After(from) {
		writeErr(w, http.StatusBadRequest, errors.New("to must be after from"))
		return
	}
	res, err := s.app.Sync(r.Context(), from, to)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	rep, _, _, err := s.buildReport(r.URL.Query())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) buildReport(query url.Values) (report.Report, model.PeriodMode, string, error) {
	timezone := query.Get("timezone")
	if timezone == "" {
		timezone = time.Local.String()
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return report.Report{}, "", timezone, err
	}
	now := time.Now().In(loc)
	rangeName := strings.ToLower(query.Get("range"))
	if rangeName != "" && rangeName != "today" && rangeName != "week" && rangeName != "month" && rangeName != "all" {
		return report.Report{}, "", timezone, fmt.Errorf("invalid range %q", rangeName)
	}
	mode := model.PeriodMode(query.Get("period"))
	if mode == "" {
		switch rangeName {
		case "today", "all":
			mode = model.PeriodCustom
		case "month":
			mode = model.PeriodMonthly
		default:
			mode = model.PeriodWeekly
		}
	}
	if !mode.Valid() {
		return report.Report{}, mode, timezone, errors.New("invalid period")
	}
	var from, to time.Time
	switch rangeName {
	case "today":
		from, to = core.TodayRange(now)
	case "all":
		to = now.AddDate(0, 0, 1)
	default:
		from, to = core.PeriodRange(mode, "", now)
	}
	fromText, toText := query.Get("from"), query.Get("to")
	if (fromText == "") != (toText == "") {
		return report.Report{}, mode, timezone, errors.New("from and to are required together")
	}
	if value := fromText; value != "" {
		from, err = time.ParseInLocation("2006-01-02", value, loc)
		if err != nil {
			return report.Report{}, mode, timezone, err
		}
		mode = model.PeriodCustom
	}
	if value := toText; value != "" {
		end, parseErr := time.ParseInLocation("2006-01-02", value, loc)
		if parseErr != nil {
			return report.Report{}, mode, timezone, parseErr
		}
		to = end.AddDate(0, 0, 1)
		mode = model.PeriodCustom
	}
	if mode == model.PeriodCustom && fromText == "" && rangeName != "today" && rangeName != "all" {
		return report.Report{}, mode, timezone, errors.New("custom period requires from and to")
	}
	billing := query.Get("billing") == "true"
	if mode == model.PeriodFinal {
		if query.Get("item") == "" {
			return report.Report{}, mode, timezone, errors.New("final period requires item")
		}
		item, resolveErr := s.app.ResolveWorkItem(query.Get("item"))
		if resolveErr != nil {
			return report.Report{}, mode, timezone, resolveErr
		}
		from, to = core.FinalRange(item, now)
		rep, reportErr := s.app.ReportWorkItemWithBilling(item, from, to, billing)
		return rep, mode, timezone, reportErr
	}
	if !to.After(from) {
		return report.Report{}, mode, timezone, errors.New("report range must be non-empty")
	}
	rep, err := s.app.ReportWithBilling(from, to, billing)
	return rep, mode, timezone, err
}

func (s *Server) handleGetBillingProfile(w http.ResponseWriter, r *http.Request) {
	if item := r.URL.Query().Get("work_item"); item != "" {
		res, err := s.app.ResolveBillingProfile(item)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	scope := model.BillingScopeType(r.URL.Query().Get("scope_type"))
	if scope == "" {
		scope = model.BillingScopeGlobal
	}
	p, err := s.app.Store.GetBillingProfile(scope, r.URL.Query().Get("scope_key"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
func (s *Server) handleSetBillingProfile(w http.ResponseWriter, r *http.Request) {
	var patch model.BillingProfilePatch
	if err := decodeJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.app.PatchBillingProfile(patch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := s.app.Store.ListReportSnapshots()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}
func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	snap, err := s.app.Store.GetReportSnapshot(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleFinalizeSnapshot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label    string `json:"label"`
		Period   string `json:"period"`
		Timezone string `json:"timezone"`
		Item     string `json:"item"`
		From     string `json:"from"`
		To       string `json:"to"`
		Billing  bool   `json:"billing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	query := url.Values{}
	query.Set("period", body.Period)
	query.Set("timezone", body.Timezone)
	query.Set("item", body.Item)
	query.Set("from", body.From)
	query.Set("to", body.To)
	if body.Billing {
		query.Set("billing", "true")
	}
	rep, period, timezone, err := s.buildReport(query)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	snapshot, err := s.app.FinalizeReport(rep, body.Label, period, timezone)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, snapshot)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	var eventID int64
	if raw := r.URL.Query().Get("event_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		eventID = id
	}
	audit, err := s.app.Store.ListClassificationAudit(eventID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, audit)
}
