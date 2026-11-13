package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

const (
	historyMax      = 100 // runs kept
	historyListCap  = 100 // file names kept per list
	historyErrorCap = 5
)

// HistPair is what is remembered about one folder copy in a finished run.
type HistPair struct {
	Folder   string   `json:"folder"`
	Dest     string   `json:"dest"`
	DestPath string   `json:"destPath"`
	Status   string   `json:"status"`
	NAdded   int      `json:"nAdded"`
	NUpdated int      `json:"nUpdated"`
	NRemoved int      `json:"nRemoved"`
	Added    []string `json:"added"`
	Updated  []string `json:"updated"`
	Removed  []string `json:"removed"`
	Errors   []string `json:"errors"`
	Trash    string   `json:"trash"`
}

// HistEntry is one finished check or sync.
type HistEntry struct {
	Kind     string     `json:"kind"`
	Started  string     `json:"started"`
	Finished string     `json:"finished"`
	Mode     string     `json:"mode"`
	Pairs    []HistPair `json:"pairs"`
}

func (a *App) historyPath() string { return filepath.Join(a.dir, "history.json") }

func (a *App) loadHistory() {
	a.history = []HistEntry{}
	if b, err := os.ReadFile(a.historyPath()); err == nil {
		json.Unmarshal(b, &a.history)
	}
}

func capStrings(s []string, n int) []string {
	if len(s) > n {
		s = s[:n]
	}
	return append([]string{}, s...)
}

// recordHistoryLocked remembers a finished job. The caller holds a.mu.
func (a *App) recordHistoryLocked(j *Job) {
	e := HistEntry{Kind: j.Kind, Started: j.Started, Finished: j.Finished, Mode: j.Mode, Pairs: []HistPair{}}
	for _, p := range j.Pairs {
		e.Pairs = append(e.Pairs, HistPair{
			Folder: p.Folder, Dest: p.Dest, DestPath: p.DestPath, Status: p.Status,
			NAdded: p.NAdded, NUpdated: p.NUpdated, NRemoved: p.NRemoved,
			Added: capStrings(p.Added, historyListCap), Updated: capStrings(p.Updated, historyListCap), Removed: capStrings(p.Removed, historyListCap),
			Errors: capStrings(p.Errors, historyErrorCap), Trash: p.Trash,
		})
	}
	a.history = append([]HistEntry{e}, a.history...)
	if len(a.history) > historyMax {
		a.history = a.history[:historyMax]
	}
	a.saveHistoryLocked()
}

func (a *App) saveHistoryLocked() {
	if b, err := json.Marshal(a.history); err == nil {
		os.WriteFile(a.historyPath(), b, 0o600)
	}
}

func (a *App) hHistory(r *http.Request) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]HistEntry{}, a.history...), nil
}

func (a *App) hHistoryClear(r *http.Request) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = []HistEntry{}
	a.saveHistoryLocked()
	return map[string]bool{"ok": true}, nil
}
