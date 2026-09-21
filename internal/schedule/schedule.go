// Package schedule parses and evaluates task schedules for whip's
// wakeup channel: '@every 10m' for interval work, '@at <rfc3339>' for
// one-shots. Fires land on the schedule's own grid (anchor + n×interval), so
// a slow run never drifts later fires — the exo scheduler semantics, minus
// cron (whip keeps the grammar at two forms on purpose).
package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Schedule is one parsed schedule expression.
type Schedule struct {
	Every time.Duration // >0 for "@every" (recurring)
	At    time.Time     // non-zero for "@at" (one-shot)
}

var everyRe = regexp.MustCompile(`^@every\s+(\d+(?:\.\d+)?)(s|m|h|d)$`)

// Parse reads "@every 10m" / "@every 1h" / "@at 2026-07-26T17:00:00Z".
func Parse(expr string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if m := everyRe.FindStringSubmatch(expr); m != nil {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil || n <= 0 {
			return Schedule{}, fmt.Errorf("bad interval %q", m[1])
		}
		unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		return Schedule{Every: time.Duration(n * float64(unit))}, nil
	}
	if at, ok := strings.CutPrefix(expr, "@at"); ok {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
		if err != nil {
			return Schedule{}, fmt.Errorf("@at needs an RFC3339 time (e.g. @at 2026-07-26T17:00:00Z): %w", err)
		}
		return Schedule{At: t}, nil
	}
	return Schedule{}, fmt.Errorf("schedule must be @every <dur> or @at <rfc3339>, got %q", expr)
}

// String renders the canonical form (what /schedule list shows).
func (s Schedule) String() string {
	if s.Every > 0 {
		return "@every " + s.Every.String()
	}
	return "@at " + s.At.Format(time.RFC3339)
}

// NextSlot returns the next unclaimed occurrence, including overdue slots.
// A recurring schedule first fires at its anchor; claiming a slot, not the
// passage of time, advances the schedule.
func (s Schedule) NextSlot(anchor, lastFire time.Time) (time.Time, bool) {
	if lastFire.IsZero() {
		if s.Every > 0 {
			return anchor, !anchor.IsZero()
		}
		return s.At, !s.At.IsZero()
	}
	if s.Every <= 0 {
		return time.Time{}, false
	}
	return s.NextAfter(anchor, lastFire)
}

// NextAfter returns the next fire time strictly after t, anchored so
// recurring fires land on the grid (anchor + n×interval). (0, false) means
// the schedule is done (a fired one-shot).
func (s Schedule) NextAfter(anchor, t time.Time) (time.Time, bool) {
	if s.Every > 0 {
		next := anchor
		for !next.After(t) {
			// Jump whole intervals rather than walking the elapsed grid. Sub
			// saturates for spans over ~292 years; repeating handles those
			// spans without overflowing the duration product.
			steps := t.Sub(next) / s.Every
			next = next.Add(steps * s.Every).Add(s.Every)
		}
		return next, true
	}
	if s.At.IsZero() {
		return time.Time{}, false
	}
	if s.At.After(t) {
		return s.At, true
	}
	return time.Time{}, false // one-shot already fired
}
