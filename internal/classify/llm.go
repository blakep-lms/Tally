package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

// SignalOf projects an event onto a privacy-minimized classification signal.
func SignalOf(e model.Event) Signal {
	return Signal{App: e.App, Title: e.Title, URL: privateURLHost(e.URL), Repo: e.Repo}
}

// LLMClassifier asks an Anthropic model to bucket ambiguous signals into one
// of the operator's active work items. It is optional, off by default, and only
// consulted for signals no deterministic rule matched.
type LLMClassifier struct {
	client        anthropic.Client
	model         string
	minConfidence float64
}

// NewLLMClassifier builds a classifier for the given model using apiKey.
func NewLLMClassifier(apiKey, modelID string, minConfidence ...float64) *LLMClassifier {
	threshold := 0.80
	if len(minConfidence) > 0 && minConfidence[0] > 0 && minConfidence[0] <= 1 {
		threshold = minConfidence[0]
	}
	return &LLMClassifier{
		client:        anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:         modelID,
		minConfidence: threshold,
	}
}

// Suggest returns, for each signal, the id of the best-matching work item or nil
// when the model is not confident enough to bucket it. The returned slice is
// aligned with signals. WorkItems should be the active, classifiable set.
func (c *LLMClassifier) Suggest(ctx context.Context, workItems []model.WorkItem, signals []Signal) ([]*int64, error) {
	out := make([]*int64, len(signals))
	if len(signals) == 0 || len(workItems) == 0 {
		return out, nil
	}

	valid := map[int64]bool{}
	var projLines []string
	sorted := append([]model.WorkItem(nil), workItems...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, p := range sorted {
		valid[p.ID] = true
		label := string(p.Kind)
		if p.Context != "" {
			label += ", context: " + p.Context
		}
		projLines = append(projLines, fmt.Sprintf("- id=%d name=%q (%s)", p.ID, p.Name, label))
	}

	var evLines []string
	for i, s := range signals {
		evLines = append(evLines, fmt.Sprintf("%d) app=%q title=%q url_host=%q repo=%q", i, s.App, s.Title, privateURLHost(s.URL), s.Repo))
	}

	system := "You are a time-tracking classifier. Map each activity event to the single " +
		"work item it most likely belongs to, using the app, window title, URL, and git repo. " +
		"Only assign a work item when you are confident; otherwise return null for that event. " +
		"Return strictly a JSON object of the form {\"assignments\":[{\"event\":<index>,\"work_item_id\":<id or null>,\"confidence\":<0 to 1>}]} " +
		"with one entry per event and no prose."

	user := fmt.Sprintf("WorkItems:\n%s\n\nEvents:\n%s",
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

	body := extractJSON(raw.String())
	return parseAssignments(body, valid, len(out), c.minConfidence)
}

func parseAssignments(body string, valid map[int64]bool, count int, minConfidence float64) ([]*int64, error) {
	out := make([]*int64, count)
	var parsed struct {
		Assignments []struct {
			Event      int     `json:"event"`
			WorkItemID *int64  `json:"work_item_id"`
			Confidence float64 `json:"confidence"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	for _, a := range parsed.Assignments {
		if a.Event < 0 || a.Event >= len(out) || a.Confidence < minConfidence {
			continue
		}
		if a.WorkItemID != nil && valid[*a.WorkItemID] {
			id := *a.WorkItemID
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

// privateURLHost strips path, query, fragment, user info, and other potentially
// sensitive URL material before optional LLM classification leaves the machine.
func privateURLHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
