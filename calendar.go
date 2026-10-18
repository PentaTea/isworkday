package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Date is a calendar date with no time zone.
type Date struct{ Y, M, D int }

func (d Date) String() string        { return fmt.Sprintf("%04d-%02d-%02d", d.Y, d.M, d.D) }
func (d Date) utc() time.Time        { return time.Date(d.Y, time.Month(d.M), d.D, 0, 0, 0, 0, time.UTC) }
func (d Date) Weekday() time.Weekday { return d.utc().Weekday() }
func (d Date) Add(days int) Date     { return dateOf(d.utc().AddDate(0, 0, days)) }
func (d Date) daysSince(e Date) int  { return int(d.utc().Sub(e.utc()).Hours() / 24) }
func dateOf(t time.Time) Date        { y, m, d := t.Date(); return Date{y, int(m), d} }

// ParseDate accepts only YYYY-MM-DD naming a real date.
func ParseDate(s string) (Date, bool) {
	t, err := time.Parse(time.DateOnly, s)
	return dateOf(t), err == nil && t.Year() >= 1
}

type Kind int

const (
	Weekday Kind = iota
	Weekend
	Holiday
	Workday
)

// Data is schema 1 of data/<code>.json.
type Data struct {
	Schema   int               `json:"schema"`
	Region   string            `json:"region"`
	Version  string            `json:"version"`
	Verified string            `json:"verified,omitempty"`
	Range    [2]string         `json:"range"`
	Holidays map[string]string `json:"holidays"`
	Workdays map[string]string `json:"workdays"`
}

// parse accepts only valid data for r: a captive-portal page or another
// region's file never replaces good data.
func parse(b []byte, r region) (d Data, err error) {
	if json.Unmarshal(b, &d) != nil {
		return d, errors.New("not JSON")
	}
	return d, d.Validate(r)
}

func (d *Data) Validate(r region) error {
	if d.Schema > 1 {
		return fmt.Errorf("schema %d not supported", d.Schema)
	}
	_, okV := ParseDate(d.Version)
	_, ok0 := ParseDate(d.Range[0])
	_, ok1 := ParseDate(d.Range[1])
	if d.Schema != 1 || d.Region != r.code || !okV || !ok0 || !ok1 || d.Range[0] > d.Range[1] {
		return errors.New("invalid data")
	}
	return nil
}

// Lookup returns ok=false when day is outside the published range: never guess.
func (d *Data) Lookup(day Date, r region) (Kind, string, bool) {
	key := day.String() // ISO strings sort in date order
	if key < d.Range[0] || key > d.Range[1] {
		return 0, "", false
	}
	if n, ok := d.Holidays[key]; ok {
		return Holiday, n, true
	}
	if n, ok := d.Workdays[key]; ok {
		return Workday, n, true
	}
	if r.weekend[day.Weekday()] {
		return Weekend, "", true
	}
	return Weekday, "", true
}

// newer reports whether b should replace a; ties go to b.
func newer(a, b Data) bool {
	return b.Version > a.Version || b.Version == a.Version && b.Verified >= a.Verified
}
