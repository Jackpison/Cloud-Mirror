// CloudMirror: a small local web UI around rclone that mirrors chosen folders
// from one "main" cloud account to other cloud accounts at the press of a button.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

const addr = "127.0.0.1:47800"
const trashDir = "_SyncTrash"
const listCap = 300

type Settings struct {
	Main       string              `json:"main"`
	Folders    map[string][]string `json:"folders"` // folder path on main -> destination accounts
	DeleteMode string              `json:"deleteMode"`
}

type Pair struct {
	Folder   string   `json:"folder"`
	Dest     string   `json:"dest"`
	Status   string   `json:"status"` // pending, running, done, error, cancelled
	Added    []string `json:"added"`
	Updated  []string `json:"updated"`
	Removed  []string `json:"removed"`
	NAdded   int      `json:"nAdded"`
	NUpdated int      `json:"nUpdated"`
	NRemoved int      `json:"nRemoved"`
	Bytes    int64    `json:"bytes"`
	Total    int64    `json:"totalBytes"`
	Speed    float64  `json:"speed"`
	ETA      *float64 `json:"eta"`
	Current  []string `json:"current"`
	Errors   []string `json:"errors"`
	Trash    string   `json:"trash"`
}

type Job struct {
	Kind     string  `json:"kind"` // preview or sync
	Running  bool    `json:"running"`
	Started  string  `json:"started"`
	Finished string  `json:"finished"`
	Mode     string  `json:"mode"`
	Pairs    []*Pair `json:"pairs"`
	cancel   context.CancelFunc
}

type sizeInfo struct {
	Count   int64  `json:"count"`
	Bytes   int64  `json:"bytes"`
	Missing bool   `json:"missing"`
	Error   string `json:"error,omitempty"`
}

type App struct {
	dir string

	mu        sync.Mutex
	settings  Settings
	job       *Job
	sizes     map[string]sizeInfo
	providers json.RawMessage
	acctStop  context.CancelFunc

	sizeSem chan struct{}
}

func main() {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	a := &App{dir: filepath.Join(base, "CloudMirror"), sizes: map[string]sizeInfo{}, sizeSem: make(chan struct{}, 3)}
	os.MkdirAll(a.dir, 0o755)
	a.settings = Settings{Folders: map[string][]string{}, DeleteMode: "trash"}
	if b, err := os.ReadFile(filepath.Join(a.dir, "settings.json")); err == nil {
		json.Unmarshal(b, &a.settings)
		if a.settings.Folders == nil {
			a.settings.Folders = map[string][]string{}
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Already running: just bring the existing window up.
		openBrowser("http://" + addr)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML)
	})
	api := func(path string, h func(r *http.Request) (any, error)) {
		mux.HandleFunc("/api/"+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.Header.Get("X-CloudMirror") != "1" {
				w.WriteHeader(403)
				return
			}
			v, err := h(r)
			if err != nil {
				w.WriteHeader(400)
				v = map[string]string{"error": err.Error()}
			}
			json.NewEncoder(w).Encode(v)
		})
	}
	api("state", a.hState)
	api("install", a.hInstall)
	api("providers", a.hProviders)
	api("account/step", a.hAccountStep)
	api("account/cancel", a.hAccountCancel)
	api("account/delete", a.hAccountDelete)
	api("folders", a.hFolders)
	api("size", a.hSize)
	api("settings", a.hSettings)
	api("run", a.hRun)
	api("cancel", a.hCancel)
	api("quit", func(r *http.Request) (any, error) {
		a.stopAll()
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
		return map[string]bool{"ok": true}, nil
	})

	// Only answer requests addressed to this machine (blocks DNS-rebinding from web pages).
	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr && r.Host != "localhost:47800" {
			w.WriteHeader(403)
			return
		}
		mux.ServeHTTP(w, r)
	})
	if os.Getenv("CLOUDMIRROR_NO_BROWSER") == "" {
		go openBrowser("http://" + addr)
	}
	http.Serve(ln, guarded)
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	c.Start()
}

// ---------- rclone plumbing ----------

