package classify

import (
	"regexp"
	"strings"
	"sync"

	"github.com/blakep-lms/tally/internal/model"
)

type Match struct {
	WorkItemID int64
	ProjectID  int64 // legacy alias
	RuleID     int64
	Source     string
}
type Engine struct {
	rules   []model.Rule
	mu      sync.Mutex
	reCache map[string]*regexp.Regexp
}

func NewEngine(rules []model.Rule) *Engine {
	return &Engine{rules: rules, reCache: map[string]*regexp.Regexp{}}
}
func (en *Engine) Classify(e model.Event) (Match, bool) {
	for _, r := range en.rules {
		id := r.WorkItemID
		if id == 0 && r.ProjectID != 0 {
			id = r.ProjectID
		}
		if en.ruleMatches(r, e) {
			return Match{WorkItemID: id, ProjectID: id, RuleID: r.ID, Source: "rule"}, true
		}
	}
	return Match{}, false
}
func (en *Engine) fieldValue(f model.RuleField, e model.Event) string {
	switch f {
	case model.FieldApp:
		return e.App
	case model.FieldTitle:
		return e.Title
	case model.FieldURL:
		return e.URL
	case model.FieldRepo:
		return e.Repo
	}
	return ""
}
func (en *Engine) ruleMatches(r model.Rule, e model.Event) bool {
	val := en.fieldValue(r.Field, e)
	if val == "" {
		return false
	}
	switch r.Match {
	case model.MatchContains:
		return strings.Contains(strings.ToLower(val), strings.ToLower(r.Pattern))
	case model.MatchEquals:
		return strings.EqualFold(val, r.Pattern)
	case model.MatchRegex:
		re := en.compile(r.Pattern)
		return re != nil && re.MatchString(val)
	}
	return false
}
func (en *Engine) compile(pattern string) *regexp.Regexp {
	en.mu.Lock()
	defer en.mu.Unlock()
	if re, ok := en.reCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		en.reCache[pattern] = nil
		return nil
	}
	en.reCache[pattern] = re
	return re
}
