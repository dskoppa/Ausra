package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

type Job struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Project      string   `json:"project"`
	RepositoryID string   `json:"repositoryId,omitempty"`
	Type         string   `json:"type"`
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	Dir          string   `json:"dir"`
	TimeoutSec   int      `json:"timeoutSec"`
}

type Workflow struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Project         string   `json:"project"`
	JobIDs          []string `json:"jobIds"`
	RequireApproval bool     `json:"requireApproval"`
}

type Schedule struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TargetType   string    `json:"targetType"`
	TargetID     string    `json:"targetId"`
	EveryMinutes int       `json:"everyMinutes"`
	Enabled      bool      `json:"enabled"`
	NextAt       time.Time `json:"nextAt"`
}

type Run struct {
	ID          string      `json:"id"`
	TargetType  string      `json:"targetType"`
	TargetID    string      `json:"targetId"`
	TargetName  string      `json:"targetName"`
	Status      string      `json:"status"`
	Step        int         `json:"step"`
	Log         string      `json:"log"`
	StartedAt   time.Time   `json:"startedAt"`
	FinishedAt  time.Time   `json:"finishedAt,omitempty"`
	TriggeredBy string      `json:"triggeredBy"`
	Sources     []RunSource `json:"sources,omitempty"`
}

type RunSource struct {
	JobID        string `json:"jobId"`
	RepositoryID string `json:"repositoryId"`
	Commit       string `json:"commit"`
}

type Audit struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

type State struct {
	Jobs              []Job             `json:"jobs"`
	Repositories      []Repository      `json:"repositories"`
	Workflows         []Workflow        `json:"workflows"`
	Schedules         []Schedule        `json:"schedules"`
	Runs              []Run             `json:"runs"`
	Audit             []Audit           `json:"audit"`
	WebhookDeliveries []WebhookDelivery `json:"webhookDeliveries,omitempty"`
}

type App struct {
	mu          sync.Mutex
	repoMu      sync.Mutex
	state       State
	dataFile    string
	workspace   string
	adminPass   string
	sessions    map[string]time.Time
	cancelRun   map[string]context.CancelFunc
	lastSaveErr error
}

func main() {
	password := os.Getenv("AUSRA_ADMIN_PASSWORD")
	if len(password) < 12 {
		log.Fatal("AUSRA_ADMIN_PASSWORD must be at least 12 characters")
	}
	workspace := env("AUSRA_WORKSPACE", ".")
	root, err := filepath.Abs(workspace)
	if err != nil {
		log.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		log.Fatal(err)
	}
	dataDir := env("AUSRA_DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatal(err)
	}
	a := &App{dataFile: filepath.Join(dataDir, "state.json"), workspace: root, adminPass: password, sessions: make(map[string]time.Time), cancelRun: make(map[string]context.CancelFunc)}
	if b, err := os.ReadFile(a.dataFile); err == nil {
		if err := json.Unmarshal(b, &a.state); err != nil {
			log.Fatal(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	for i := range a.state.Runs {
		if a.state.Runs[i].Status == "running" || a.state.Runs[i].Status == "queued" {
			a.state.Runs[i].Status = "interrupted"
			a.state.Runs[i].FinishedAt = time.Now().UTC()
		}
	}
	for i := range a.state.WebhookDeliveries {
		if a.state.WebhookDeliveries[i].Status == "processing" {
			a.state.WebhookDeliveries[i].Status = "failed"
		}
	}
	a.mu.Lock()
	if err := a.saveLocked(); err != nil {
		log.Fatal(err)
	}
	a.mu.Unlock()
	go a.scheduler()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/logout", a.auth(a.logout))
	mux.HandleFunc("/api/me", a.auth(a.me))
	mux.HandleFunc("/api/jobs", a.auth(a.jobs))
	mux.HandleFunc("/api/jobs/", a.auth(a.jobAction))
	mux.HandleFunc("/api/repositories", a.auth(a.repositories))
	mux.HandleFunc("/api/repositories/", a.auth(a.repositoryAction))
	mux.HandleFunc("/hooks/github/", a.githubWebhook)
	mux.HandleFunc("/api/workflows", a.auth(a.workflows))
	mux.HandleFunc("/api/workflows/", a.auth(a.workflowAction))
	mux.HandleFunc("/api/schedules", a.auth(a.schedules))
	mux.HandleFunc("/api/schedules/", a.auth(a.scheduleAction))
	mux.HandleFunc("/api/runs", a.auth(a.runs))
	mux.HandleFunc("/api/runs/", a.auth(a.runAction))
	mux.HandleFunc("/api/audit", a.auth(a.audit))
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))
	server := &http.Server{Addr: env("AUSRA_ADDR", "127.0.0.1:8080"), Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Aušra listening on %s; workspace %s", server.Addr, root)
	log.Fatal(server.ListenAndServe())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func id() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func bad(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]string{"error": message})
}
func decode(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(dst)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
func (a *App) saveLocked() error {
	b, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.dataFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, a.dataFile); err != nil {
		return err
	}
	return nil
}
func (a *App) persistLocked() {
	if err := a.saveLocked(); err != nil {
		a.lastSaveErr = err
		log.Printf("save state: %v", err)
	}
}
func (a *App) auditLocked(action, detail string) {
	a.state.Audit = append(a.state.Audit, Audit{time.Now().UTC(), action, detail})
	if len(a.state.Audit) > 2000 {
		a.state.Audit = a.state.Audit[len(a.state.Audit)-2000:]
	}
}
func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("ausra_session")
		if err != nil {
			bad(w, 401, "sign in required")
			return
		}
		a.mu.Lock()
		expiry, ok := a.sessions[c.Value]
		a.mu.Unlock()
		if !ok || time.Now().After(expiry) {
			bad(w, 401, "sign in required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			bad(w, 403, "invalid request origin")
			return
		}
		next(w, r)
	}
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	if !sameOrigin(r) {
		bad(w, 403, "invalid request origin")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if decode(r, &input) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	if subtle.ConstantTimeCompare([]byte(input.Password), []byte(a.adminPass)) != 1 {
		time.Sleep(200 * time.Millisecond)
		bad(w, 401, "invalid password")
		return
	}
	token := id() + id()
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(12 * time.Hour)
	a.auditLocked("login", "admin signed in")
	a.persistLocked()
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "ausra_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	respond(w, 200, map[string]string{"user": "admin"})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	if c, err := r.Cookie("ausra_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "ausra_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]string{"ok": "true"})
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]string{"user": "admin", "workspace": a.workspace})
}

