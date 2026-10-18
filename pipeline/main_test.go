package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const placeholder = `{"year": 2027, "papers": [], "days": []}`

func TestRun(t *testing.T) {
	golden := read(t, "testdata/golden.json")
	// prev is golden with replacements, standing in for the committed file.
	prev := func(oldnew ...string) []byte { return []byte(strings.NewReplacer(oldnew...).Replace(string(golden))) }
	cdConflict := map[string]string{"workdays/2026-10-10": ""}
	cdNewYear2008 := "New Year's Day,元旦,1"
	cases := []struct {
		name    string
		today   string            // default 2026-10-17
		args    []string          // appended to the defaults; $SRV is the test server
		hcn     map[string]string // file → content; "" removes the file
		cd      map[string]string // "table/date" → value; "" removes the entry
		patches map[string]string // file → content
		prev    []byte            // the existing output file; nil means none
		code    int
		stderr  string   // exact, with the server URL and temp dir as $SRV and $DIR
		golden  bool     // output equals testdata/golden.json
		want    []string // "key=value" on flat(output); "key=" means absent
	}{
		{name: "sources agree", golden: true},
		{name: "second run changes nothing", prev: golden, stderr: "nothing changed\n", golden: true},
		{name: "placeholder year is not published", hcn: map[string]string{"2027.json": placeholder}, golden: true, want: []string{"years[2027]="}},
		{name: "only holiday-cn has 2027",
			hcn:  map[string]string{"2027.json": `{"papers":["https://www.gov.cn/2027"],"days":[{"name":"元旦","date":"2027-01-01","isOffDay":true},` + hcnPad(2027) + `]}`},
			want: []string{"range[1]=2027-12-28", "years[2027]=https://www.gov.cn/2027", "holidays[2027-01-01]=元旦"}},
		{name: "a year after next is not taken yet",
			hcn: map[string]string{
				"2027.json": `{"papers":["p2027"],"days":[{"name":"元旦","date":"2027-01-01","isOffDay":true},` + hcnPad(2027) + `]}`,
				"2028.json": `{"papers":["p2028"],"days":[{"name":"元旦","date":"2028-01-03","isOffDay":true},` + hcnPad(2028) + `]}`},
			want: []string{"range[1]=2027-12-28", "years[2027]=p2027", "years[2028]=", "holidays[2028-01-03]="}},
		{name: "only chinese-days has 2027",
			cd:   cdPad(map[string]string{"holidays/2027-01-01": "New Year's Day,元旦,1", "holidays/2027-04-05": "Tomb-sweeping Day,清明,1"}, 2027),
			want: []string{"range[1]=2027-12-28", "years[2027]=", "holidays[2027-04-05]=清明节"}},
		{name: "conflict", cd: cdConflict, code: 1, stderr: "conflict 2026-10-10: holiday-cn=on chinese-days=off\n"},
		{name: "patch settles the conflict and is not redundant", cd: cdConflict,
			patches: map[string]string{"2026-10-10-national-day.json": `{"workdays":{"2026-10-10":"国庆节"}}`},
			golden:  true},
		{name: "a default patch settles a conflict and is not redundant",
			cd:      map[string]string{"holidays/2026-10-12": "National Day,国庆节,3"},
			patches: map[string]string{"a.json": `{"default":["2026-10-12"]}`}, golden: true},
		{name: "every unsettled conflict is printed",
			cd:      map[string]string{"workdays/2026-02-14": "", "workdays/2026-10-10": "", "holidays/2026-10-12": "National Day,国庆节,3"},
			patches: map[string]string{"a.json": `{"default":["2026-10-12"]}`}, code: 1,
			stderr: "conflict 2026-02-14: holiday-cn=on chinese-days=off\nconflict 2026-10-10: holiday-cn=on chinese-days=off\n"},
		{name: "a Monday listed as a workday is a no-op",
			hcn:  map[string]string{"2020.json": `{"papers":["p2020"],"days":[{"name":"元旦","date":"2020-01-01","isOffDay":true},{"name":"春节","date":"2020-02-03","isOffDay":false},` + hcnPad(2020) + `]}`},
			cd:   cdPad(map[string]string{"holidays/2020-01-01": "New Year's Day,元旦,1"}, 2020),
			want: []string{"workdays[2020-02-03]=", "holidays[2020-01-01]=元旦", "years[2020]=p2020"}},
		{name: "Dec 29-31 filed under the next year",
			hcn: map[string]string{
				"2007.json": `{"papers":["p2007"],"days":[` + hcnPad(2007) + `]}`,
				"2008.json": `{"papers":["p2008"],"days":[
				{"name":"元旦","date":"2007-12-29","isOffDay":false},{"name":"元旦","date":"2007-12-30","isOffDay":true},
				{"name":"元旦","date":"2007-12-31","isOffDay":true},{"name":"元旦","date":"2008-01-01","isOffDay":true},` + hcnPad(2008) + `]}`},
			cd: cdPad(map[string]string{"workdays/2007-12-29": cdNewYear2008, "holidays/2007-12-30": cdNewYear2008,
				"holidays/2007-12-31": cdNewYear2008, "holidays/2008-01-01": cdNewYear2008}, 2007, 2008),
			want: []string{"workdays[2007-12-29]=元旦", "holidays[2007-12-30]=", "holidays[2007-12-31]=元旦", "years[2008]=p2008", "years[2007]=p2007"}},
		{name: "weekend rest listed by one source only", cd: map[string]string{"holidays/2026-10-03": ""}, golden: true},
		{name: "chinese-days contradicts itself", cd: map[string]string{"workdays/2026-10-01": "National Day,国庆节,3"}, code: 1,
			stderr: "error: chinese-days: 2026-10-01 is listed as both a holiday and a workday\n"},
		{name: "holiday-cn contradicts itself across files", code: 1,
			hcn:    map[string]string{"2027.json": `{"papers":["p2027"],"days":[{"name":"国庆节","date":"2026-10-01","isOffDay":false}]}`},
			stderr: "error: holiday-cn: 2026-10-01 is listed as both a holiday and a workday\n"},
		{name: "patch back to the default", patches: map[string]string{"a.json": `{"default":["2026-10-10"]}`}, want: []string{"workdays[2026-10-10]="}},
		{name: "redundant patch",
			patches: map[string]string{"a.json": `{"source":"https://www.gov.cn/x","note":"n","holidays":{"2026-10-01":"国庆节"}}`},
			stderr:  "note: patches/a.json is redundant (upstreams agree) — delete it\n", golden: true},
		{name: "agreeing with an earlier patch is not redundant", want: []string{"holidays[2026-10-12]=国庆节"},
			patches: map[string]string{"a.json": `{"holidays":{"2026-10-12":"国庆节"}}`, "b.json": `{"holidays":{"2026-10-12":"国庆节"}}`}},
		{name: "patch outside the range", patches: map[string]string{"a.json": `{"holidays":{"2027-01-04":"元旦"}}`}, code: 1,
			stderr: "error: patches/a.json: 2027-01-04 is outside 2007-01-01..2026-12-28\n"},
		{name: "patch lists a date twice", patches: map[string]string{"a.json": `{"holidays":{"2026-10-12":"国庆节"},"workdays":{"2026-10-12":"国庆节"}}`}, code: 1,
			stderr: "error: patches/a.json: 2026-10-12 is listed twice\n"},
		{name: "patch puts a holiday on a Saturday", patches: map[string]string{"a.json": `{"holidays":{"2026-10-10":"国庆节"}}`}, code: 1,
			stderr: "error: holidays: 2026-10-10 \"国庆节\" must be a named Mon–Fri date within 2007-01-01..2026-12-28\n"},
		{name: "patch with a misspelt key", patches: map[string]string{"a.json": `{"holiday":{"2026-10-12":"国庆节"}}`}, code: 1,
			stderr: "error: patches/a.json: json: unknown field \"holiday\"\n"},
		{name: "range never shrinks", prev: prev(`"2026-12-28"`, `"2027-12-28"`), code: 1,
			stderr: "error: range would shrink from 2027-12-28 to 2026-12-28: an upstream dropped a year\n"},
		{name: "a year never loses its holidays", prev: prev(`"2025-01-01"`, `"2024-10-01": "国庆节", "2025-01-01"`), code: 1,
			stderr: "error: 2024 would lose all its holidays: the upstreams dropped the year\n"},
		{name: "a published year without holidays is rejected",
			hcn:  map[string]string{"2020.json": `{"papers":["p2020"],"days":[{"name":"春节","date":"2020-01-19","isOffDay":false}]}`},
			cd:   map[string]string{"workdays/2020-01-19": "Spring Festival,春节,4"},
			code: 1, stderr: "error: 2020 has only 0 holidays: a partial year?\n"},
		{name: "a new year with only New Year's Day is rejected", prev: golden, code: 1,
			hcn:    map[string]string{"2027.json": `{"papers":["p2027"],"days":[{"name":"元旦","date":"2027-01-01","isOffDay":true}]}`},
			stderr: "error: 2027 has only 1 holiday: a partial year?\n"},
		{name: "heartbeat after 31 days", prev: prev(`"version": "2026-10-17"`, `"version": "2026-09-01"`, `"verified": "2026-10-17"`, `"verified": "2026-09-16"`),
			want: []string{"version=2026-09-01", "verified=2026-10-17"}},
		{name: "heartbeat at 30 days", prev: prev(`"verified": "2026-10-17"`, `"verified": "2026-09-17"`), want: []string{"verified=2026-10-17"}},
		{name: "no heartbeat at 29 days", prev: prev(`"verified": "2026-10-17"`, `"verified": "2026-09-18"`),
			stderr: "nothing changed\n", want: []string{"verified=2026-09-18"}},
		{name: "content change", golden: true,
			prev: prev(`"version": "2026-10-17"`, `"version": "2026-09-01"`, `"verified": "2026-10-17"`, `"verified": "2026-10-01"`, `"2026-10-01": "国庆节"`, `"2026-10-01": "国庆"`)},
		{name: "deadline from Dec 20", today: "2026-12-20", code: 2, want: []string{"version=2026-12-20"},
			stderr: "deadline: schedule for 2027 is still missing on 2026-12-20 — check holiday-cn and chinese-days\n"},
		{name: "deadline goes on into the new year", today: "2027-01-05", code: 2, want: []string{"version=2027-01-05"},
			stderr: "deadline: schedule for 2027 is still missing on 2027-01-05 — check holiday-cn and chinese-days\n"},
		{name: "no deadline on Dec 19", today: "2026-12-19", want: []string{"version=2026-12-19"}},
		{name: "registry request fails", args: []string{"-cd-latest", "$SRV/down"}, code: 1,
			stderr: "error: chinese-days: GET $SRV/down: 404 Not Found\n"},
		{name: "unknown flag", args: []string{"-all"}, code: 1, stderr: "flag provided but not defined: -all\n" + usage + "\n"},
		{name: "-hcn is required", args: []string{"-hcn", ""}, code: 1, stderr: usage + "\n"},
		{name: "-today must be strict", today: "2026-1-2", code: 1, stderr: "error: -today: want YYYY-MM-DD, got \"2026-1-2\"\n"},
		{name: "empty holiday-cn clone", hcn: map[string]string{"2025.json": "", "2026.json": ""}, code: 1,
			stderr: "error: holiday-cn: no YYYY.json in $DIR/hcn\n"},
		{name: "holiday-cn without a published year", hcn: map[string]string{"2025.json": placeholder, "2026.json": ""}, code: 1,
			stderr: "error: holiday-cn: no published year\n"},
		{name: "unreadable previous file", prev: []byte("{"), code: 1, stderr: "error: $DIR/cn.json: unexpected end of JSON input\n"},
	}

	latest := read(t, "testdata/latest.json")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{"cn.json": string(c.prev)}
			for _, f := range []string{"2025.json", "2026.json"} {
				files["hcn/"+f] = string(read(t, "testdata/hcn/"+f))
			}
			for f, s := range c.hcn {
				files["hcn/"+f] = s
			}
			for f, s := range c.patches {
				files["patches/"+f] = s
			}
			for f, s := range files {
				if s != "" {
					os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755)
					os.WriteFile(filepath.Join(dir, f), []byte(s), 0o644)
				}
			}

			var cd map[string]map[string]string
			json.Unmarshal(read(t, "testdata/cd.json"), &cd)
			for k, v := range c.cd {
				tbl, date, _ := strings.Cut(k, "/")
				if cd[tbl][date] = v; v == "" {
					delete(cd[tbl], date)
				}
			}
			cdJSON, _ := json.Marshal(cd)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					w.Write(latest)
				case "/chinese-days@1.5.9/dist/chinese-days.json": // only the version latest names
					w.Write(cdJSON)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			today := c.today
			if today == "" {
				today = "2026-10-17"
			}
			out := filepath.Join(dir, "cn.json")
			args := []string{"-hcn", filepath.Join(dir, "hcn"), "-out", out, "-patches", filepath.Join(dir, "patches"), "-today", today,
				"-cd-latest", srv.URL + "/latest", "-cd", srv.URL + "/chinese-days@%s/dist/chinese-days.json"}
			for _, a := range c.args {
				args = append(args, strings.ReplaceAll(a, "$SRV", srv.URL))
			}
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)

			gotErr := strings.NewReplacer(srv.URL, "$SRV", dir, "$DIR").Replace(stderr.String())
			if code != c.code || gotErr != c.stderr {
				t.Fatalf("exit %d, stderr:\n%s\nwant exit %d, stderr:\n%s", code, gotErr, c.code, c.stderr)
			}
			b, err := os.ReadFile(out)
			written := err == nil && !bytes.Equal(b, c.prev)
			var d Data
			json.Unmarshal(b, &d)
			summary := ""
			if written {
				summary = fmt.Sprintf("cn.json: through %s, %d holidays / %d workdays, version %s, verified %s\n",
					d.Range[1], len(d.Holidays), len(d.Workdays), d.Version, d.Verified)
			}
			if code == 1 && written {
				t.Error("wrote the file although it failed")
			}
			if stdout.String() != summary {
				t.Errorf("stdout %q, want %q", stdout.String(), summary)
			}
			if c.golden && !bytes.Equal(b, golden) {
				t.Errorf("output differs from testdata/golden.json:\n%s", b)
			}
			f := flat(d)
			for _, w := range c.want {
				k, v, _ := strings.Cut(w, "=")
				if f[k] != v {
					t.Errorf("%s = %q, want %q", k, f[k], v)
				}
			}
		})
	}
}

