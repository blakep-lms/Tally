// Package server hosts Tally's local dashboard: a JSON API over the core
// service plus an embedded static single-page app. It runs fully offline and
// binds to localhost only.
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
)

//go:embed web/*
var webFS embed.FS

// Server wires the core service to HTTP handlers.
type Server struct {
	app *core.App
	mux *http.ServeMux
}

// New builds the HTTP server.
func New(app *core.App) *Server {
	s := &Server{app: app, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	s.mux.HandleFunc("GET /api/status", s.handleStatus)
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
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
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
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
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
	json.NewDecoder(r.Body).Decode(&body)
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
	json.NewDecoder(r.Body).Decode(&body)
	if body.From != "" {
		if t, err := time.Parse(time.RFC3339, body.From); err == nil {
			from = t
		}
	}
	if body.To != "" {
		if t, err := time.Parse(time.RFC3339, body.To); err == nil {
			to = t
		}
	}
	res, err := s.app.Sync(r.Context(), from, to)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	from, to := resolveRange(r.URL.Query().Get("range"))
	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t2 := r.URL.Query().Get("to"); t2 != "" {
		if t, err := time.Parse("2006-01-02", t2); err == nil {
			to = t.AddDate(0, 0, 1)
		}
	}
	rep, err := s.app.Report(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func resolveRange(name string) (time.Time, time.Time) {
	now := time.Now()
	switch strings.ToLower(name) {
	case "today":
		return core.TodayRange(now)
	case "all", "":
		return time.Time{}, now.AddDate(0, 0, 1)
	default: // week
		return core.WeekRange(now)
	}
}
