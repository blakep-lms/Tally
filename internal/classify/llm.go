package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/blakep-lms/tally/internal/model"
)

// Signal is the distilled, cache-keyable shape of an event for the LLM: the
// fields that determine classification, without the timestamp/duration noise.
type Signal struct {
	App   string
	Title string
	URL   string
	Repo  string
}

// Key is a stable signature used for caching LLM decisions.
func (s Signal) Key() string {
	return strings.ToLower(fmt.Sprintf("%s\x1f%s\x1f%s\x1f%s", s.App, s.Title, s.URL, s.Repo))
}

// SignalOf projects an event onto its classification signal.
func SignalOf(e model.Event) Signal {
	return Signal{App: e.App, Title: e.Title, URL: e.URL, Repo: e.Repo}
}

// LLMClassifier asks an Anthropic model to bucket ambiguous signals into one
// of the operator's active projects. It is optional, off by default, and only
// consulted for signals no deterministic rule matched.
type LLMClassifier struct {
	client anthropic.Client
	model  string
}

// NewLLMClassifier builds a classifier for the given model using apiKey.
func NewLLMClassifier(apiKey, modelID string) *LLMClassifier {
	return &LLMClassifier{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  modelID,
	}
}

// Suggest returns, for each signal, the id of the best-matching project or nil
// when the model is not confident enough to bucket it. The returned slice is
// aligned with signals. Projects should be the active, classifiable set.
func (c *LLMClassifier) Suggest(ctx context.Context, projects []model.Project, signals []Signal) ([]*int64, error) {
	out := make([]*int64, len(signals))
	if len(signals) == 0 || len(projects) == 0 {
		return out, nil
	}

	valid := map[int64]bool{}
	var projLines []string
	sorted := append([]model.Project(nil), projects...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, p := range sorted {
		valid[p.ID] = true
		label := string(p.Type)
		if p.Client != "" {
			label += ", client: " + p.Client
		}
		projLines = append(projLines, fmt.Sprintf("- id=%d name=%q (%s)", p.ID, p.Name, label))
	}

	var evLines []string
	for i, s := range signals {
		evLines = append(evLines, fmt.Sprintf("%d) app=%q title=%q url=%q repo=%q", i, s.App, s.Title, s.URL, s.Repo))
	}

	system := "You are a time-tracking classifier. Map each activity event to the single " +
		"project it most likely belongs to, using the app, window title, URL, and git repo. " +
		"Only assign a project when you are confident; otherwise return null for that event. " +
		"Return strictly a JSON object of the form {\"assignments\":[{\"event\":<index>,\"project_id\":<id or null>}]} " +
		"with one entry per event and no prose."

	user := fmt.Sprintf("Projects:\n%s\n\nEvents:\n%s",
		strings.Join(projLines, "\n"), strings.Join(evLines, "\n"))

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return nil, err
	}

	var raw strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			raw.WriteString(t.Text)
		}
	}

	var parsed struct {
		Assignments []struct {
			Event     int    `json:"event"`
			ProjectID *int64 `json:"project_id"`
		} `json:"assignments"`
	}
	body := extractJSON(raw.String())
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	for _, a := range parsed.Assignments {
		if a.Event < 0 || a.Event >= len(out) {
			continue
		}
		if a.ProjectID != nil && valid[*a.ProjectID] {
			id := *a.ProjectID
			out[a.Event] = &id
		}
	}
	return out, nil
}

// extractJSON returns the first {...} span in s, tolerating markdown fences or
// stray prose around the JSON object.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
