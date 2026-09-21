package schedule

import (
	"testing"
	"time"
)

func TestNextAfterAncientAnchor(t *testing.T) {
	anchor := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 20, 12, 0, 0, 123456789, time.UTC)
	// Compute the small prime interval's remainder without constructing the
	// overflowing full nanosecond duration between year 1 and today.
	remainder := ((now.Unix()-anchor.Unix())%17*(int64(time.Second)%17) + int64(now.Nanosecond())) % 17
	for _, test := range []struct {
		every time.Duration
		want  time.Time
	}{
		{time.Nanosecond, now.Add(time.Nanosecond)},
		{17 * time.Nanosecond, now.Add(time.Duration(17 - remainder))},
		{time.Second, now.Truncate(time.Second).Add(time.Second)},
		{10 * time.Minute, now.Truncate(10 * time.Minute).Add(10 * time.Minute)},
	} {
		t.Run(test.every.String(), func(t *testing.T) {
			s := Schedule{Every: test.every}
			next, ok := s.NextAfter(anchor, now)
			if !ok || !next.Equal(test.want) {
				t.Fatalf("next=%v want=%v", next, test.want)
			}
			boundary, _ := s.NextAfter(anchor, next)
			if !boundary.Equal(next.Add(test.every)) {
				t.Fatalf("boundary=%v want=%v", boundary, next.Add(test.every))
			}
		})
	}
	large := time.Duration(1<<63 - 1)
	s := Schedule{Every: large}
	next, ok := s.NextAfter(anchor, anchor.Add(large))
	if !ok || !next.Equal(anchor.Add(large).Add(large)) {
		t.Fatalf("overflowed interval successor=%v", next)
	}
}

func TestNextSlot(t *testing.T) {
	anchor := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		schedule Schedule
		last     time.Time
		want     time.Time
		ok       bool
	}{
		{name: "first recurrence is anchor", schedule: Schedule{Every: 10 * time.Minute}, want: anchor, ok: true},
		{name: "claimed recurrence keeps grid", schedule: Schedule{Every: 10 * time.Minute}, last: anchor.Add(25 * time.Minute), want: anchor.Add(30 * time.Minute), ok: true},
		{name: "unclaimed overdue one shot", schedule: Schedule{At: anchor}, want: anchor, ok: true},
		{name: "claimed one shot", schedule: Schedule{At: anchor}, last: anchor},
		{name: "any one shot claim exhausts", schedule: Schedule{At: anchor}, last: anchor.Add(-time.Second)},
		{name: "unsupported schedule", schedule: Schedule{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.schedule.NextSlot(anchor, test.last)
			if ok != test.ok || !got.Equal(test.want) {
				t.Fatalf("NextSlot=%v/%v want=%v/%v", got, ok, test.want, test.ok)
			}
		})
	}
}