func exeName() string {
	if runtime.GOOS == "windows" {
		return "rclone.exe"
	}
	return "rclone"
}

func (a *App) rclonePath() string {
	cands := []string{filepath.Join(a.dir, exeName())}
	if self, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(self), exeName()))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("rclone"); err == nil {
		return p
	}
	return ""
}

func (a *App) rclone(ctx context.Context, args ...string) ([]byte, error) {
	p := a.rclonePath()
	if p == "" {
		return nil, errors.New("the sync engine (rclone) is not installed yet")
	}
	c := exec.CommandContext(ctx, p, args...)
	hideWindow(c)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		msg := lastLine(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), errors.New(msg)
	}
	return out.Bytes(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	// strip rclone's "2026/01/02 15:04:05 ERROR : " prefix
	if m := regexp.MustCompile(`^\d{4}/\d\d/\d\d \d\d:\d\d:\d\d \w+\s*: `).FindString(l); m != "" {
		l = l[len(m):]
	}
	return l
}

type remote struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (a *App) remotes(ctx context.Context) []remote {
	out, err := a.rclone(ctx, "config", "dump")
	if err != nil {
		return []remote{}
	}
	var m map[string]map[string]any
	json.Unmarshal(out, &m)
	rs := []remote{}
	for n, v := range m {
		t, _ := v["type"].(string)
		rs = append(rs, remote{n, t})
	}
	sort.Slice(rs, func(i, j int) bool { return strings.ToLower(rs[i].Name) < strings.ToLower(rs[j].Name) })
	return rs
}

