// Package schedule parses and evaluates task schedules for whip's
// wakeup channel: '@every 10m' for interval work, '@at <rfc3339>' for
// one-shots. Fires land on the schedule's own grid (anchor + n×interval), so
// a slow run never drifts later fires — the exo scheduler semantics, minus
// cron (whipcode keeps the grammar at two forms on purpose).
package schedule

import (
	"errors"
	"fmt"
	"math/big"
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

var (
	everyRe   = regexp.MustCompile(`^@every\s+(\d+(?:\.\d+)?)(s|m|h|d)$`)
	instantRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)
)

// Parse reads "@every 10m" / "@every 1h" / "@at 2026-07-26T17:00:00Z".
func Parse(expr string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if len(expr) > 128 {
		return Schedule{}, errors.New("schedule expression exceeds 128 bytes")
	}
	if m := everyRe.FindStringSubmatch(expr); m != nil {
		number, ok := new(big.Rat).SetString(m[1])
		if !ok {
			return Schedule{}, errors.New("invalid interval")
		}
		unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		number.Mul(number, new(big.Rat).SetInt64(int64(unit)))
		if !number.IsInt() || !number.Num().IsInt64() || number.Sign() <= 0 {
			return Schedule{}, errors.New("interval must be a positive exact nanosecond duration within int64")
		}
		return Schedule{Every: time.Duration(number.Num().Int64())}, nil
	}
	if at, ok := strings.CutPrefix(expr, "@at "); ok {
		at = strings.TrimSpace(at)
		if !instantRe.MatchString(at) {
			return Schedule{}, errors.New("@at requires an RFC3339 instant with at most nine fractional digits")
		}
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return Schedule{}, fmt.Errorf("@at needs an RFC3339 time (e.g. @at 2026-07-26T17:00:00Z): %w", err)
		}
		if t.Year() < 1 || t.UTC().Year() > 9999 || t.UTC().Year() < 1 {
			return Schedule{}, errors.New("schedule time is outside years 1–9999")
		}
		return Schedule{At: t.UTC()}, nil
	}
	return Schedule{}, fmt.Errorf("schedule must be @every <dur> or @at <rfc3339>, got %q", expr)
}

// String renders the canonical form (what /schedule list shows).
func (s Schedule) String() string {
	if s.Every > 0 {
		seconds := strconv.FormatInt(int64(s.Every/time.Second), 10)
		fraction := strings.TrimRight(fmt.Sprintf("%09d", int64(s.Every%time.Second)), "0")
		if fraction != "" {
			seconds += "." + fraction
		}
		return "@every " + seconds + "s"
	}
	return "@at " + s.At.UTC().Format(time.RFC3339Nano)
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
			// spans without overflowing a single duration.
			next = next.Add(t.Sub(next).Truncate(s.Every)).Add(s.Every)
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

// Stamp is the exact, chronologically sortable SQLite occurrence representation.
func Stamp(at time.Time) (string, error) {
	at = at.UTC()
	if at.Year() < 1 || at.Year() > 9999 {
		return "", errors.New("schedule time is outside years 1–9999")
	}
	return at.Format("2006-01-02T15:04:05.000000000Z"), nil
}

// Successor never rounds an occurrence or silently wraps the supported date range.
func Successor(at time.Time, every time.Duration) (time.Time, error) {
	if every <= 0 {
		return time.Time{}, errors.New("recurrence interval must be positive")
	}
	next := at.Add(every).UTC()
	if !next.After(at) {
		return time.Time{}, errors.New("schedule successor does not advance")
	}
	if _, err := Stamp(next); err != nil {
		return time.Time{}, err
	}
	return next, nil
}
