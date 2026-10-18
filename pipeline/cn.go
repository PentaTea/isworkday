package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Everything specific to mainland China and its two upstreams lives here.
const (
	region        = "cn"
	offset        = 8 * time.Hour // "today" is the Beijing date
	firstDay      = "2007-01-01"  // holiday-cn starts here; chinese-days' 2004–2006 would be single-source
	lastDay       = "12-28"       // announcements reach back to Dec 29–31 of the previous year, never earlier
	deadlineDays  = 8             // the latest announcement so far came on Dec 16
	minHolidays   = 10            // per year; 2007–2026 had 16–23 Mon–Fri holidays each
	cdLatest      = "https://registry.npmjs.org/chinese-days/latest"
	cdFile        = "https://cdn.jsdelivr.net/npm/chinese-days@%s/dist/chinese-days.json" // exact version: the unversioned path is cached 12 h
	upstreamNames = "holiday-cn and chinese-days"
)

func working(w time.Weekday) bool { return w != time.Saturday && w != time.Sunday }

// canonical unifies the upstreams' spellings of a festival; display only.
var canonical = map[string]string{
	"清明": "清明节", "端午": "端午节", "中秋": "中秋节",
	"“五一”": "劳动节", "“五一”国际劳动节": "劳动节", "“十一”": "国庆节",
	"中秋节、国庆节": "国庆节、中秋节",
}

func canon(name string) string {
	if c, ok := canonical[name]; ok {
		return c
	}
	return name
}

// upstreams lists the sources in name priority: holiday-cn keeps the official wording.
func upstreams(hcnDir, latestURL, fileURL string) ([]source, error) {
	hcn, err := holidayCN(hcnDir)
	if err != nil {
		return nil, err
	}
	cd, err := chineseDays(&http.Client{Timeout: 30 * time.Second}, latestURL, fileURL)
	return []source{hcn, cd}, err
}

// holidayCN reads a clone of holiday-cn; a year with empty papers or days is an
// unannounced placeholder.
func holidayCN(dir string) (source, error) {
	s := source{name: "holiday-cn", days: map[string]day{}, years: map[int][]string{}}
	files, _ := filepath.Glob(filepath.Join(dir, "[0-9][0-9][0-9][0-9].json"))
	if len(files) == 0 {
		return s, fmt.Errorf("holiday-cn: no YYYY.json in %s", dir)
	}
	for _, f := range files {
		var j struct {
			Papers []string
			Days   []struct {
				Name, Date string
				IsOffDay   bool
			}
		}
		b, err := os.ReadFile(f)
		if err == nil {
			err = json.Unmarshal(b, &j)
		}
		if err != nil {
			return s, fmt.Errorf("holiday-cn: %s: %w", filepath.Base(f), err)
		}
		if len(j.Papers) > 0 && len(j.Days) > 0 {
			y, _ := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".json"))
			s.years[y] = j.Papers
			for _, d := range j.Days {
				if err := s.add(d.Date, !d.IsOffDay, d.Name); err != nil {
					return s, err
				}
			}
		}
	}
	return s, nil
}

// chineseDays fetches the latest chinese-days release; inLieuDays is ignored.
func chineseDays(c *http.Client, latestURL, fileURL string) (source, error) {
	s := source{name: "chinese-days", days: map[string]day{}, years: map[int][]string{}}
	var latest struct{ Version string }
	var j struct{ Holidays, Workdays map[string]string } // "English,中文,days"
	err := getJSON(c, latestURL, &latest)
	if err == nil {
		err = getJSON(c, fmt.Sprintf(fileURL, latest.Version), &j)
	}
	if err != nil {
		return s, fmt.Errorf("chinese-days: %w", err)
	}
	for i, m := range []map[string]string{j.Holidays, j.Workdays} { // days off, then days on
		for k, v := range m {
			f := strings.Split(v, ",")
			if err := s.add(k, i == 1, f[min(1, len(f)-1)]); err != nil {
				return s, err
			}
			y, _ := strconv.Atoi(k[:min(4, len(k))])
			s.years[y] = nil
		}
	}
	return s, nil
}

// add records one upstream entry; an upstream contradicting itself is an error.
func (s source) add(date string, on bool, name string) error {
	if v, ok := s.days[date]; ok && v.on != on {
		return fmt.Errorf("%s: %s is listed as both a holiday and a workday", s.name, date)
	}
	s.days[date] = day{on, canon(name)}
	return nil
}

func getJSON(c *http.Client, url string, v any) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}