func body(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,40}$`)

func cleanFolder(p string) (string, error) {
	p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", errors.New("invalid folder")
		}
	}
	return p, nil
}

// ---------- handlers ----------

func (a *App) hState(r *http.Request) (any, error) {
	ver := ""
	found := a.rclonePath() != ""
	var rs []remote = []remote{}
	if found {
		if out, err := a.rclone(r.Context(), "version"); err == nil {
			ver = strings.TrimPrefix(strings.SplitN(string(out), "\n", 2)[0], "rclone ")
		}
		rs = a.remotes(r.Context())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	jb, _ := json.Marshal(a.job)
	return map[string]any{
		"engine":   map[string]any{"found": found, "version": ver},
		"remotes":  rs,
		"settings": a.settings,
		"job":      json.RawMessage(jb),
	}, nil
}

func (a *App) hInstall(r *http.Request) (any, error) {
	osn := map[string]string{"windows": "windows", "darwin": "osx", "linux": "linux"}[runtime.GOOS]
	if osn == "" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, errors.New("automatic install is not available for this computer; install rclone from rclone.org")
	}
	url := fmt.Sprintf("https://downloads.rclone.org/rclone-current-%s-%s.zip", osn, runtime.GOARCH)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 300<<20))
	if err != nil {
		return nil, fmt.Errorf("download failed: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != exeName() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		tmp := filepath.Join(a.dir, exeName()+".tmp")
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return nil, err
		}
		out.Close()
		if err := os.Rename(tmp, filepath.Join(a.dir, exeName())); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	}
	return nil, errors.New("the download did not contain rclone")
}

func (a *App) hProviders(r *http.Request) (any, error) {
	a.mu.Lock()
	cached := a.providers
	a.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	out, err := a.rclone(r.Context(), "config", "providers")
	if err != nil {
		return nil, err
	}
	var ps []struct{ Name, Description string }
	json.Unmarshal(out, &ps)
	list := []map[string]string{}
	skip := map[string]bool{"alias": true, "union": true, "combine": true, "crypt": true, "chunker": true, "compress": true, "hasher": true, "cache": true, "memory": true, "local": true, "archive": true}
	for _, p := range ps {
		if !skip[p.Name] {
			list = append(list, map[string]string{"name": p.Name, "description": strings.SplitN(p.Description, "\n", 2)[0]})
		}
	}
	b, _ := json.Marshal(list)
	a.mu.Lock()
	a.providers = b
	a.mu.Unlock()
	return json.RawMessage(b), nil
}

// hAccountStep drives rclone's own question-and-answer account setup, one question at a time.
func (a *App) hAccountStep(r *http.Request) (any, error) {
	var in struct {
		Name, Type, State, Result string
		First, Password           bool
	}
	if err := body(r, &in); err != nil {
		return nil, err
	}
	if !nameRe.MatchString(in.Name) {
		return nil, errors.New("use only letters, numbers, - and _ in the account name")
	}
	if !nameRe.MatchString(in.Type) {
		return nil, errors.New("choose a service")
	}
	if in.First {
		for _, rm := range a.remotes(r.Context()) {
			if strings.EqualFold(rm.Name, in.Name) {
				return nil, errors.New("an account with that name already exists")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	a.mu.Lock()
	a.acctStop = cancel
	a.mu.Unlock()
	defer cancel()

	first, state, result, pw := in.First, in.State, in.Result, in.Password
	for {
		args := []string{"config", "create", in.Name, in.Type, "--non-interactive", "--all"}
		if !first {
			args = append(args, "--continue", "--state", state, "--result", result)
			if pw {
				args = append(args, "--obscure")
			}
		}
		out, err := a.rclone(ctx, args...)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errors.New("cancelled")
			}
			return nil, err
		}
		var q struct {
			State  string
			Error  string
			Option *struct {
				Name, Help, Type, DefaultStr    string
				IsPassword, Required, Exclusive bool
				Examples                        []struct{ Value, Help string }
			}
		}
		if err := json.Unmarshal(out, &q); err != nil {
			return nil, errors.New("unexpected answer from rclone")
		}
		if q.State == "" || q.Option == nil {
			return map[string]any{"done": true}, nil
		}
		first, state, pw = false, q.State, false
		// Questions a desktop user never needs to see.
		if q.Error == "" {
			switch q.Option.Name {
			case "config_fs_advanced":
				result = "false"
				continue
			case "config_is_local":
				result = "true"
				continue
			}
		}
		return map[string]any{"done": false, "state": q.State, "error": q.Error, "option": q.Option}, nil
	}
}

func (a *App) hAccountCancel(r *http.Request) (any, error) {
	var in struct{ Name string }
	body(r, &in)
	a.mu.Lock()
	if a.acctStop != nil {
		a.acctStop()
	}
	a.mu.Unlock()
	if nameRe.MatchString(in.Name) {
		time.Sleep(200 * time.Millisecond)
		a.rclone(context.Background(), "config", "delete", in.Name)
	}
	return map[string]bool{"ok": true}, nil
}

func (a *App) hAccountDelete(r *http.Request) (any, error) {
	var in struct{ Name string }
	if err := body(r, &in); err != nil || !nameRe.MatchString(in.Name) {
		return nil, errors.New("invalid account")
	}
	if _, err := a.rclone(r.Context(), "config", "delete", in.Name); err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.settings.Main == in.Name {
		a.settings.Main = ""
		a.settings.Folders = map[string][]string{}
	}
	for f, ds := range a.settings.Folders {
		keep := []string{}
		for _, d := range ds {
			if d != in.Name {
				keep = append(keep, d)
			}
		}
		a.settings.Folders[f] = keep
	}
	a.saveLocked()
	a.mu.Unlock()
	return map[string]bool{"ok": true}, nil
}

func (a *App) hFolders(r *http.Request) (any, error) {
	rm := r.URL.Query().Get("remote")
	p, err := cleanFolder(r.URL.Query().Get("path"))
	if err != nil || !nameRe.MatchString(rm) {
		return nil, errors.New("invalid folder")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := a.rclone(ctx, "lsjson", rm+":"+p, "--dirs-only")
	if err != nil {
		return nil, err
	}
	var items []struct{ Name string }
	json.Unmarshal(out, &items)
	names := []string{}
	for _, it := range items {
		if it.Name != trashDir {
			names = append(names, it.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names, nil
}

func (a *App) hSize(r *http.Request) (any, error) {
	rm := r.URL.Query().Get("remote")
	p, err := cleanFolder(r.URL.Query().Get("path"))
	if err != nil || !nameRe.MatchString(rm) || p == "" {
		return nil, errors.New("invalid folder")
	}
	key := rm + ":" + p
	if r.URL.Query().Get("refresh") == "" {
		a.mu.Lock()
		s, ok := a.sizes[key]
		a.mu.Unlock()
		if ok {
			return s, nil
		}
	}
	a.sizeSem <- struct{}{}
	defer func() { <-a.sizeSem }()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	var s sizeInfo
	out, err := a.rclone(ctx, "size", key, "--json")
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.Missing = true
		} else {
			return sizeInfo{Error: err.Error()}, nil // not cached, so it is retried next time
		}
	} else {
		json.Unmarshal(out, &s)
	}
	a.mu.Lock()
	a.sizes[key] = s
	a.mu.Unlock()
	return s, nil
}

func (a *App) saveLocked() {
	b, _ := json.MarshalIndent(a.settings, "", "  ")
	os.WriteFile(filepath.Join(a.dir, "settings.json"), b, 0o600)
}

func (a *App) hSettings(r *http.Request) (any, error) {
	var s Settings
	if err := body(r, &s); err != nil {
		return nil, err
	}
	if s.Main != "" && !nameRe.MatchString(s.Main) {
		return nil, errors.New("invalid main account")
	}
	if s.DeleteMode != "mirror" && s.DeleteMode != "keep" {
		s.DeleteMode = "trash"
	}
	clean := map[string][]string{}
	for f, ds := range s.Folders {
		f, err := cleanFolder(f)
		if err != nil || f == "" {
			continue
		}
		out := []string{}
		for _, d := range ds {
			if nameRe.MatchString(d) && d != s.Main {
				out = append(out, d)
			}
		}
		clean[f] = out
	}
	s.Folders = clean
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job != nil && a.job.Running {
		return nil, errors.New("wait for the current run to finish before changing settings")
	}
	a.settings = s
	a.saveLocked()
	return s, nil
}

func (a *App) stopAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job != nil && a.job.cancel != nil {
		a.job.cancel()
	}
	if a.acctStop != nil {
		a.acctStop()
	}
}

func (a *App) hCancel(r *http.Request) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job != nil && a.job.cancel != nil {
		a.job.cancel()
	}
	return map[string]bool{"ok": true}, nil
}

// ---------- preview / sync ----------

func (a *App) hRun(r *http.Request) (any, error) {
	var in struct{ Kind string }
	if err := body(r, &in); err != nil || (in.Kind != "preview" && in.Kind != "sync") {
		return nil, errors.New("invalid request")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job != nil && a.job.Running {
		return nil, errors.New("a run is already in progress")
	}
	if a.settings.Main == "" {
		return nil, errors.New("choose a main account first")
	}
	folders := make([]string, 0, len(a.settings.Folders))
	for f := range a.settings.Folders {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	job := &Job{Kind: in.Kind, Running: true, Started: time.Now().Format(time.RFC3339), Mode: a.settings.DeleteMode}
	for _, f := range folders {
		for _, d := range a.settings.Folders[f] {
			job.Pairs = append(job.Pairs, &Pair{Folder: f, Dest: d, Status: "pending",
				Added: []string{}, Updated: []string{}, Removed: []string{}, Current: []string{}, Errors: []string{}})
		}
	}
	if len(job.Pairs) == 0 {
		return nil, errors.New("tick at least one folder to copy first")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	a.job = job
	main, mode, stamp := a.settings.Main, a.settings.DeleteMode, time.Now().Format("2006-01-02_150405")

	// One worker per destination account: accounts run side by side, folders within an account in turn.
	byDest := map[string][]*Pair{}
	for _, p := range job.Pairs {
		byDest[p.Dest] = append(byDest[p.Dest], p)
	}
	var wg sync.WaitGroup
	for _, ps := range byDest {
		wg.Add(1)
		go func(ps []*Pair) {
			defer wg.Done()
			for _, p := range ps {
				if ctx.Err() != nil {
					a.mu.Lock()
					p.Status = "cancelled"
					a.mu.Unlock()
					continue
				}
				a.runPair(ctx, p, main, job.Kind, mode, stamp)
			}
		}(ps)
	}
	go func() {
		wg.Wait()
		cancel()
		a.mu.Lock()
		job.Running = false
		job.Finished = time.Now().Format(time.RFC3339)
		if job.Kind == "sync" {
			for _, p := range job.Pairs {
				delete(a.sizes, p.Dest+":"+p.Folder)
			}
		}
		a.mu.Unlock()
	}()
	return map[string]bool{"ok": true}, nil
}

func (a *App) runPair(ctx context.Context, p *Pair, main, kind, mode, stamp string) {
	set := func(f func()) { a.mu.Lock(); f(); a.mu.Unlock() }
	set(func() { p.Status = "running" })

	report, err := os.CreateTemp("", "cloudmirror-*.txt")
	if err != nil {
		set(func() { p.Status = "error"; p.Errors = append(p.Errors, err.Error()) })
		return
	}
	report.Close()
	defer os.Remove(report.Name())

	verb := "sync"
	if mode == "keep" {
		verb = "copy"
	}
	args := []string{verb, main + ":" + p.Folder, p.Dest + ":" + p.Folder,
		"--combined", report.Name(), "--create-empty-src-dirs",
		"--use-json-log", "-v", "--stats", "1s"}
	if mode == "trash" {
		trash := trashDir + "/" + stamp + "/" + p.Folder
		args = append(args, "--backup-dir", p.Dest+":"+trash)
		set(func() { p.Trash = trash })
	}
	if kind == "preview" {
		args = append(args, "--dry-run")
	}

	c := exec.CommandContext(ctx, a.rclonePath(), args...)
	hideWindow(c)
	stderr, _ := c.StderrPipe()
	if err := c.Start(); err != nil {
		set(func() { p.Status = "error"; p.Errors = append(p.Errors, err.Error()) })
		return
	}
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		var l struct {
			Level  string
			Msg    string
			Object string
			Stats  *struct {
				Bytes        int64
				TotalBytes   int64
				Speed        float64
				Eta          *float64
				Transferring []struct{ Name string }
			}
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		set(func() {
			if l.Stats != nil {
				p.Bytes, p.Total, p.Speed, p.ETA = l.Stats.Bytes, l.Stats.TotalBytes, l.Stats.Speed, l.Stats.Eta
				p.Current = []string{}
				for _, t := range l.Stats.Transferring {
					p.Current = append(p.Current, t.Name)
				}
			} else if l.Level == "error" && len(p.Errors) < 50 {
				m := l.Msg
				if l.Object != "" {
					m = l.Object + ": " + m
				}
				p.Errors = append(p.Errors, m)
			}
		})
	}
	runErr := c.Wait()

	// The report lists every path with a marker: + new, * changed, - only on the copy.
	var added, updated, removed []string
	if f, err := os.Open(report.Name()); err == nil {
		rs := bufio.NewScanner(f)
		rs.Buffer(make([]byte, 64*1024), 8<<20)
		for rs.Scan() {
			line := rs.Text()
			if len(line) < 3 {
				continue
			}
			switch line[0] {
			case '+':
				added = append(added, line[2:])
			case '*':
				updated = append(updated, line[2:])
			case '-':
				if mode != "keep" {
					removed = append(removed, line[2:])
				}
			}
		}
		f.Close()
	}
	capList := func(s []string) []string {
		sort.Strings(s)
		if len(s) > listCap {
			s = s[:listCap]
		}
		if s == nil {
			s = []string{}
		}
		return s
	}
	set(func() {
		p.NAdded, p.NUpdated, p.NRemoved = len(added), len(updated), len(removed)
		p.Added, p.Updated, p.Removed = capList(added), capList(updated), capList(removed)
		p.Current = []string{}
		switch {
		case ctx.Err() != nil:
			p.Status = "cancelled"
		case runErr != nil:
			p.Status = "error"
			if len(p.Errors) == 0 {
				p.Errors = append(p.Errors, runErr.Error())
			}
		default:
			p.Status = "done"
		}
	})
}
