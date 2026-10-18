package main

import (
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
		wd string
	}{
		{"2026-10-10", true, "Sat"},
		{"2024-02-29", true, "Thu"},
		{"2023-02-29", false, ""},
		{"2026-02-30", false, ""},
		{"2026-04-31", false, ""},
		{"2026-13-01", false, ""},
		{"2026-00-10", false, ""},
		{"0000-01-01", false, ""},
		{"2026-1-2", false, ""},
		{"20260102", false, ""},
		{"2026-10-10T00:00", false, ""},
		{" 2026-10-10", false, ""},
		{"+2026-10-10", false, ""},
		{"2026-10-1a", false, ""},
		{"", false, ""},
	} {
		d, ok := ParseDate(c.in)
		if ok != c.ok || ok && (d.String() != c.in || d.Weekday().String()[:3] != c.wd) {
			t.Errorf("ParseDate(%q) = %v %v (%v), want ok=%v %s", c.in, d, ok, d.Weekday(), c.ok, c.wd)
		}
	}
}

func TestResolveDate(t *testing.T) {
	if got := (Date{2026, 12, 31}).Add(1); got != (Date{2027, 1, 1}) {
		t.Errorf("2026-12-31 + 1 = %v", got)
	}
	for _, c := range []struct {
		now       time.Time
		arg, want string
		fromClock bool
	}{
		{time.Date(2026, 10, 18, 16, 30, 0, 0, time.UTC), "today", "2026-10-19", true},
		{time.Date(2026, 10, 18, 16, 30, 0, 0, time.UTC), "tomorrow", "2026-10-20", true},
		{time.Date(2026, 10, 18, 15, 59, 59, 0, time.UTC), "today", "2026-10-18", true},
		{time.Date(2026, 12, 31, 16, 0, 0, 0, time.UTC), "tomorrow", "2027-01-02", true},
		{time.Date(2026, 10, 18, 16, 30, 0, 0, time.UTC), "2026-10-10", "2026-10-10", false},
	} {
		d, clock, err := resolveDate(c.arg, c.now, cn)
		if err != nil || d.String() != c.want || clock != c.fromClock {
			t.Errorf("resolveDate(%q, %v) = %v %v %v, want %s %v", c.arg, c.now, d, clock, err, c.want, c.fromClock)
		}
	}
}

func TestLookup(t *testing.T) {
	d := fixture(t)
	for _, c := range []struct {
		day  string
		kind Kind
		name string
		ok   bool
	}{
		{"2026-10-01", Holiday, "国庆节", true}, // Thu
		{"2026-10-10", Workday, "国庆节", true}, // Sat
		{"2026-10-19", Weekday, "", true},
		{"2026-10-18", Weekend, "", true},
		{"2018-12-31", Holiday, "元旦", true}, // next year's schedule reaching back into December
		{"2018-12-29", Workday, "元旦", true},
		{"2007-01-01", Weekday, "", true},
		{"2026-12-28", Weekday, "", true},
		{"2026-12-29", 0, "", false},
		{"2006-12-31", 0, "", false},
	} {
		day, _ := ParseDate(c.day)
		k, n, ok := d.Lookup(day, cn)
		if k != c.kind || n != c.name || ok != c.ok {
			t.Errorf("Lookup(%s) = %v %q %v, want %v %q %v", c.day, k, n, ok, c.kind, c.name, c.ok)
		}
	}
}
