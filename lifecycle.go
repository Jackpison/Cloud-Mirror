package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// CloudMirror has no window of its own: its page runs in the browser. To avoid a program that keeps
// running invisibly after the page is closed, every open page holds a long-lived connection to /api/live.
// The connection drops when the tab or browser closes (or crashes), and the app exits shortly after the
// last one is gone.
var (
	exitGrace  = 20 * time.Second  // no page connected for this long: exit
	startGrace = 100 * time.Second // extra time at start-up for the browser to open the page
)

// shouldExit decides whether the app has been left without a page. It never exits during a run.
func shouldExit(now, noPageSince time.Time, pages int, running bool) bool {
	return !running && pages == 0 && now.Sub(noPageSince) > exitGrace
}

// hLive keeps a connection open for as long as a page is open.
func (a *App) hLive(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	a.mu.Lock()
	a.pages++
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.pages--
		if a.pages == 0 {
			a.noPageSince = time.Now()
		}
		a.mu.Unlock()
	}()
	fmt.Fprint(w, "retry: 2000\n: connected\n\n")
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// watchPages ends the program when no page is left.
func (a *App) watchPages() {
	for range time.Tick(5 * time.Second) {
		a.mu.Lock()
		running := a.job != nil && a.job.Running
		pages, since := a.pages, a.noPageSince
		a.mu.Unlock()
		if shouldExit(time.Now(), since, pages, running) {
			os.Exit(0)
		}
	}
}
