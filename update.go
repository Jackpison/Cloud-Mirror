package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var updateURL = "https://api.github.com/repos/Jackpison/Cloud-Mirror/releases/latest"

const releasesPage = "https://github.com/Jackpison/Cloud-Mirror/releases/latest"

type updateCache struct {
	mu   sync.Mutex
	at   time.Time
	info UpdateInfo
}

// UpdateInfo says whether a newer CloudMirror has been released.
type UpdateInfo struct {
	Newer  bool   `json:"newer"`
	Latest string `json:"latest"`
	URL    string `json:"url"`
}

// cmpVersion compares dotted numbers such as 0.1.2 and 0.10.0: -1, 0 or 1.
func cmpVersion(a, b string) int {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// hUpdate asks GitHub (at most every six hours) whether a newer release exists.
// It sends nothing about the user: an anonymous request with a generic client name.
func (a *App) hUpdate(r *http.Request) (any, error) {
	a.mu.Lock()
	off := a.settings.NoUpdates
	a.mu.Unlock()
	if off || version == "dev" {
		return UpdateInfo{}, nil
	}
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	if time.Since(a.upd.at) < 6*time.Hour && !a.upd.at.IsZero() {
		return a.upd.info, nil
	}
	info := UpdateInfo{}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, updateURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CloudMirror-update-check")
	if resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req); err == nil {
		defer resp.Body.Close()
		var rel struct {
			Tag string `json:"tag_name"`
			URL string `json:"html_url"`
		}
		if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&rel) == nil && rel.Tag != "" {
			info.Latest = strings.TrimPrefix(rel.Tag, "v")
			info.Newer = cmpVersion(info.Latest, version) > 0
			info.URL = releasesPage
			if strings.HasPrefix(strings.ToLower(rel.URL), "https://github.com/jackpison/cloud-mirror/") {
				info.URL = rel.URL
			}
		}
	}
	a.upd.at, a.upd.info = time.Now(), info
	if info.Latest == "" { // the lookup failed: try again in 30 minutes, not 6 hours
		a.upd.at = time.Now().Add(-5*time.Hour - 30*time.Minute)
	}
	return info, nil
}
