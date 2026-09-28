package schedule

import (
	"testing"
	"time"
)

func TestParseEvery(t *testing.T) {
	cases := map[string]time.Duration{
		"@every 10m":   10 * time.Minute,
		"@every 30s":   30 * time.Second,
		"@every 1h":    time.Hour,
		"@every 2d":    48 * time.Hour,
		"  @every 5m ": 5 * time.Minute, // whitespace-tolerant
	}
	for expr, want := range cases {
		s, err := Parse(expr)
		if err != nil || s.Every != want {
			t.Fatalf("Parse(%q) = %+v, %v; want every %s", expr, s, err, want)
		}
		if round, err := Parse(s.String()); err != nil || round.Every != want {
			t.Fatalf("canonical round trip = %+v %v", round, err)
		}
	}
}

func TestParseAt(t *testing.T) {
	s, err := Parse("@at 2026-07-26T17:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if s.At.Format(time.RFC3339) != "2026-07-26T17:00:00Z" {
		t.Fatalf("at = %s", s.At)
	}
	for _, bad := range []string{"@at tuesday", "@every", "@every 0m", "@every xm", "*/5 * * * *", ""} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) should fail", bad)
		}
	}
}

func TestGridAnchoring(t *testing.T) {
	s, _ := Parse("@every 10m")
	anchor := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// a slow run finishing at 12:25 fires next at 12:30, not 12:35
	next, ok := s.NextAfter(anchor, anchor.Add(25*time.Minute))
	if !ok || next.Format("15:04") != "12:30" {
		t.Fatalf("grid should not drift: next=%s ok=%v", next, ok)
	}
	// rapid ticks between fires don't double-fire
	next, _ = s.NextAfter(anchor, anchor.Add(30*time.Minute))
	if next.Format("15:04") != "12:40" {
		t.Fatalf("next after 12:30 = %s", next)
	}
}

func TestOneShot(t *testing.T) {
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	s, _ := Parse("@at " + at.Format(time.RFC3339))
	if next, ok := s.NextAfter(time.Now(), time.Now()); !ok || !next.Equal(at) {
		t.Fatalf("pending one-shot: next=%s ok=%v", next, ok)
	}
	if _, ok := s.NextAfter(at, at.Add(time.Minute)); ok {
		t.Fatal("a fired one-shot is done")
	}
}

func TestStringAt(t *testing.T) {
	s, err := Parse("@at 2026-07-26T17:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.String(); got != "@at 2026-07-26T17:00:00Z" {
		t.Fatalf("String() = %q", got)
	}
}

func TestExactScheduleRoundTripsAndRejectsTruncation(t *testing.T) {
	for _, expression := range []string{"@every 10m", "@every 1h", "@every 0.000000001s", "@every 1.25d", "@every 9223372036.854775807s", "@at 2500-01-02T03:04:05.123456789-07:00"} {
		t.Run(expression, func(t *testing.T) {
			value, err := Parse(expression)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Parse(value.String())
			if err != nil || again.Every != value.Every || !again.At.Equal(value.At) {
				t.Fatalf("roundtrip %q: %+v %v", value.String(), again, err)
			}
		})
	}
	for _, expression := range []string{"@every 0.0000000001s", "@every 0s", "@every 9223372036.854775808s", "@every 99999999999999999999999999999d", "@at 0000-01-01T00:00:00Z", "@at 2026-01-01T00:00:00.1234567891Z", "@at 2026-01-01T00:00:00+24:00", "@at 2026-01-01T00:00:00+01:60", "@at 9999-12-31T23:59:59-01:00"} {
		if _, err := Parse(expression); err == nil {
			t.Fatalf("accepted %q", expression)
		}
	}
	a, _ := Parse("@at 2500-01-02T03:04:05.123456789-07:00")
	b, _ := Parse("@at 2500-01-02T10:04:05.123456789Z")
	left, _ := Stamp(a.At)
	right, _ := Stamp(b.At)
	if left != right || left != "2500-01-02T10:04:05.123456789Z" {
		t.Fatal(left, right)
	}
	limit, _ := Parse("@at 9999-12-31T23:59:59.999999999Z")
	if _, err := Successor(limit.At, time.Nanosecond); err == nil {
		t.Fatal("successor silently overflowed")
	}
}