// The committed file passes every invariant and is in the pipeline's encoding.
func TestRepoData(t *testing.T) {
	b := read(t, "../data/cn.json")
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	years := map[int]bool{}
	for y := 2007; fmt.Sprintf("%d-%s", y, lastDay) <= d.Range[1]; y++ {
		years[y] = true
	}
	if err := check(d, Data{}, years); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encode(d), b) {
		t.Error("../data/cn.json is not in the pipeline's encoding")
	}
}

// padDates are minHolidays Mon–Fri dates of year y from Oct 1 on: with them a
// fixture year is complete enough for check.
func padDates(y int) (ks []string) {
	for t := time.Date(y, 10, 1, 0, 0, 0, 0, time.UTC); len(ks) < minHolidays; t = t.AddDate(0, 0, 1) {
		if working(t.Weekday()) {
			ks = append(ks, t.Format(time.DateOnly))
		}
	}
	return ks
}

// hcnPad is padDates(y) as holiday-cn days, to splice into a y.json.
func hcnPad(y int) string {
	var days []string
	for _, k := range padDates(y) {
		days = append(days, `{"name":"国庆节","date":"`+k+`","isOffDay":true}`)
	}
	return strings.Join(days, ",")
}

// cdPad adds padDates of each year to the chinese-days entries cd.
func cdPad(cd map[string]string, years ...int) map[string]string {
	for _, y := range years {
		for _, k := range padDates(y) {
			cd["holidays/"+k] = "National Day,国庆节,3"
		}
	}
	return cd
}

// flat names every value in d so that a case can assert "key=value".
func flat(d Data) map[string]string {
	f := map[string]string{"range[1]": d.Range[1], "version": d.Version, "verified": d.Verified}
	for k, v := range d.Holidays {
		f["holidays["+k+"]"] = v
	}
	for k, v := range d.Workdays {
		f["workdays["+k+"]"] = v
	}
	for k, v := range d.Years {
		f["years["+k+"]"] = strings.Join(v, " ")
	}
	return f
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
