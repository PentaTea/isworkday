// Command isworkday answers whether a date is a statutory workday in a region,
// with one word on stdout and the exit code.
package main

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

const repoURL = "https://github.com/PentaTea/isworkday"

// version is set by -ldflags "-X main.version=…" in releases.
var version = "dev"

func init() {
	// Without ldflags, use the version Go stamped from the module or VCS.
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = strings.TrimPrefix(bi.Main.Version, "v")
	}
}

const help = `isworkday — is DATE a statutory workday in REGION?

Usage: isworkday REGION [DATE]
  REGION   %s
  DATE     YYYY-MM-DD, today (default) or tomorrow; today/tomorrow are dates in REGION.

Output: one word on stdout + exit code, a one-line reason on stderr.
  true      0   workday (a weekday, or a make-up day on a weekend)
  false     1   holiday or weekend
  unknown   3   no data for that date yet
  (empty)   2   usage error

Data: official calendars via https://github.com/PentaTea/isworkday, refreshed
  at most every 12 h into $XDG_CACHE_HOME/isworkday (~/.cache); offline, the
  last known data answers with a warning.

Options:
  -h, --help      show this help
  -V, --version   show version and data coverage

Environment:
  ISWORKDAY_DATA_URL   fetch data from this URL instead
`

func main() {
	env := Env{Now: time.Now, Client: &http.Client{Timeout: 3 * time.Second}, CacheDir: cacheDir()}
	if u := os.Getenv("ISWORKDAY_DATA_URL"); u != "" {
		env.URLs = []string{u}
	}
	os.Exit(run(os.Args[1:], env, os.Stdout, os.Stderr))
}

func run(args []string, env Env, stdout, stderr io.Writer) int {
	fail := func(code int, msg string) int {
		fmt.Fprintln(stderr, "error: "+msg)
		return code
	}
	const usage = "usage: isworkday REGION [DATE]"
	codes := slices.Sorted(maps.Keys(regions))
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	switch {
	case len(args) > 2 || len(args) == 2 && strings.HasPrefix(first, "-"): // options take no arguments
		return fail(2, "too many arguments — "+usage)
	case len(args) == 0:
		return fail(2, "missing region — "+usage+", e.g. isworkday cn")
	case first == "-h" || first == "--help":
		var list []string
		for _, k := range codes {
			list = append(list, k+" ("+regions[k].name+")")
		}
		fmt.Fprintf(stdout, help, strings.Join(list, ", "))
		return 0
	case first == "-V" || first == "--version":
		for _, k := range codes {
			d, source, _ := load(env, regions[k], false)
			fmt.Fprintf(stdout, "isworkday %s · %s data %s (%s..%s) from %s\n", version, k, d.Version, d.Range[0], d.Range[1], source)
		}
		return 0
	case strings.HasPrefix(first, "-"):
		return fail(2, fmt.Sprintf("unknown option %q — see isworkday --help", first))
	}
	r, ok := regions[strings.ToLower(first)]
	if !ok {
		return fail(2, fmt.Sprintf("unsupported region %q — supported: %s", first, strings.Join(codes, ", ")))
	}
	arg := "today"
	if len(args) == 2 {
		arg = args[1]
	}
	now := env.Now()
	day, fromClock, err := resolveDate(arg, now, r)
	if err != nil {
		return fail(2, err.Error())
	}
	d, _, warn := load(env, r, true)
	if warn != "" {
		fmt.Fprintln(stderr, "warning: "+warn)
	}
	kind, name, ok := d.Lookup(day, r)
	if !ok {
		fmt.Fprintln(stdout, "unknown")
		if day.String() < d.Range[0] {
			return fail(3, fmt.Sprintf("no data for %s — covered from %s", day, d.Range[0]))
		}
		return fail(3, fmt.Sprintf("no data for %s yet — covered through %s", day, d.Range[1]))
	}
	annot := ""
	if local := dateOf(now); fromClock && local != r.date(now) {
		annot = " (local date is " + local.String() + ")"
	}
	fmt.Fprintln(stderr, reason(day, kind, name, annot))
	work := kind == Weekday || kind == Workday
	fmt.Fprintln(stdout, work) // "true" / "false"
	if work {
		return 0
	}
	return 1
}

// resolveDate takes the instant so that the date and the local-date annotation
// cannot straddle midnight.
func resolveDate(arg string, now time.Time, r region) (day Date, fromClock bool, err error) {
	switch arg {
	case "today":
		return r.date(now), true, nil
	case "tomorrow":
		return r.date(now).Add(1), true, nil
	}
	if d, ok := ParseDate(arg); ok {
		return d, false, nil
	}
	// %q keeps the error on one line whatever the argument holds.
	return Date{}, false, fmt.Errorf("%q is not a valid date — use YYYY-MM-DD, today or tomorrow", arg)
}

func reason(day Date, k Kind, name string, annot string) string {
	s := day.String() + " " + day.Weekday().String()[:3] + " · " + [...]string{"weekday", "weekend", "holiday", "workday"}[k]
	if k == Holiday {
		s += " (" + name + ")"
	} else if k == Workday {
		s += " (" + name + " 补班)"
	}
	return s + annot
}
