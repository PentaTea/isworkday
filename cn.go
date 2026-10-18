package main

import (
	_ "embed"
	"time"
)

// region holds everything region-specific.
type region struct {
	code, name string        // lowercase ISO 3166-1 alpha-2, display name
	offset     time.Duration // fixed UTC offset that defines "today"
	weekend    [7]bool       // indexed by time.Weekday
	urls       []string      // data URLs, tried in order
	snapshot   []byte        // data embedded at build time
}

// regions are the supported REGION arguments.
var regions = map[string]region{cn.code: cn}

//go:embed data/cn.json
var cnSnapshot []byte

var cn = region{
	code:    "cn",
	name:    "mainland China",
	offset:  8 * time.Hour,
	weekend: [7]bool{time.Saturday: true, time.Sunday: true},
	urls: []string{
		// @main: without it jsDelivr pins the latest release tag
		"https://cdn.jsdelivr.net/gh/PentaTea/isworkday@main/data/cn.json",
		"https://raw.githubusercontent.com/PentaTea/isworkday/main/data/cn.json",
	},
	snapshot: cnSnapshot,
}

// date is the region's calendar date at instant t (plain arithmetic, no tzdata).
func (r region) date(t time.Time) Date { return dateOf(t.UTC().Add(r.offset)) }
