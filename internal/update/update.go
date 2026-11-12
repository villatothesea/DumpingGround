// Package update checks the project's GitHub releases for a newer tag.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LatestURL is the repository's stable-release endpoint. It returns the
// newest non-draft, non-prerelease release GitHub has marked latest.
const LatestURL = "https://api.github.com/repos/villatothesea/DumpingGround/releases/latest"

// Release is the small part of a GitHub release the UI needs.
type Release struct {
	Tag         string
	Name        string
	URL         string
	PublishedAt time.Time
}

type releaseJSON struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// Latest fetches the newest stable release. The endpoint is injectable so
// tests can point it at a local server.
func Latest(ctx context.Context, client *http.Client, url string) (Release, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "DumpingGround")
	res, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Release{}, fmt.Errorf("GitHub releases: %s", res.Status)
	}
	var r releaseJSON
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return Release{}, err
	}
	tag := r.TagName
	if tag == "" {
		tag = r.Name
	}
	if tag == "" {
		return Release{}, fmt.Errorf("GitHub releases: empty tag")
	}
	return Release{Tag: tag, Name: r.Name, URL: r.HTMLURL, PublishedAt: r.PublishedAt}, nil
}

// Newer reports whether latest names a newer release than current. The
// comparator understands the project's v-prefixed three-part tags and
// treats a prerelease suffix as older than the same stable version.
func Newer(current, latest string) bool {
	cur, curPre, ok1 := parseVersion(current)
	next, nextPre, ok2 := parseVersion(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range cur {
		if next[i] != cur[i] {
			return next[i] > cur[i]
		}
	}
	return curPre && !nextPre
}

func parseVersion(s string) ([3]int, bool, bool) {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(s), "v"))
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	pre := false
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = i+1 < len(s)
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return out, false, false
	}
	for i, p := range parts {
		if p == "" {
			return out, false, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false, false
		}
		out[i] = n
	}
	return out, pre, true
}
