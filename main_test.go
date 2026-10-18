package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func runCLI(env Env, args ...string) (stdout, stderr string, code int) {
	var o, e bytes.Buffer
	code = run(args, env, &o, &e)
	return o.String(), e.String(), code
}

func snapshot(t *testing.T) Data {
	var d Data
	if err := json.Unmarshal(cn.snapshot, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestContract pins every user-visible string, offline.
func TestContract(t *testing.T) {
	snap := snapshot(t)
	pdt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.FixedZone("PDT", -7*3600)) // Beijing: 10-19 01:00
	cst := time.Date(2026, 10, 19, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	const bad = ` is not a valid date — use YYYY-MM-DD, today or tomorrow` + "\n"
	const unsupported = ` — supported: cn` + "\n"
	const tooMany = "error: too many arguments — usage: isworkday REGION [DATE]\n"
	for _, c := range []struct {
		args   []string
		now    time.Time
		out    string
		code   int
		stderr string
	}{
		{[]string{"cn", "2026-10-19"}, t0, "true\n", 0, "2026-10-19 Mon · weekday\n"},
		{[]string{"cn", "2026-10-18"}, t0, "false\n", 1, "2026-10-18 Sun · weekend\n"},
		{[]string{"cn", "2026-10-01"}, t0, "false\n", 1, "2026-10-01 Thu · holiday (国庆节)\n"},
		{[]string{"cn", "2026-10-10"}, t0, "true\n", 0, "2026-10-10 Sat · workday (国庆节 补班)\n"},
		{[]string{"CN", "2026-10-10"}, t0, "true\n", 0, "2026-10-10 Sat · workday (国庆节 补班)\n"},
		{[]string{"cn", "2026-02-30"}, t0, "", 2, `error: "2026-02-30"` + bad},
		{[]string{"cn", "yesterday"}, t0, "", 2, `error: "yesterday"` + bad},
		{[]string{"cn", ""}, t0, "", 2, `error: ""` + bad},
		{[]string{"cn", "２０２６-10-10"}, t0, "", 2, `error: "２０２６-10-10"` + bad},
		{[]string{"cn", "2026-10-10\nfoo"}, t0, "", 2, `error: "2026-10-10\nfoo"` + bad}, // still one line
		{[]string{"cn", "--help"}, t0, "", 2, `error: "--help"` + bad},                   // options stand alone
		{nil, t0, "", 2, "error: missing region — usage: isworkday REGION [DATE], e.g. isworkday cn\n"},
		{[]string{"2026-10-01"}, t0, "", 2, `error: unsupported region "2026-10-01"` + unsupported},
		{[]string{"today"}, t0, "", 2, `error: unsupported region "today"` + unsupported},
		{[]string{"jp"}, t0, "", 2, `error: unsupported region "jp"` + unsupported},
		{[]string{"jp", "2026-10-10"}, t0, "", 2, `error: unsupported region "jp"` + unsupported},
		{[]string{"2026-10-10", "cn"}, t0, "", 2, `error: unsupported region "2026-10-10"` + unsupported},
		{[]string{""}, t0, "", 2, `error: unsupported region ""` + unsupported},
		{[]string{"cn\nfoo"}, t0, "", 2, `error: unsupported region "cn\nfoo"` + unsupported},
		{[]string{"--json"}, t0, "", 2, `error: unknown option "--json" — see isworkday --help` + "\n"},
		{[]string{"--x\ny"}, t0, "", 2, `error: unknown option "--x\ny" — see isworkday --help` + "\n"},
		{[]string{"-"}, t0, "", 2, `error: unknown option "-" — see isworkday --help` + "\n"},
		{[]string{"cn", "a", "b"}, t0, "", 2, tooMany},
		{[]string{"--help", "x"}, t0, "", 2, tooMany},
		{[]string{"-V", "cn"}, t0, "", 2, tooMany},
		{[]string{"cn", "2999-01-01"}, t0, "unknown\n", 3, "error: no data for 2999-01-01 yet — covered through " + snap.Range[1] + "\n"},
		{[]string{"cn", "2006-05-01"}, t0, "unknown\n", 3, "error: no data for 2006-05-01 — covered from 2007-01-01\n"},
		// the local date is noted only when it differs, and only for today/tomorrow
		{[]string{"cn"}, pdt, "true\n", 0, "2026-10-19 Mon · weekday (local date is 2026-10-18)\n"},
		{[]string{"cn", "today"}, pdt, "true\n", 0, "2026-10-19 Mon · weekday (local date is 2026-10-18)\n"},
		{[]string{"cn", "tomorrow"}, pdt, "true\n", 0, "2026-10-20 Tue · weekday (local date is 2026-10-18)\n"},
		{[]string{"cn", "2026-10-19"}, pdt, "true\n", 0, "2026-10-19 Mon · weekday\n"},
		{[]string{"cn", "today"}, cst, "true\n", 0, "2026-10-19 Mon · weekday\n"},
	} {
		out, errOut, code := runCLI(Env{Now: func() time.Time { return c.now }}, c.args...)
		if out != c.out || code != c.code || errOut != c.stderr {
			t.Errorf("isworkday %q\n got %q %d %q\nwant %q %d %q", c.args, out, code, errOut, c.out, c.code, c.stderr)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	s, hits := server(t, body(cn.snapshot))
	env := Env{Now: func() time.Time { return t0 }, Client: &http.Client{Timeout: timeout}, CacheDir: t.TempDir(), URLs: []string{s.URL}}

	help, _, _ := runCLI(env, "--help")
	for _, a := range []string{"-h", "--help"} {
		out, errOut, code := runCLI(env, a)
		if out != help || code != 0 || errOut != "" || strings.Count(out, "\n") > 25 ||
			!strings.HasPrefix(out, "isworkday — is DATE a statutory workday in REGION?\n") {
			t.Errorf("%s: %d %q\n%s", a, code, errOut, out)
		}
	}
	skill, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(string(skill), "<!-- isworkday --help -->")
	_, block, _ := strings.Cut(after, "```text\n")
	if block, _, _ = strings.Cut(block, "```"); block != help {
		t.Errorf("SKILL.md reference block differs from --help:\n%s", block)
	}

	snap := snapshot(t)
	re := regexp.MustCompile(`^isworkday \S+ · cn data \d{4}-\d{2}-\d{2} \(\d{4}-\d{2}-\d{2}\.\.\d{4}-\d{2}-\d{2}\) from (embedded|cache)\n$`)
	older, _ := json.Marshal(Data{Schema: 1, Region: "cn", Version: "2020-01-01", Range: [2]string{"2007-01-01", "2020-12-28"}})
	newer, _ := json.Marshal(Data{Schema: 1, Region: "cn", Version: "2099-01-01", Range: [2]string{"2007-01-01", "2099-12-28"}})
	for _, c := range []struct {
		cache []byte
		want  string
	}{
		{nil, " · cn data " + snap.Version + " (" + snap.Range[0] + ".." + snap.Range[1] + ") from embedded\n"},
		{older, " · cn data " + snap.Version + " (" + snap.Range[0] + ".." + snap.Range[1] + ") from embedded\n"},
		{newer, " · cn data 2099-01-01 (2007-01-01..2099-12-28) from cache\n"},
	} {
		if c.cache != nil {
			os.WriteFile(filepath.Join(env.CacheDir, "cn.json"), c.cache, 0o644)
		}
		for _, a := range []string{"-V", "--version"} {
			out, errOut, code := runCLI(env, a)
			if !re.MatchString(out) || !strings.HasSuffix(out, c.want) || code != 0 || errOut != "" {
				t.Errorf("%s: %d %q %q, want suffix %q", a, code, out, errOut, c.want)
			}
		}
	}
	if hits.Load() != 0 {
		t.Errorf("--help/--version went online %d times", hits.Load())
	}
}

// future is the snapshot extended through 2099, with 2099-10-01 a holiday:
// only data fetched from the network can answer for that date.
func future(t *testing.T) []byte {
	snap := snapshot(t)
	f := snap
	f.Version, f.Verified, f.Range[1] = "2099-01-01", "2099-01-01", "2099-12-28"
	f.Holidays = map[string]string{"2099-10-01": "国庆节"}
	for k, v := range snap.Holidays {
		f.Holidays[k] = v
	}
	b, _ := json.Marshal(f)
	return b
}

// TestNoUpgradeNeeded: a binary whose snapshot ends this year answers next
// year from the network.
func TestNoUpgradeNeeded(t *testing.T) {
	snap := snapshot(t)
	up, _ := server(t, body(future(t)))
	down, _ := server(t, body(nil))
	down.Close()
	for _, c := range []struct {
		name, arg, out string
		code           int
		urls           []string
		stderr         *regexp.Regexp
	}{
		{"next year online", "2099-10-01", "false\n", 1, []string{up.URL}, regexp.MustCompile(`^2099-10-01 Thu · holiday \(国庆节\)\n$`)},
		{"offline", "2099-10-01", "unknown\n", 3, nil, regexp.MustCompile(`^error: no data for 2099-10-01 yet — covered through ` + snap.Range[1] + `\n$`)},
		{"unreachable", "2026-10-01", "false\n", 1, []string{down.URL},
			regexp.MustCompile(`^warning: using data as of ` + snap.Version + ` — refresh failed: 127\.0\.0\.1:\d+: .+\n2026-10-01 Thu · holiday \(国庆节\)\n$`)},
	} {
		env := Env{Now: func() time.Time { return t0 }, CacheDir: t.TempDir(), URLs: c.urls}
		if c.urls != nil { // nil URLs would mean the real ones: offline instead
			env.Client = &http.Client{Timeout: timeout}
		}
		out, errOut, code := runCLI(env, "cn", c.arg)
		if out != c.out || code != c.code || !c.stderr.MatchString(errOut) {
			t.Errorf("%s: got %q %d %q", c.name, out, code, errOut)
		}
	}
}

func TestSnapshot(t *testing.T) {
	d := snapshot(t)
	if err := d.Validate(cn); err != nil || d.Range[0] != "2007-01-01" {
		t.Fatalf("embedded data/cn.json: %v, range %v", err, d.Range)
	}
	if d.Holidays["2026-10-01"] != "国庆节" || d.Workdays["2026-10-10"] != "国庆节" {
		t.Errorf("embedded data/cn.json lacks the 2026 National Day schedule")
	}
}

// TestProcess checks exit codes through a real os.Exit.
func TestProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "isworkday")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	s, hits := server(t, body(future(t)))
	for _, c := range []struct {
		args []string
		out  string
		code int
	}{
		{[]string{"cn", "2026-10-19"}, "true\n", 0},
		{[]string{"cn", "2026-10-18"}, "false\n", 1},
		{[]string{"cn", "2026-02-30"}, "", 2},
		{nil, "", 2},
		{[]string{"cn", "2999-01-01"}, "unknown\n", 3},
		{[]string{"cn", "2099-10-01"}, "false\n", 1}, // only right if ISWORKDAY_DATA_URL was used
	} {
		cmd := exec.Command(bin, c.args...)
		cmd.Env = append(os.Environ(), "ISWORKDAY_DATA_URL="+s.URL, "XDG_CACHE_HOME="+t.TempDir())
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		code := 0
		if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if out.String() != c.out || code != c.code {
			t.Errorf("isworkday %q: %q %d, want %q %d", c.args, out.String(), code, c.out, c.code)
		}
	}
	if hits.Load() == 0 {
		t.Error("ISWORKDAY_DATA_URL was never fetched")
	}
}
