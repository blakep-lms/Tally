package classify

import (
	"testing"

	"github.com/blakep-lms/tally/internal/model"
)

func TestEngineFirstMatchWins(t *testing.T) {
	rules := []model.Rule{
		{ID: 1, ProjectID: 10, Field: model.FieldApp, Match: model.MatchContains, Pattern: "code", Priority: 1},
		{ID: 2, ProjectID: 20, Field: model.FieldTitle, Match: model.MatchContains, Pattern: "tally", Priority: 2},
	}
	eng := NewEngine(rules)

	// App rule (priority 1) wins over the title rule.
	m, ok := eng.Classify(model.Event{App: "Code", Title: "tally — main"})
	if !ok || m.ProjectID != 10 || m.RuleID != 1 {
		t.Fatalf("expected project 10 via rule 1, got %+v ok=%v", m, ok)
	}
	// Only the title rule matches here.
	m, ok = eng.Classify(model.Event{App: "iTerm2", Title: "tally build"})
	if !ok || m.ProjectID != 20 {
		t.Fatalf("expected project 20, got %+v ok=%v", m, ok)
	}
	// Nothing matches.
	if _, ok := eng.Classify(model.Event{App: "Slack", Title: "chat"}); ok {
		t.Fatal("expected no match")
	}
}

func TestEngineMatchKinds(t *testing.T) {
	eng := NewEngine([]model.Rule{
		{ID: 1, ProjectID: 1, Field: model.FieldURL, Match: model.MatchEquals, Pattern: "https://x.com"},
		{ID: 2, ProjectID: 2, Field: model.FieldRepo, Match: model.MatchRegex, Pattern: `^secure.*`},
	})
	if _, ok := eng.Classify(model.Event{URL: "HTTPS://X.COM"}); !ok {
		t.Error("equals should be case-insensitive")
	}
	if _, ok := eng.Classify(model.Event{Repo: "secureai-backend"}); !ok {
		t.Error("regex should match")
	}
	if _, ok := eng.Classify(model.Event{Repo: "insecure"}); ok {
		t.Error("anchored regex should not match 'insecure'")
	}
}

func TestSignalKeyStable(t *testing.T) {
	a := SignalOf(model.Event{App: "Code", Title: "T", URL: "https://Example.COM/path?secret=x", Repo: "R"})
	b := Signal{App: "code", Title: "t", URL: "example.com", Repo: "r"}
	if a.Key() != b.Key() {
		t.Error("signal key should be case-insensitive and stable")
	}
}