func validName(s string) bool { return len(strings.TrimSpace(s)) >= 2 && len(s) <= 100 }
func (a *App) safeDir(dir string) (string, error) {
	return safeDirInRoot(a.workspace, dir)
}
func safeDirInRoot(root, dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	if filepath.IsAbs(dir) {
		return "", errors.New("directory must be relative to workspace")
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, dir))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("directory is outside workspace")
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("directory is not a folder")
	}
	return p, nil
}
func (a *App) jobs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		out := append([]Job{}, a.state.Jobs...)
		a.mu.Unlock()
		respond(w, 200, out)
		return
	}
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	var j Job
	if decode(r, &j) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	if !validName(j.Name) || (j.Type != "shell" && j.Type != "ansible" && j.Type != "terraform" && j.Type != "tofu") {
		bad(w, 400, "invalid name or type")
		return
	}
	if j.RepositoryID == "" {
		if _, err := a.safeDir(j.Dir); err != nil {
			bad(w, 400, err.Error())
			return
		}
	} else {
		if !validRelativeDir(j.Dir) {
			bad(w, 400, "directory must stay inside the repository")
			return
		}
		a.mu.Lock()
		repo := a.repositoryLocked(j.RepositoryID)
		ready := repo != nil && repo.Revision != ""
		a.mu.Unlock()
		if !ready {
			bad(w, 400, "repository not found or not synced")
			return
		}
	}
	if j.Type == "shell" && strings.TrimSpace(j.Command) == "" {
		bad(w, 400, "shell script is required")
		return
	}
	if j.Type != "shell" && len(j.Args) == 0 {
		bad(w, 400, "arguments are required")
		return
	}
	if j.TimeoutSec == 0 {
		j.TimeoutSec = 3600
	}
	if j.TimeoutSec < 1 || j.TimeoutSec > 86400 {
		bad(w, 400, "timeout must be 1–86400 seconds")
		return
	}
	j.ID = id()
	j.Project = strings.TrimSpace(j.Project)
	if j.Project == "" {
		j.Project = "Default"
	}
	a.mu.Lock()
	if j.RepositoryID != "" {
		repo := a.repositoryLocked(j.RepositoryID)
		if repo == nil || repo.Revision == "" {
			a.mu.Unlock()
			bad(w, 409, "repository changed before job creation")
			return
		}
	}
	a.state.Jobs = append(a.state.Jobs, j)
	a.auditLocked("job.created", j.Name)
	a.persistLocked()
	a.mu.Unlock()
	respond(w, 201, j)
}
func (a *App) jobAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	a.mu.Lock()
	var job Job
	found := false
	for _, j := range a.state.Jobs {
		if j.ID == parts[0] {
			job = j
			found = true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		bad(w, 404, "job not found")
		return
	}
	if len(parts) == 2 && parts[1] == "run" && r.Method == http.MethodPost {
		run := a.startRun("job", job.ID, job.Name, "admin")
		go a.executeJob(run.ID, job)
		respond(w, 202, run)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		a.mu.Lock()
		for _, repo := range a.state.Repositories {
			if repo.WebhookTargetType == "job" && repo.WebhookTargetID == job.ID {
				a.mu.Unlock()
				bad(w, 409, "job is used by a webhook trigger")
				return
			}
		}
		for _, wf := range a.state.Workflows {
			for _, jid := range wf.JobIDs {
				if jid == job.ID {
					a.mu.Unlock()
					bad(w, 409, "job is used by a workflow")
					return
				}
			}
		}
		for i, j := range a.state.Jobs {
			if j.ID == job.ID {
				a.state.Jobs = append(a.state.Jobs[:i], a.state.Jobs[i+1:]...)
				break
			}
		}
		a.auditLocked("job.deleted", job.Name)
		a.persistLocked()
		a.mu.Unlock()
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	bad(w, 405, "method not allowed")
}
func (a *App) workflows(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		out := append([]Workflow{}, a.state.Workflows...)
		a.mu.Unlock()
		respond(w, 200, out)
		return
	}
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	var wf Workflow
	if decode(r, &wf) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	if !validName(wf.Name) || len(wf.JobIDs) == 0 || len(wf.JobIDs) > 50 {
		bad(w, 400, "name and 1–50 steps required")
		return
	}
	a.mu.Lock()
	for _, jid := range wf.JobIDs {
		if !a.jobExistsLocked(jid) {
			a.mu.Unlock()
			bad(w, 400, "unknown job: "+jid)
			return
		}
	}
	wf.ID = id()
	wf.Project = strings.TrimSpace(wf.Project)
	if wf.Project == "" {
		wf.Project = "Default"
	}
	a.state.Workflows = append(a.state.Workflows, wf)
	a.auditLocked("workflow.created", wf.Name)
	a.persistLocked()
	a.mu.Unlock()
	respond(w, 201, wf)
}
func (a *App) jobExistsLocked(jid string) bool {
	for _, j := range a.state.Jobs {
		if j.ID == jid {
			return true
		}
	}
	return false
}
func (a *App) workflowAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/workflows/"), "/")
	a.mu.Lock()
	var wf Workflow
	found := false
	for _, x := range a.state.Workflows {
		if x.ID == parts[0] {
			wf = x
			found = true
			break
		}
	}
	a.mu.Unlock()
	if !found {
		bad(w, 404, "workflow not found")
		return
	}
	if len(parts) == 2 && parts[1] == "run" && r.Method == http.MethodPost {
		run := a.startRun("workflow", wf.ID, wf.Name, "admin")
		if wf.RequireApproval {
			a.updateRun(run.ID, func(x *Run) { x.Status = "waiting_approval"; x.Log = "Waiting for approval\n" })
			run.Status = "waiting_approval"
		} else {
			go a.executeWorkflow(run.ID, wf)
		}
		respond(w, 202, run)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		a.mu.Lock()
		for _, repo := range a.state.Repositories {
			if repo.WebhookTargetType == "workflow" && repo.WebhookTargetID == wf.ID {
				a.mu.Unlock()
				bad(w, 409, "workflow is used by a webhook trigger")
				return
			}
		}
		for i, x := range a.state.Workflows {
			if x.ID == wf.ID {
				a.state.Workflows = append(a.state.Workflows[:i], a.state.Workflows[i+1:]...)
				break
			}
		}
		a.auditLocked("workflow.deleted", wf.Name)
		a.persistLocked()
		a.mu.Unlock()
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	bad(w, 405, "method not allowed")
}
func (a *App) schedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		out := append([]Schedule{}, a.state.Schedules...)
		a.mu.Unlock()
		respond(w, 200, out)
		return
	}
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	var s Schedule
	if decode(r, &s) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	if !validName(s.Name) || s.EveryMinutes < 1 || s.EveryMinutes > 525600 {
		bad(w, 400, "invalid schedule name or interval")
		return
	}
	a.mu.Lock()
	valid := false
	if s.TargetType == "job" {
		valid = a.jobExistsLocked(s.TargetID)
	} else if s.TargetType == "workflow" {
		for _, wf := range a.state.Workflows {
			if wf.ID == s.TargetID {
				valid = true
			}
		}
	}
	if !valid {
		a.mu.Unlock()
		bad(w, 400, "target not found")
		return
	}
	s.ID = id()
	s.Enabled = true
	s.NextAt = time.Now().UTC().Add(time.Duration(s.EveryMinutes) * time.Minute)
	a.state.Schedules = append(a.state.Schedules, s)
	a.auditLocked("schedule.created", s.Name)
	a.persistLocked()
	a.mu.Unlock()
	respond(w, 201, s)
}
func (a *App) scheduleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		bad(w, 405, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, s := range a.state.Schedules {
		if s.ID == id {
			a.state.Schedules = append(a.state.Schedules[:i], a.state.Schedules[i+1:]...)
			a.auditLocked("schedule.deleted", s.Name)
			a.persistLocked()
			respond(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	bad(w, 404, "schedule not found")
}
func (a *App) runs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	out := append([]Run{}, a.state.Runs...)
	a.mu.Unlock()
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	respond(w, 200, out)
}
func (a *App) audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	out := append([]Audit{}, a.state.Audit...)
	a.mu.Unlock()
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	respond(w, 200, out)
}
func (a *App) runAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/runs/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	var run *Run
	for i := range a.state.Runs {
		if a.state.Runs[i].ID == parts[0] {
			run = &a.state.Runs[i]
			break
		}
	}
	if run == nil {
		a.mu.Unlock()
		bad(w, 404, "run not found")
		return
	}
	if parts[1] == "approve" && run.Status == "waiting_approval" {
		if run.TargetType == "job" {
			var job Job
			ok := false
			for _, x := range a.state.Jobs {
				if x.ID == run.TargetID {
					job = x
					ok = true
					break
				}
			}
			if !ok {
				a.mu.Unlock()
				bad(w, 409, "job no longer exists")
				return
			}
			run.Status = "queued"
			run.Log += "Approved by admin\n"
			runID := run.ID
			a.auditLocked("run.approved", runID)
			a.persistLocked()
			a.mu.Unlock()
			go a.executeJob(runID, job)
			respond(w, 200, map[string]bool{"ok": true})
			return
		}
		var wf Workflow
		ok := false
		for _, x := range a.state.Workflows {
			if x.ID == run.TargetID {
				wf = x
				ok = true
				break
			}
		}
		if !ok {
			a.mu.Unlock()
			bad(w, 409, "workflow no longer exists")
			return
		}
		run.Status = "queued"
		run.Log += "Approved by admin\n"
		runID := run.ID
		a.auditLocked("run.approved", runID)
		a.persistLocked()
		a.mu.Unlock()
		go a.executeWorkflow(runID, wf)
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	if parts[1] == "cancel" && (run.Status == "running" || run.Status == "queued" || run.Status == "waiting_approval") {
		cancel := a.cancelRun[run.ID]
		run.Status = "canceled"
		run.FinishedAt = time.Now().UTC()
		run.Log += "Canceled by admin\n"
		a.auditLocked("run.canceled", run.ID)
		a.persistLocked()
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	a.mu.Unlock()
	bad(w, 409, "run action unavailable")
}
func (a *App) recordAudit(action, detail string) {
	a.mu.Lock()
	a.auditLocked(action, detail)
	a.persistLocked()
	a.mu.Unlock()
}
func (a *App) startRun(kind, targetID, name, by string) Run {
	a.mu.Lock()
	r := a.startRunLocked(kind, targetID, name, by)
	a.persistLocked()
	a.mu.Unlock()
	return r
}
func (a *App) startRunLocked(kind, targetID, name, by string) Run {
	r := Run{ID: id(), TargetType: kind, TargetID: targetID, TargetName: name, Status: "queued", StartedAt: time.Now().UTC(), TriggeredBy: by}
	r.Sources = a.sourceSnapshotLocked(kind, targetID)
	a.state.Runs = append(a.state.Runs, r)
	a.auditLocked("run.started", kind+": "+name+" by "+by)
	return r
}
func (a *App) updateRun(id string, update func(*Run)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.state.Runs {
		if a.state.Runs[i].ID == id {
			update(&a.state.Runs[i])
			a.persistLocked()
			return
		}
	}
}
func (a *App) status(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.state.Runs {
		if r.ID == id {
			return r.Status
		}
	}
	return "missing"
}
func (a *App) executeWorkflow(runID string, wf Workflow) {
	for i, jid := range wf.JobIDs {
		if a.status(runID) == "canceled" {
			return
		}
		a.mu.Lock()
		var job Job
		found := false
		for _, j := range a.state.Jobs {
			if j.ID == jid {
				job = j
				found = true
				break
			}
		}
		a.mu.Unlock()
		if !found {
			a.finish(runID, "failed", "Job was deleted before execution\n")
			return
		}
		a.updateRun(runID, func(r *Run) {
			r.Step = i + 1
			r.Log += fmt.Sprintf("\nStep %d/%d: %s\n", i+1, len(wf.JobIDs), job.Name)
		})
		if !a.runCommand(runID, job) {
			return
		}
	}
	a.finish(runID, "succeeded", "Workflow complete\n")
}
func (a *App) executeJob(runID string, job Job) {
	if a.status(runID) == "canceled" {
		return
	}
	if a.runCommand(runID, job) {
		a.finish(runID, "succeeded", "Job complete\n")
	}
}
func (a *App) runCommand(runID string, job Job) bool {
	dir, cleanup, err := a.jobDirectory(runID, job)
	if err != nil {
		a.finish(runID, "failed", err.Error()+"\n")
		return false
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(job.TimeoutSec)*time.Second)
	a.mu.Lock()
	a.cancelRun[runID] = cancel
	a.mu.Unlock()
	defer func() { cancel(); a.mu.Lock(); delete(a.cancelRun, runID); a.mu.Unlock() }()
	if a.status(runID) == "canceled" {
		return false
	}
	var cmd *exec.Cmd
	switch job.Type {
	case "shell":
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", job.Command)
	case "ansible":
		cmd = exec.CommandContext(ctx, "ansible-playbook", job.Args...)
	case "terraform":
		cmd = exec.CommandContext(ctx, "terraform", job.Args...)
	case "tofu":
		cmd = exec.CommandContext(ctx, "tofu", job.Args...)
	default:
		a.finish(runID, "failed", "Unknown job type\n")
		return false
	}
	cmd.Dir = dir
	cmd.Env = jobEnv()
	a.updateRun(runID, func(r *Run) {
		if r.Status != "canceled" {
			r.Status = "running"
		}
	})
	output, err := cmd.CombinedOutput()
	logText := string(output)
	if len(logText) > 1<<20 {
		logText = logText[:1<<20] + "\n[output truncated]\n"
	}
	a.updateRun(runID, func(r *Run) {
		if r.Status != "canceled" {
			r.Status = "running"
		}
		r.Log += logText
		if len(r.Log) > 2<<20 {
			r.Log = r.Log[len(r.Log)-(2<<20):]
		}
	})
	if a.status(runID) == "canceled" {
		return false
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			a.finish(runID, "failed", "Timed out\n")
		} else {
			a.finish(runID, "failed", fmt.Sprintf("Command failed: %v\n", err))
		}
		return false
	}
	return true
}
func jobEnv() []string {
	var result []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "AUSRA_ADMIN_PASSWORD=") {
			result = append(result, entry)
		}
	}
	return result
}
func (a *App) finish(runID, status, message string) {
	a.updateRun(runID, func(r *Run) {
		if r.Status == "canceled" {
			return
		}
		r.Status = status
		r.FinishedAt = time.Now().UTC()
		r.Log += message
	})
	a.recordAudit("run."+status, runID)
}
func (a *App) scheduler() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		var due []Schedule
		a.mu.Lock()
		for i := range a.state.Schedules {
			s := &a.state.Schedules[i]
			if s.Enabled && !now.Before(s.NextAt) {
				due = append(due, *s)
				s.NextAt = now.UTC().Add(time.Duration(s.EveryMinutes) * time.Minute)
			}
		}
		if len(due) > 0 {
			a.persistLocked()
		}
		a.mu.Unlock()
		for _, s := range due {
			a.mu.Lock()
			var job Job
			var wf Workflow
			found := false
			if s.TargetType == "job" {
				for _, x := range a.state.Jobs {
					if x.ID == s.TargetID {
						job = x
						found = true
						break
					}
				}
			} else {
				for _, x := range a.state.Workflows {
					if x.ID == s.TargetID {
						wf = x
						found = true
						break
					}
				}
			}
			a.mu.Unlock()
			if !found {
				continue
			}
			if s.TargetType == "job" {
				run := a.startRun("job", job.ID, job.Name, "schedule: "+s.Name)
				go a.executeJob(run.ID, job)
			} else {
				run := a.startRun("workflow", wf.ID, wf.Name, "schedule: "+s.Name)
				if wf.RequireApproval {
					a.updateRun(run.ID, func(r *Run) { r.Status = "waiting_approval"; r.Log = "Waiting for approval\n" })
				} else {
					go a.executeWorkflow(run.ID, wf)
				}
			}
		}
	}
}
