package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ttl = 12 * time.Hour

// Env is everything load touches outside the process.
type Env struct {
	Now      func() time.Time
	Client   *http.Client // its Timeout bounds each fetch; nil ⇒ offline
	CacheDir string       // "" ⇒ no cache
	URLs     []string     // if non-nil, tried in order instead of the region's own
}

// cacheDir follows XDG on every OS (os.UserCacheDir would pick ~/Library on
// macOS).
func cacheDir() string {
	dir := ""
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" && filepath.IsAbs(x) {
		dir = filepath.Join(x, "isworkday")
	} else if h := os.Getenv("HOME"); h != "" {
		dir = filepath.Join(h, ".cache", "isworkday")
	}
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return "" // no cache: every call refreshes
	}
	return dir
}

// load returns the newest of snapshot, cache and, if refresh and the cache is
// older than ttl, the network.
func load(env Env, r region, refresh bool) (best Data, source, warn string) {
	best, _ = parse(r.snapshot, r) // validity is guaranteed by tests
	source = "embedded"
	path := ""
	if env.CacheDir != "" {
		path = filepath.Join(env.CacheDir, r.code+".json")
	}
	b, _ := os.ReadFile(path)
	cached, cacheErr := parse(b, r)
	if cacheErr == nil && newer(best, cached) {
		best, source = cached, "cache"
	}
	urls := r.urls
	if env.URLs != nil {
		urls = env.URLs
	}
	if !refresh || env.Client == nil || len(urls) == 0 {
		return
	}
	now := env.Now()
	// The mtime of the last attempt, successful or not: a blocked network costs
	// one timeout per ttl. A future mtime counts as stale.
	if st, err := os.Stat(path); cacheErr == nil && err == nil {
		if age := now.Sub(st.ModTime()); age >= 0 && age < ttl {
			return
		}
	}
	var errs []string
	for _, u := range urls {
		got, err := fetch(env.Client, u, "isworkday/"+version+" (+"+repoURL+")", r)
		if err != nil {
			errs = append(errs, host(u)+": "+err.Error())
			continue
		}
		if newer(best, got) {
			best, source = got, "online"
		}
		errs = nil
		break
	}
	if path != "" {
		b, _ := json.Marshal(best)
		_ = writeAtomic(path, b)
	}
	if errs != nil {
		warn = "using data as of " + best.Version + " — refresh failed: " + strings.Join(errs, "; ")
	} else if v, ok := ParseDate(best.Verified); ok && source == "online" && r.date(now).daysSince(v) > 90 {
		// Only freshly fetched data says anything about the pipeline.
		warn = "data last verified " + best.Verified + ", over 90 days ago — the data pipeline may have stopped; see " + repoURL
	}
	return
}

func fetch(client *http.Client, u, ua string, r region) (Data, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return Data{}, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Cache-Control", "no-cache") // make intermediate caches revalidate
	resp, err := client.Do(req)
	if err != nil {
		return Data{}, netReason(err, client.Timeout)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Data{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Data{}, netReason(err, client.Timeout)
	}
	return parse(b, r)
}

// netReason shortens transport errors for the one-line warning.
func netReason(err error, timeout time.Duration) error {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return fmt.Errorf("timeout after %v", timeout)
	}
	// Strip "remote error: " from TLS alerts: no "error" in a warning line.
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "remote error" {
		return oe.Err
	}
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// writeAtomic writes through a temp file and a rename.
func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err = errors.Join(err, f.Close(), os.Chmod(f.Name(), 0o644)); err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
