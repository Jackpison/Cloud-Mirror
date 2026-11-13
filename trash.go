package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Each sync that moves files out of the way puts them in _SyncTrash/<stamp>/ on the destination.
const stampLayout = "2006-01-02_150405"

var stampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{6}$`)

// TrashRemote reports what was found (and, when cleaning, removed) on one account.
type TrashRemote struct {
	Remote    string `json:"remote"`
	Snapshots int    `json:"snapshots"` // all trash snapshots on the account
	Old       int    `json:"old"`       // those older than the chosen age
	Files     int64  `json:"files"`     // files inside the old ones
	Bytes     int64  `json:"bytes"`
	Cleaned   int    `json:"cleaned"` // old snapshots actually deleted
	Error     string `json:"error,omitempty"`
}

// oldSnapshots picks the snapshot folder names older than cutoff. Names that are not
// exactly a timestamp are never touched.
func oldSnapshots(names []string, cutoff time.Time) []string {
	var out []string
	for _, n := range names {
		if !stampRe.MatchString(n) {
			continue
		}
		t, err := time.ParseInLocation(stampLayout, n, time.Local)
		if err == nil && t.Before(cutoff) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) trashOn(ctx context.Context, remote string, cutoff time.Time, clean bool) TrashRemote {
	res := TrashRemote{Remote: remote}
	out, err := a.rclone(ctx, "lsjson", remote+":"+trashDir, "--dirs-only")
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			res.Error = err.Error()
		}
		return res
	}
	var items []struct{ Name string }
	json.Unmarshal(out, &items)
	var names []string
	for _, it := range items {
		if stampRe.MatchString(it.Name) {
			names = append(names, it.Name)
		}
	}
	res.Snapshots = len(names)
	old := oldSnapshots(names, cutoff)
	res.Old = len(old)
	for _, n := range old {
		path := remote + ":" + trashDir + "/" + n
		if so, err := a.rclone(ctx, "size", path, "--json"); err == nil {
			var s sizeInfo
			json.Unmarshal(so, &s)
			res.Files += s.Count
			res.Bytes += s.Bytes
		}
		if clean {
			if _, err := a.rclone(ctx, "purge", path); err != nil {
				res.Error = err.Error()
				break
			}
			res.Cleaned++
		}
	}
	return res
}

// hTrash scans (or, with clean, empties) the trash older than the chosen number of days on every other account.
func (a *App) hTrash(clean bool) func(r *http.Request) (any, error) {
	return func(r *http.Request) (any, error) {
		var in struct{ Days int }
		if err := body(r, &in); err != nil || in.Days < 0 || in.Days > 3650 {
			return nil, errors.New("choose how old the trash must be")
		}
		a.mu.Lock()
		if clean && a.job != nil && a.job.Running {
			a.mu.Unlock()
			return nil, errors.New("wait for the current run to finish")
		}
		main := a.settings.Main
		a.mu.Unlock()
		cutoff := time.Now().Add(-time.Duration(in.Days) * 24 * time.Hour)
		if in.Days == 0 {
			cutoff = time.Now().Add(time.Minute) // everything
		}
		var targets []string
		for _, rm := range a.remotes(r.Context()) {
			if rm.Name != main {
				targets = append(targets, rm.Name)
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()
		results := make([]TrashRemote, len(targets))
		var wg sync.WaitGroup
		for i, t := range targets {
			wg.Add(1)
			go func(i int, t string) {
				defer wg.Done()
				results[i] = a.trashOn(ctx, t, cutoff, clean)
			}(i, t)
		}
		wg.Wait()
		return results, nil
	}
}
