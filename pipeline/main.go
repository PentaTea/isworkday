// Command pipeline rebuilds data/cn.json from its upstreams on every run:
// merge, apply manual patches, check invariants, and write only real changes.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Data's field order is data/cn.json's, so encoding reproduces it byte for byte.
type Data struct {
	Schema   int                 `json:"schema"`
	Region   string              `json:"region"`
	Version  string              `json:"version"`
	Verified string              `json:"verified,omitempty"`
	Range    [2]string           `json:"range"`
	Years    map[string][]string `json:"years,omitempty"`
	Holidays map[string]string   `json:"holidays"`
	Workdays map[string]string   `json:"workdays"`
}

// source is one upstream: its dates, and its published years with their notices.
type source struct {
	name  string
	days  map[string]day
	years map[int][]string
}

type day struct {
	on   bool // a working day
	name string
}

// patch is a maintainer's ruling from pipeline/patches/*.json; see README.md there.
type patch struct {
	Source, Note       string
	Holidays, Workdays map[string]string
	Default            []string
}

func (p patch) dates() []string {
	return slices.Concat(slices.Collect(maps.Keys(p.Holidays)), slices.Collect(maps.Keys(p.Workdays)), p.Default)
}

// A commit at least this often keeps GitHub from disabling the cron (60 days idle).
const heartbeat = 30 * 24 * time.Hour

const usage = "usage: go run ./pipeline -hcn <dir> [-out data/cn.json] [-patches pipeline/patches] [-today YYYY-MM-DD] [-cd-latest <url>] [-cd <url template %s>]"

var onOff = map[bool]string{true: "on", false: "off"}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run returns 0 when done, changed or not; 1 when nothing was written; 2 when
// the file was written but next year's schedule is overdue.
func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("pipeline", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.Usage = func() { io.WriteString(stderr, usage+"\n") }
	hcnDir := fl.String("hcn", "", "")
	out := fl.String("out", "data/cn.json", "")
	patchDir := fl.String("patches", "pipeline/patches", "")
	todayArg := fl.String("today", time.Now().UTC().Add(offset).Format(time.DateOnly), "")
	latestURL := fl.String("cd-latest", cdLatest, "")
	fileURL := fl.String("cd", cdFile, "")
	if fl.Parse(args) != nil { // flag's own exit code 2 would read as a deadline
		return 1
	}
	if *hcnDir == "" || fl.NArg() > 0 {
		fl.Usage()
		return 1
	}
	today, err := time.Parse(time.DateOnly, *todayArg)
	if err != nil {
		return fail(stderr, fmt.Errorf("-today: want YYYY-MM-DD, got %q", *todayArg))
	}
	var prev Data
	prevB, err := os.ReadFile(*out)
	if err == nil {
		err = json.Unmarshal(prevB, &prev)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return fail(stderr, fmt.Errorf("%s: %w", *out, err))
	}
	srcs, err := upstreams(*hcnDir, *latestURL, *fileURL)
	if err != nil {
		return fail(stderr, err)
	}
	d, years, conflicts, err := merge(srcs, today.Year())
	if err != nil {
		return fail(stderr, err)
	}
	names, patches, err := loadPatches(*patchDir, d.Range)
	if err != nil {
		return fail(stderr, err)
	}

	// Every unsettled conflict is printed, so one patch can settle them all.
	unsettled := false
	for _, k := range slices.Sorted(maps.Keys(conflicts)) {
		if !slices.ContainsFunc(patches, func(p patch) bool { return slices.Contains(p.dates(), k) }) {
			fmt.Fprintln(stderr, conflicts[k])
			unsettled = true
		}
	}
	if unsettled {
		return 1
	}
	// Redundant means the upstreams alone agree, not merely an earlier patch.
	hol, wrk := maps.Clone(d.Holidays), maps.Clone(d.Workdays)
	for i, p := range patches {
		redundant := true
		for _, k := range p.dates() {
			redundant = redundant && conflicts[k] == "" && hol[k] == p.Holidays[k] && wrk[k] == p.Workdays[k]
			delete(d.Holidays, k)
			delete(d.Workdays, k)
		}
		maps.Copy(d.Holidays, p.Holidays)
		maps.Copy(d.Workdays, p.Workdays)
		if redundant {
			fmt.Fprintf(stderr, "note: patches/%s is redundant (upstreams agree) — delete it\n", names[i])
		}
	}
	if err := check(d, prev, years); err != nil {
		return fail(stderr, err)
	}

	same := d.Range == prev.Range && maps.Equal(d.Holidays, prev.Holidays) &&
		maps.Equal(d.Workdays, prev.Workdays) && maps.EqualFunc(d.Years, prev.Years, slices.Equal[[]string])
	ts := today.Format(time.DateOnly)
	d.Version, d.Verified = prev.Version, prev.Verified
	if !same {
		d.Version = ts
	}
	if v, err := time.Parse(time.DateOnly, prev.Verified); !same || err != nil || today.Sub(v) >= heartbeat {
		d.Verified = ts
	}
	if b := encode(d); bytes.Equal(b, prevB) {
		fmt.Fprintln(stderr, "nothing changed")
	} else if err := writeAtomic(*out, b); err != nil {
		return fail(stderr, err)
	} else {
		fmt.Fprintf(stdout, "%s: through %s, %d holidays / %d workdays, version %s, verified %s\n",
			filepath.Base(*out), d.Range[1], len(d.Holidays), len(d.Workdays), d.Version, d.Verified)
	}

	if end, _ := time.Parse(time.DateOnly, d.Range[1]); !today.Before(end.AddDate(0, 0, -deadlineDays)) {
		fmt.Fprintf(stderr, "deadline: schedule for %d is still missing on %s — check %s\n", end.Year()+1, ts, upstreamNames)
		return 2
	}
	return 0
}

