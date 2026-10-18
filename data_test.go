package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 17, 4, 0, 0, 0, time.UTC) // noon in Beijing

func fixtureBytes(t *testing.T) []byte {
	b, err := os.ReadFile("testdata/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixture(t *testing.T) Data {
	var d Data
	if err := json.Unmarshal(fixtureBytes(t), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// testRegion is cn with a small, old snapshot.
func testRegion(t *testing.T) region {
	r := cn
	r.snapshot = fixtureBytes(t)
	return r
}

func readCache(path string, r region) (Data, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Data{}, err
	}
	return parse(b, r)
}

func variant(t *testing.T, version, verified string, edit func(*Data)) []byte {
	d := fixture(t)
	d.Version, d.Verified = version, verified
	if edit != nil {
		edit(&d)
	}
	b, _ := json.Marshal(d)
	return b
}

type reply func(http.ResponseWriter, *http.Request)

func body(b []byte) reply { return func(w http.ResponseWriter, _ *http.Request) { w.Write(b) } }
func status(code int) reply {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}
func hang(_ http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(5 * time.Second):
	}
}

// server answers every request with rep, counting hits and checking the headers.
func server(t *testing.T, rep reply) (*httptest.Server, *atomic.Int32) {
	hits := new(atomic.Int32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.UserAgent(), "isworkday/") || r.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("request headers: %v", r.Header)
		}
		rep(w, r)
	}))
	t.Cleanup(s.Close)
	return s, hits
}

const timeout = 300 * time.Millisecond

func TestLoad(t *testing.T) {
	r := testRegion(t)
	v := func(version, verified string) []byte { return variant(t, version, verified, nil) }
	old, cur := v("2026-06-01", "2026-06-01"), v("2026-10-17", "2026-10-17")
	const failed = "using data as of 2026-06-01 — refresh failed: "
	stale, fresh := 12*time.Hour+time.Minute, 12*time.Hour-time.Minute // pins the 12 h of --help
	extended := variant(t, "2026-10-17", "2026-10-17", func(d *Data) { d.Range[1] = "2027-12-28" })
	for _, tc := range []struct {
		name   string
		cache  []byte        // nil: no cache file
		age    time.Duration // of the cache file at t0
		serve  []reply       // one server per URL
		want   string        // version/verified[/range end] of the answer
		source string
		hits   int
		warn   string // H1, H2 stand for the servers' host:port
	}{
		{"no cache", nil, 0, []reply{body(cur)}, "2026-10-17/2026-10-17", "online", 1, ""},
		{"fresh cache", old, fresh, []reply{body(cur)}, "2026-06-01/2026-06-01", "cache", 0, ""},
		{"stale cache", old, stale, []reply{body(cur)}, "2026-10-17/2026-10-17", "online", 1, ""},
		{"cache mtime in the future", old, -24 * time.Hour, []reply{body(cur)}, "2026-10-17/2026-10-17", "online", 1, ""},
		{"primary times out", old, stale, []reply{hang, body(cur)}, "2026-10-17/2026-10-17", "online", 2, ""},
		{"404, HTML", old, stale, []reply{status(404), body([]byte("<html>login</html>"))},
			"2026-06-01/2026-06-01", "cache", 2, failed + "H1: HTTP 404; H2: not JSON"},
		{"schema 2, truncated", old, stale, []reply{body(variant(t, "2026-10-17", "", func(d *Data) { d.Schema = 2 })), body(cur[:len(cur)/2])},
			"2026-06-01/2026-06-01", "cache", 2, failed + "H1: schema 2 not supported; H2: not JSON"},
		{"other region, timeout", old, stale, []reply{body(variant(t, "2026-10-17", "", func(d *Data) { d.Region = "us" })), hang},
			"2026-06-01/2026-06-01", "cache", 2, failed + "H1: invalid data; H2: timeout after 300ms"},
		{"no schema, range reversed", old, stale, []reply{body(variant(t, "2026-10-17", "", func(d *Data) { d.Schema = 0 })),
			body(variant(t, "2026-10-17", "", func(d *Data) { d.Range[0], d.Range[1] = d.Range[1], d.Range[0] }))},
			"2026-06-01/2026-06-01", "cache", 2, failed + "H1: invalid data; H2: invalid data"},
		{"online older than cache", cur, stale, []reply{body(old)}, "2026-10-17/2026-10-17", "cache", 1, ""},
		{"same version, newer verified", v("2026-10-10", "2026-10-10"), stale, []reply{body(v("2026-10-10", "2026-10-17"))},
			"2026-10-10/2026-10-17", "online", 1, ""},
		{"same version and verified, other content", cur, stale, []reply{body(extended)}, "2026-10-17/2026-10-17/2027-12-28", "online", 1, ""},
		{"corrupt cache", []byte("\x00\xff{garbage"), fresh, []reply{body(cur)}, "2026-10-17/2026-10-17", "online", 1, ""},
		{"fetched, verified 91 days ago", nil, 0, []reply{body(v("2026-07-18", "2026-07-18"))}, "2026-07-18/2026-07-18", "online", 1,
			"data last verified 2026-07-18, over 90 days ago — the data pipeline may have stopped; see https://github.com/PentaTea/isworkday"},
		{"fetched, verified exactly 90 days ago", nil, 0, []reply{body(v("2026-07-19", "2026-07-19"))}, "2026-07-19/2026-07-19", "online", 1, ""},
		{"verified 91 days ago, refresh fails", v("2026-07-18", "2026-07-18"), stale, []reply{status(500), status(429)},
			"2026-07-18/2026-07-18", "cache", 2, "using data as of 2026-07-18 — refresh failed: H1: HTTP 500; H2: HTTP 429"},
		{"fresh cache verified long ago, no fetch", v("2026-03-01", "2026-03-01"), fresh, []reply{body(cur)},
			"2026-03-01/2026-03-01", "cache", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cn.json")
			var before time.Time
			if tc.cache != nil {
				os.WriteFile(path, tc.cache, 0o644)
				os.Chtimes(path, t0.Add(-tc.age), t0.Add(-tc.age))
				st, _ := os.Stat(path)
				before = st.ModTime()
			}
			var urls, hosts []string
			var hits []*atomic.Int32
			for _, rep := range tc.serve {
				s, h := server(t, rep)
				urls, hosts, hits = append(urls, s.URL), append(hosts, host(s.URL)), append(hits, h)
			}
			env := Env{Now: func() time.Time { return t0 }, Client: &http.Client{Timeout: timeout}, CacheDir: dir, URLs: urls}

			d, source, warn := load(env, r, true)

			n := 0
			for _, h := range hits {
				n += int(h.Load())
			}
			wantWarn := tc.warn
			if len(hosts) == 2 {
				wantWarn = strings.NewReplacer("H1", hosts[0], "H2", hosts[1]).Replace(wantWarn)
			}
			id := func(d Data) string { return d.Version + "/" + d.Verified + "/" + d.Range[1] }
			if got := id(d); !strings.HasPrefix(got, tc.want) || source != tc.source || n != tc.hits || warn != wantWarn {
				t.Fatalf("got %s from %s after %d fetches, warn %q\nwant %s from %s after %d fetches, warn %q", got, source, n, warn, tc.want, tc.source, tc.hits, wantWarn)
			}
			cached, err := readCache(path, r)
			st, _ := os.Stat(path)
			switch {
			case tc.hits > 0 && (err != nil || !strings.HasPrefix(id(cached), tc.want)):
				t.Errorf("cache after fetch: %v %v", id(cached), err)
			case tc.hits > 0 && tc.cache != nil && st.ModTime().Equal(before):
				t.Errorf("cache mtime not refreshed by the attempt (no backoff)")
			case tc.hits == 0 && tc.cache != nil && !st.ModTime().Equal(before):
				t.Errorf("fresh cache rewritten without a fetch")
			}
		})
	}
}

