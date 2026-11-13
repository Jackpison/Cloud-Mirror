package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanFolder(t *testing.T) {
	cases := map[string]string{"a/b": "a/b", "/a/b/": "a/b", `a\b`: "a/b", "": ""}
	for in, want := range cases {
		got, err := cleanFolder(in)
		if err != nil || got != want {
			t.Errorf("cleanFolder(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := cleanFolder("a/../b"); err == nil {
		t.Error("expected an error for ..")
	}
}

func plan(src string, ex []string, targets ...Target) Plan {
	return Plan{Source: src, Targets: targets, Exclude: ex}
}

func TestNormalizePlans(t *testing.T) {
	ok, err := normalizePlans("main", []Plan{
		plan("Videos/2025", []string{" *.tmp ", "", "*.tmp", "# note", "Thumbs.db"}, Target{"pcloud", "Backup/2025"}, Target{"main", "ignored"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ok[0].Targets) != 1 || ok[0].Targets[0].Remote != "pcloud" {
		t.Errorf("the main account must not be a target: %+v", ok[0].Targets)
	}
	if got := strings.Join(ok[0].Exclude, ","); got != "*.tmp,Thumbs.db" {
		t.Errorf("exclude = %q", got)
	}
	if !idRe.MatchString(ok[0].ID) {
		t.Errorf("bad id %q", ok[0].ID)
	}

	bad := map[string][]Plan{
		"overlap":        {plan("A", nil, Target{"p", "Media"}), plan("B", nil, Target{"p", "media/Music"})},
		"same folder":    {plan("A", nil, Target{"p", "X"}), plan("B", nil, Target{"p", "x"})},
		"trash source":   {plan("_SyncTrash/x", nil, Target{"p", "X"})},
		"trash dest":     {plan("A", nil, Target{"p", "_SyncTrash"})},
		"no destination": {plan("A", nil)},
		"empty dest":     {plan("A", nil, Target{"p", ""})},
		"empty source":   {plan("", nil, Target{"p", "X"})},
		"bad glob":       {plan("A", []string{"[abc"}, Target{"p", "X"})},
	}
	for name, in := range bad {
		if _, err := normalizePlans("main", in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// The same folder name on different accounts is fine.
	if _, err := normalizePlans("main", []Plan{plan("A", nil, Target{"p", "X"}, Target{"k", "X"})}); err != nil {
		t.Errorf("different accounts must not overlap: %v", err)
	}
}

func TestMigrateOldSettings(t *testing.T) {
	dir := t.TempDir()
	old := `{"main":"gdrive","folders":{"Videos":["pcloud","filen"],"Music":["pcloud"]},"deleteMode":"trash"}`
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600)
	a := newApp(dir)
	if len(a.settings.Plans) != 2 || a.settings.GDocs != "skip" {
		t.Fatalf("migration failed: %+v", a.settings)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.Contains(string(b), `"folders"`) {
		t.Error("the legacy field should be gone from the saved file")
	}
}

func TestCmpVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"0.1.2", "0.1.1", 1}, {"0.1.1", "0.1.1", 0}, {"0.1.9", "0.1.10", -1}, {"v0.2.0", "0.1.9", 1}, {"1.0", "0.9.9", 1}}
	for _, c := range cases {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpVersion(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestOldSnapshots(t *testing.T) {
	now := time.Now()
	f := func(d time.Duration) string { return now.Add(-d).Format(stampLayout) }
	names := []string{f(40 * 24 * time.Hour), f(10 * 24 * time.Hour), f(time.Hour), "notes", "2026-13-99_000000x", "keepme"}
	got := oldSnapshots(names, now.Add(-30*24*time.Hour))
	if len(got) != 1 || got[0] != f(40*24*time.Hour) {
		t.Errorf("got %v", got)
	}
	if n := len(oldSnapshots(names, now.Add(time.Minute))); n != 3 {
		t.Errorf("everything cutoff should select the 3 real snapshots, got %d", n)
	}
}

func TestHistoryIsCapped(t *testing.T) {
	a := newApp(t.TempDir())
	many := make([]string, 500)
	for i := range many {
		many[i] = fmt.Sprint("f", i)
	}
	for i := 0; i < historyMax+20; i++ {
		a.mu.Lock()
		a.recordHistoryLocked(&Job{Kind: "sync", Pairs: []*Pair{{Folder: "A", Added: many, Errors: many}}})
		a.mu.Unlock()
	}
	if len(a.history) != historyMax {
		t.Errorf("history has %d entries", len(a.history))
	}
	p := a.history[0].Pairs[0]
	if len(p.Added) != historyListCap || len(p.Errors) != historyErrorCap {
		t.Errorf("lists not capped: %d %d", len(p.Added), len(p.Errors))
	}
	b := newApp(a.dir) // survives a restart
	if len(b.history) != historyMax {
		t.Errorf("history not reloaded: %d", len(b.history))
	}
}

func TestHostGuard(t *testing.T) {
	srv := httptest.NewServer(newApp(t.TempDir()).routes())
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/state", nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 403 {
		t.Errorf("a foreign Host header must be refused, got %v %v", resp, err)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/cancel", bytes.NewReader([]byte("{}")))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Errorf("a POST without the CloudMirror header must be refused, got %d", resp.StatusCode)
	}
}

func TestUpdateCheck(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "CloudMirror-update-check" {
			t.Errorf("unexpected user agent %q", r.Header.Get("User-Agent"))
		}
		fmt.Fprint(w, `{"tag_name":"v0.9.0","html_url":"https://evil.example.com/x"}`)
	}))
	defer fake.Close()
	oldURL, oldVer := updateURL, version
	updateURL, version = fake.URL, "0.1.0"
	defer func() { updateURL, version = oldURL, oldVer }()

	a := newApp(t.TempDir())
	v, _ := a.hUpdate(httptest.NewRequest("GET", "/api/update", nil))
	u := v.(UpdateInfo)
	if !u.Newer || u.Latest != "0.9.0" {
		t.Errorf("update not detected: %+v", u)
	}
	if u.URL != releasesPage {
		t.Errorf("a link outside the project must be replaced, got %q", u.URL)
	}
	a2 := newApp(t.TempDir())
	a2.settings.NoUpdates = true
	if v, _ := a2.hUpdate(httptest.NewRequest("GET", "/api/update", nil)); v.(UpdateInfo).Newer {
		t.Error("the check must be off when switched off")
	}
}

// ---- end to end against real rclone and local folders ----

type e2e struct {
	t   *testing.T
	srv *httptest.Server
	app *App
}

func (e *e2e) post(path string, in any) map[string]any {
	e.t.Helper()
	b, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/"+path, bytes.NewReader(b))
	req.Header.Set("X-CloudMirror", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 {
		e.t.Fatalf("POST %s: %d %v", path, resp.StatusCode, out["error"])
	}
	return out
}

func (e *e2e) postList(path string, in any) []map[string]any {
	e.t.Helper()
	b, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/"+path, bytes.NewReader(b))
	req.Header.Set("X-CloudMirror", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func (e *e2e) run(kind string) {
	e.t.Helper()
	e.post("run", map[string]any{"kind": kind})
	for i := 0; i < 300; i++ {
		time.Sleep(100 * time.Millisecond)
		e.app.mu.Lock()
		running := e.app.job.Running
		e.app.mu.Unlock()
		if !running {
			return
		}
	}
	e.t.Fatal("run did not finish")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestSyncEndToEnd(t *testing.T) {
	rc, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone is not installed")
	}
	root := t.TempDir()
	t.Setenv("RCLONE_CONFIG", filepath.Join(root, "rclone.conf"))
	for _, n := range []string{"main", "pcloud", "koofr"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
		if out, err := exec.Command(rc, "config", "create", n, "alias", "remote="+filepath.Join(root, n)).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	src := filepath.Join(root, "main", "Gallery", "Trips")
	write(t, filepath.Join(src, "a.jpg"), "aaa")
	write(t, filepath.Join(src, "Paris", "b.jpg"), "bbb")
	write(t, filepath.Join(src, "scratch.tmp"), "junk")
	write(t, filepath.Join(src, "Thumbs.db"), "junk")

	a := newApp(filepath.Join(root, "cfg"))
	srv := httptest.NewServer(a.routes())
	defer srv.Close()
	e := &e2e{t, srv, a}

	e.post("settings", map[string]any{"main": "main", "deleteMode": "trash", "gdocs": "skip", "plans": []any{
		map[string]any{"source": "Gallery/Trips", "exclude": []string{"*.tmp", "Thumbs.db"},
			"targets": []any{map[string]any{"remote": "pcloud", "path": "Backups/Holiday"}, map[string]any{"remote": "koofr", "path": "Photos"}}},
	}})

	// A check changes nothing.
	e.run("preview")
	if exists(filepath.Join(root, "pcloud", "Backups")) {
		t.Fatal("a check must not create anything")
	}
	if p := a.job.Pairs[0]; p.NAdded != 2 {
		t.Errorf("preview should list 2 files to add (excludes honoured), got %d: %v", p.NAdded, p.Added)
	}

	// A sync copies everything except the excluded files, into the chosen folders.
	e.run("sync")
	for _, d := range []string{"pcloud/Backups/Holiday", "koofr/Photos"} {
		if !exists(filepath.Join(root, d, "Paris", "b.jpg")) || !exists(filepath.Join(root, d, "a.jpg")) {
			t.Errorf("%s: files missing", d)
		}
		if exists(filepath.Join(root, d, "scratch.tmp")) || exists(filepath.Join(root, d, "Thumbs.db")) {
			t.Errorf("%s: excluded files were copied", d)
		}
	}

	// Remove one file and change another: the old versions go to the trash.
	os.Remove(filepath.Join(src, "Paris", "b.jpg"))
	write(t, filepath.Join(src, "a.jpg"), "changed")
	e.run("sync")
	dest := filepath.Join(root, "pcloud", "Backups", "Holiday")
	if exists(filepath.Join(dest, "Paris", "b.jpg")) {
		t.Error("the removed file should be gone from the destination")
	}
	matches, _ := filepath.Glob(filepath.Join(root, "pcloud", trashDir, "*", "Backups", "Holiday", "*"))
	deep, _ := filepath.Glob(filepath.Join(root, "pcloud", trashDir, "*", "Backups", "Holiday", "*", "*"))
	if len(matches)+len(deep) < 2 {
		t.Errorf("expected the old versions in the trash, found %v %v", matches, deep)
	}

	// History recorded the runs.
	if h := e.postList("history", nil); len(h) != 3 {
		t.Errorf("history should hold 3 runs, has %d", len(h))
	}

	// Trash cleanup: nothing is 30 days old yet; "everything" removes it.
	scan := e.postList("trash/scan", map[string]any{"days": 30})
	for _, r := range scan {
		if r["old"].(float64) != 0 {
			t.Errorf("nothing should be older than 30 days: %v", r)
		}
	}
	for _, r := range e.postList("trash/scan", map[string]any{"days": 0}) {
		if r["remote"] == "pcloud" && (r["old"].(float64) != 1 || r["files"].(float64) < 2) {
			t.Errorf("pcloud should have 1 snapshot with files: %v", r)
		}
	}
	e.postList("trash/clean", map[string]any{"days": 0})
	if left, _ := filepath.Glob(filepath.Join(root, "pcloud", trashDir, "*")); len(left) != 0 {
		t.Errorf("trash not emptied: %v", left)
	}
	if !exists(filepath.Join(dest, "a.jpg")) {
		t.Error("cleaning the trash must not touch the copies")
	}
}

func TestShouldExit(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		since   time.Time
		pages   int
		running bool
		want    bool
	}{
		{"a page is open", now.Add(-time.Hour), 1, false, false},
		{"page just closed", now.Add(-2 * time.Second), 0, false, false},
		{"nobody came back", now.Add(-exitGrace - time.Second), 0, false, true},
		{"a sync is running, page gone", now.Add(-time.Hour), 0, true, false},
		{"still inside the start-up allowance", now.Add(startGrace), 0, false, false},
	}
	for _, c := range cases {
		if got := shouldExit(now, c.since, c.pages, c.running); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLiveConnectionIsCounted(t *testing.T) {
	a := newApp(t.TempDir())
	srv := httptest.NewServer(a.routes())
	defer srv.Close()
	pages := func() int { a.mu.Lock(); defer a.mu.Unlock(); return a.pages }
	wait := func(want int) {
		for i := 0; i < 100; i++ {
			if pages() == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("pages = %d, want %d", pages(), want)
	}
	ctx1, close1 := context.WithCancel(context.Background())
	ctx2, close2 := context.WithCancel(context.Background())
	for _, ctx := range []context.Context{ctx1, ctx2} {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/live", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
	}
	wait(2)
	close1() // one tab closes: the app is still needed
	wait(1)
	close2()
	wait(0)
	a.mu.Lock()
	since := a.noPageSince
	a.mu.Unlock()
	if time.Since(since) > 5*time.Second {
		t.Error("the moment the last page left should be recorded")
	}
}