// merge compares each date's effective state, not entries: upstreams list no-op
// weekend rests and file Dec 29–31 under different years. A source has a say
// through Dec 28 of its last published year; sources that have a say must agree.
func merge(srcs []source, thisYear int) (d Data, years map[int]bool, conflicts map[string]string, err error) {
	start, _ := time.Parse(time.DateOnly, firstDay)
	d = Data{Schema: 1, Region: region, Years: map[string][]string{}, Holidays: map[string]string{}, Workdays: map[string]string{}}
	years, conflicts = map[int]bool{}, map[string]string{}
	ends, last := make([]string, len(srcs)), 0
	for i, s := range srcs {
		if len(s.years) == 0 {
			return d, nil, nil, fmt.Errorf("%s: no published year", s.name)
		}
		for y, papers := range s.years {
			if y >= start.Year() && y <= thisYear+1 {
				years[y] = true
				if k := strconv.Itoa(y); len(papers) > 0 && d.Years[k] == nil {
					d.Years[k] = papers
				}
			}
		}
		y := slices.Max(slices.Collect(maps.Keys(s.years)))
		ends[i], last = fmt.Sprintf("%d-%s", y, lastDay), max(last, y)
	}
	d.Range = [2]string{firstDay, fmt.Sprintf("%d-%s", min(last, thisYear+1), lastDay)}
	for t := start; t.Format(time.DateOnly) <= d.Range[1]; t = t.AddDate(0, 0, 1) {
		k, def := t.Format(time.DateOnly), working(t.Weekday())
		on, name, agree, said := def, "", true, []string(nil)
		for i, s := range srcs {
			if k > ends[i] {
				continue
			}
			v, ok := s.days[k]
			if !ok {
				v.on = def
			}
			if said == nil {
				on, name = v.on, v.name
			}
			agree = agree && v.on == on
			said = append(said, s.name+"="+onOff[v.on])
		}
		switch {
		case !agree:
			conflicts[k] = "conflict " + k + ": " + strings.Join(said, " ")
		case on == def:
		case on:
			d.Workdays[k] = name
		default:
			d.Holidays[k] = name
		}
	}
	return d, years, conflicts, nil
}

// loadPatches reads dir/*.json in name order; dates must lie in the range.
func loadPatches(dir string, r [2]string) ([]string, []patch, error) {
	var names []string
	var patches []patch
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		var p patch
		b, err := os.ReadFile(f)
		if err == nil {
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields() // a misspelt key must not silently do nothing
			err = dec.Decode(&p)
		}
		seen := map[string]bool{}
		for _, k := range p.dates() {
			_, bad := time.Parse(time.DateOnly, k)
			switch {
			case err != nil:
			case seen[k]:
				err = fmt.Errorf("%s is listed twice", k)
			case bad != nil || k < r[0] || k > r[1]:
				err = fmt.Errorf("%s is outside %s..%s", k, r[0], r[1])
			}
			seen[k] = true
		}
		if err != nil {
			return nil, nil, fmt.Errorf("patches/%s: %w", filepath.Base(f), err)
		}
		names, patches = append(names, filepath.Base(f)), append(patches, p)
	}
	return names, patches, nil
}

// check enforces what the CLI relies on.
func check(d Data, prev Data, years map[int]bool) error {
	holidays := map[string]int{}
	for i, tbl := range []map[string]string{d.Holidays, d.Workdays} {
		for k, v := range tbl {
			t, err := time.Parse(time.DateOnly, k)
			if err != nil || k < d.Range[0] || k > d.Range[1] || v == "" || working(t.Weekday()) != (i == 0) {
				return fmt.Errorf("%s: %s %q must be a named %s date within %s..%s",
					[]string{"holidays", "workdays"}[i], k, v, []string{"Mon–Fri", "Sat/Sun"}[i], d.Range[0], d.Range[1])
			}
			if i == 0 {
				holidays[k[:4]]++
			}
		}
	}
	if d.Range[0] != firstDay || !strings.HasSuffix(d.Range[1], "-"+lastDay) {
		return fmt.Errorf("range %v must run from %s to a %s", d.Range, firstDay, lastDay)
	}
	if d.Range[1] < prev.Range[1] {
		return fmt.Errorf("range would shrink from %s to %s: an upstream dropped a year", prev.Range[1], d.Range[1])
	}
	// An upstream that has so far added only New Year's Day must not extend the range.
	for y := range years {
		if n := holidays[strconv.Itoa(y)]; n < minHolidays {
			s := "s"
			if n == 1 {
				s = ""
			}
			return fmt.Errorf("%d has only %d holiday%s: a partial year?", y, n, s)
		}
	}
	// A year both upstreams dropped would otherwise become plain workdays without a conflict.
	for k := range prev.Holidays {
		if y := k[:min(4, len(k))]; holidays[y] == 0 {
			return fmt.Errorf("%s would lose all its holidays: the upstreams dropped the year", y)
		}
	}
	return nil
}

// encode is deterministic: struct order, sorted map keys, UTF-8 left unescaped.
func encode(d Data) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(d) // cannot fail for this type
	return b.Bytes()
}

// writeAtomic never leaves a truncated data/cn.json for the workflow to commit.
func writeAtomic(path string, b []byte) error {
	if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
		os.Remove(path + ".tmp")
		return err
	}
	return os.Rename(path+".tmp", path)
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
