package capture

import (
	"context"
	"time"

	"github.com/blakep-lms/tally/internal/model"
)

// Provider is a source of activity events. ActivityWatch is the v1
// implementation; the interface exists so a custom capture daemon can be
// swapped in later (PRD non-goal for v1, but the seam is cheap to keep).
type Provider interface {
	// Name identifies the provider (e.g. "activitywatch").
	Name() string
	// Available reports whether the provider is reachable/running.
	Available(ctx context.Context) bool
	// Pull returns merged, AFK-cleaned events in [from, to). Each event must
	// carry a stable SourceKey so re-pulls are idempotent.
	Pull(ctx context.Context, from, to time.Time) ([]model.Event, error)
}
