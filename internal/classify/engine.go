// Package classify turns captured events into project attributions. It runs a
// deterministic, ordered rule engine first and can optionally fall back to an
// LLM for events no rule matches.
package classify

import (
	"regexp"
	"strings"
	"sync"

	"github.com/blakep-lms/tally/internal/model"
)

// Match is the outcome of classifying a single event.
type Match struct {
	ProjectID int64
	RuleID    int64
	Source    string // "rule"
}

// Engine evaluates ordered rules against events. It is safe for concurrent
// use once built; compiled regexes are cached.
type Engine struct {
	rules []model.Rule

	mu      sync.Mutex
	reCache map[string]*regexp.Regexp
}

// NewEngine builds an engine from rules. Callers should pass only active rules
// in match order (priority asc, id asc); ListRules(true) does this.
func NewEngine(rules []model.Rule) *Engine {
	return &Engine{rules: rules, reCache: map[string]*regexp.Regexp{}}
}

// Classify returns the first matching rule for e, or ok=false when none match.
func (en *Engine) Classify(e model.Event) (Match, bool) {
	for _, r := range en.rules {
		if en.ruleMatches(r, e) {
			return Match{ProjectID: r.ProjectID, RuleID: r.ID, Source: "rule"}, true
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
		if re == nil {
			return false
		}
		return re.MatchString(val)
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
