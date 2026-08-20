package classify

import (
	"strings"
	"testing"

	"github.com/blakep-lms/tally/internal/model"
)

func TestPrivateURLHostRedactsSensitiveURLParts(t *testing.T) {
	got := privateURLHost("https://user:secret@example.com/client/path?token=abc#frag")
	if got != "example.com" {
		t.Fatalf("host = %q", got)
	}
	if got := privateURLHost("not a url"); got != "" {
		t.Fatalf("invalid url host = %q", got)
	}
}

func TestSignalOfNeverCachesSensitiveURLParts(t *testing.T) {
	signal := SignalOf(model.Event{URL: "https://example.com/client/path?token=abc#frag"})
	if signal.URL != "example.com" || strings.Contains(signal.Key(), "token") || strings.Contains(signal.Key(), "client/path") {
		t.Fatalf("unsafe signal: %+v key=%q", signal, signal.Key())
	}
}

func TestParseAssignmentsRequiresConfidenceAndValidCandidate(t *testing.T) {
	body := `{"assignments":[{"event":0,"work_item_id":7,"confidence":0.79},{"event":1,"work_item_id":7,"confidence":0.80},{"event":2,"work_item_id":9,"confidence":0.99}]}`
	got, err := parseAssignments(body, map[int64]bool{7: true}, 3, 0.80)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != nil || got[1] == nil || *got[1] != 7 || got[2] != nil {
		t.Fatalf("confidence filtering = %+v", got)
	}
}