func TestLoadCacheDir(t *testing.T) {
	r := testRegion(t)
	cur := variant(t, "2026-10-17", "2026-10-17", nil)
	s, hits := server(t, body(cur))
	env := Env{Now: func() time.Time { return t0 }, Client: &http.Client{Timeout: timeout}, URLs: []string{s.URL}}

	t.Run("no cache dir fetches every time", func(t *testing.T) {
		hits.Store(0)
		for range 2 {
			if d, source, warn := load(env, r, true); d.Version != "2026-10-17" || source != "online" || warn != "" {
				t.Fatalf("got %s from %s, warn %q", d.Version, source, warn)
			}
		}
		if hits.Load() != 2 {
			t.Errorf("%d fetches, want 2", hits.Load())
		}
	})

	t.Run("read-only cache dir", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		env := env
		env.CacheDir = t.TempDir()
		os.Chmod(env.CacheDir, 0o555)
		defer os.Chmod(env.CacheDir, 0o755)
		if d, _, warn := load(env, r, true); d.Version != "2026-10-17" || warn != "" {
			t.Fatalf("got %s, warn %q", d.Version, warn)
		}
		if ents, _ := os.ReadDir(env.CacheDir); len(ents) != 0 {
			t.Errorf("wrote %v into a read-only dir", ents)
		}
	})

	t.Run("concurrent loads", func(t *testing.T) {
		env := env
		env.CacheDir = t.TempDir()
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() { defer wg.Done(); load(env, r, true) }()
		}
		wg.Wait()
		if d, err := readCache(filepath.Join(env.CacheDir, "cn.json"), r); err != nil || d.Version != "2026-10-17" {
			t.Errorf("cache after concurrent writes: %v %v", d.Version, err)
		}
		if ents, _ := os.ReadDir(env.CacheDir); len(ents) != 1 {
			t.Errorf("left temp files behind: %v", ents)
		}
	})
}

// A TLS alert's reason must not put "error" into stderr.
func TestTLSAlert(t *testing.T) {
	s := httptest.NewUnstartedServer(http.NotFoundHandler())
	s.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	s.StartTLS()
	defer s.Close()
	client := s.Client()
	client.Timeout = timeout
	if _, err := fetch(client, s.URL, "isworkday/test", cn); err == nil || err.Error() != "tls: certificate required" {
		t.Errorf("fetch: %v, want the bare TLS alert", err)
	}
}

func TestCacheDir(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	for _, c := range []struct{ xdg, home, want string }{
		{xdg, home, filepath.Join(xdg, "isworkday")},
		{"relative/cache", home, filepath.Join(home, ".cache", "isworkday")},
		{"", home, filepath.Join(home, ".cache", "isworkday")},
		{"", "", ""},
	} {
		t.Setenv("XDG_CACHE_HOME", c.xdg)
		t.Setenv("HOME", c.home)
		if got := cacheDir(); got != c.want {
			t.Errorf("XDG_CACHE_HOME=%q HOME=%q: cacheDir() = %q, want %q", c.xdg, c.home, got, c.want)
		} else if st, err := os.Stat(got); got != "" && (err != nil || !st.IsDir()) {
			t.Errorf("cacheDir() did not create %s", got)
		}
	}
}
