package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Repository is a Git source managed by Aušra. URLs deliberately exclude embedded credentials.
type Repository struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	Project               string    `json:"project"`
	URL                   string    `json:"url"`
	Ref                   string    `json:"ref"`
	Revision              string    `json:"revision"`
	SyncedAt              time.Time `json:"syncedAt,omitempty"`
	LastError             string    `json:"lastError,omitempty"`
	WebhookEnabled        bool      `json:"webhookEnabled,omitempty"`
	WebhookLastDeliveryAt time.Time `json:"webhookLastDeliveryAt,omitempty"`
	WebhookLastError      string    `json:"webhookLastError,omitempty"`
	WebhookTargetType     string    `json:"webhookTargetType,omitempty"`
	WebhookTargetID       string    `json:"webhookTargetId,omitempty"`
	WebhookLastStatus     string    `json:"webhookLastStatus,omitempty"`
	WebhookLastRunID      string    `json:"webhookLastRunId,omitempty"`
}

var commitID = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

func validRepositoryURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && u.Path != "" && u.Path != "/"
}

func validRepositoryRef(ref string) bool {
	return len(ref) <= 200 && !strings.HasPrefix(ref, "-") && !strings.Contains(ref, "..") && !strings.Contains(ref, "@{") && !strings.ContainsAny(ref, " \t\n\r\\^~:?*[")
}

func validRelativeDir(dir string) bool {
	if dir == "" {
		return true
	}
	if filepath.IsAbs(dir) {
		return false
	}
	clean := filepath.Clean(dir)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(os.PathSeparator))
}

func (a *App) repositoryLocked(repoID string) *Repository {
	for i := range a.state.Repositories {
		if a.state.Repositories[i].ID == repoID {
			return &a.state.Repositories[i]
		}
	}
	return nil
}

func (a *App) repositories(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		out := append([]Repository{}, a.state.Repositories...)
		a.mu.Unlock()
		respond(w, 200, out)
		return
	}
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	var repo Repository
	if decode(r, &repo) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	repo.Name = strings.TrimSpace(repo.Name)
	repo.Project = strings.TrimSpace(repo.Project)
	repo.URL = strings.TrimSpace(repo.URL)
	repo.Ref = strings.TrimSpace(repo.Ref)
	if !validName(repo.Name) || !validRepositoryURL(repo.URL) || !validRepositoryRef(repo.Ref) {
		bad(w, 400, "provide a name, public HTTPS Git URL without credentials, and valid branch, tag, or commit")
		return
	}
	if repo.Project == "" {
		repo.Project = "Default"
	}
	repo.ID = id()
	repo.Revision = ""
	repo.SyncedAt = time.Time{}
	repo.LastError = ""
	repo.WebhookEnabled = false
	repo.WebhookLastDeliveryAt = time.Time{}
	repo.WebhookLastError = ""
	repo.WebhookTargetType, repo.WebhookTargetID = "", ""
	repo.WebhookLastStatus, repo.WebhookLastRunID = "", ""
	a.mu.Lock()
	a.state.Repositories = append(a.state.Repositories, repo)
	a.auditLocked("repository.created", repo.Name)
	a.persistLocked()
	a.mu.Unlock()
	result, err := a.syncRepository(repo.ID)
	if err != nil {
		// Keep the connection in the catalog so a failed network check can be retried.
		respond(w, 201, result)
		return
	}
	respond(w, 201, result)
}

func (a *App) repositoryAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/repositories/"), "/")
	if len(parts) == 2 && parts[1] == "webhook" {
		a.repositoryWebhookSettings(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "trigger" {
		a.repositoryWebhookTrigger(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "trigger" && parts[2] == "test" {
		a.repositoryWebhookTest(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "deliveries" {
		a.repositoryWebhookDeliveries(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "sync" && r.Method == http.MethodPost {
		repo, err := a.syncRepository(parts[0])
		if errors.Is(err, os.ErrNotExist) {
			bad(w, 404, "repository not found")
		} else if err != nil {
			bad(w, 502, repo.LastError)
		} else {
			respond(w, 200, repo)
		}
		return
	}
	if len(parts) != 1 || r.Method != http.MethodDelete {
		bad(w, 405, "method not allowed")
		return
	}
	a.repoMu.Lock()
	defer a.repoMu.Unlock()
	a.mu.Lock()
	repo := a.repositoryLocked(parts[0])
	if repo == nil {
		a.mu.Unlock()
		bad(w, 404, "repository not found")
		return
	}
	for _, job := range a.state.Jobs {
		if job.RepositoryID == repo.ID {
			a.mu.Unlock()
			bad(w, 409, "repository is used by a job")
			return
		}
	}
	for _, run := range a.state.Runs {
		if run.Status != "queued" && run.Status != "running" && run.Status != "waiting_approval" {
			continue
		}
		for _, source := range run.Sources {
			if source.RepositoryID == repo.ID {
				a.mu.Unlock()
				bad(w, 409, "repository is used by an active run")
				return
			}
		}
	}
	name := repo.Name
	a.mu.Unlock()
	if err := os.RemoveAll(a.repositoryPath(parts[0])); err != nil {
		bad(w, 500, "repository files could not be removed")
		return
	}
	if err := os.Remove(a.webhookSecretPath(parts[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
		bad(w, 500, "webhook secret could not be removed")
		return
	}
	a.mu.Lock()
	for i, item := range a.state.Repositories {
		if item.ID == parts[0] {
			a.state.Repositories = append(a.state.Repositories[:i], a.state.Repositories[i+1:]...)
			break
		}
	}
	a.auditLocked("repository.deleted", name)
	a.persistLocked()
	a.mu.Unlock()
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) repositoryPath(repoID string) string {
	return filepath.Join(filepath.Dir(a.dataFile), "repositories", repoID)
}

func gitCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(jobEnv(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "GIT_CONFIG_NOSYSTEM=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 500 {
			message = message[:500]
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git: %s", message)
	}
	return strings.TrimSpace(string(output)), nil
}

func resolveRepositoryCommit(ctx context.Context, root, ref string) (string, error) {
	candidates := []string{"refs/remotes/origin/HEAD"}
	if ref != "" {
		candidates = []string{"refs/remotes/origin/" + ref, "refs/tags/" + ref}
		if commitID.MatchString(ref) {
			candidates = append(candidates, ref)
		}
	}
	for _, candidate := range candidates {
		commit, err := gitCommand(ctx, "-C", root, "rev-parse", "--verify", "--end-of-options", candidate+"^{commit}")
		if err == nil && commitID.MatchString(commit) {
			return strings.ToLower(commit), nil
		}
	}
	return "", errors.New("branch, tag, or commit was not found in the repository")
}

func (a *App) syncRepository(repoID string) (Repository, error) {
	a.repoMu.Lock()
	defer a.repoMu.Unlock()
	a.mu.Lock()
	current := a.repositoryLocked(repoID)
	if current == nil {
		a.mu.Unlock()
		return Repository{}, os.ErrNotExist
	}
	repo := *current
	a.mu.Unlock()
	root := a.repositoryPath(repo.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Dir(root), 0700); err == nil {
			_, err = gitCommand(ctx, "-c", "credential.helper=", "clone", "--no-checkout", "--", repo.URL, root)
			if err != nil {
				_ = os.RemoveAll(root)
			}
		}
	} else if statErr != nil {
		err = statErr
	} else {
		var origin string
		origin, err = gitCommand(ctx, "-C", root, "config", "--get", "remote.origin.url")
		if err == nil && origin != repo.URL {
			err = errors.New("stored repository origin differs from the configured URL")
		}
		if err == nil {
			_, err = gitCommand(ctx, "-C", root, "-c", "credential.helper=", "fetch", "--prune", "origin")
		}
	}
	if err == nil {
		repo.Revision, err = resolveRepositoryCommit(ctx, root, repo.Ref)
	}
	a.mu.Lock()
	if current = a.repositoryLocked(repoID); current != nil {
		if err == nil {
			current.Revision = repo.Revision
			current.SyncedAt = time.Now().UTC()
			current.LastError = ""
			a.auditLocked("repository.synced", current.Name+" @ "+repo.Revision)
		} else {
			current.LastError = err.Error()
			a.auditLocked("repository.sync_failed", current.Name)
		}
		repo = *current
		a.persistLocked()
	}
	a.mu.Unlock()
	return repo, err
}

func (a *App) sourceSnapshotLocked(kind, targetID string) []RunSource {
	addJob := func(jobID string, result *[]RunSource) {
		for _, job := range a.state.Jobs {
			if job.ID == jobID && job.RepositoryID != "" {
				commit := ""
				if repo := a.repositoryLocked(job.RepositoryID); repo != nil {
					commit = repo.Revision
				}
				*result = append(*result, RunSource{JobID: job.ID, RepositoryID: job.RepositoryID, Commit: commit})
				return
			}
		}
	}
	result := []RunSource{}
	if kind == "job" {
		addJob(targetID, &result)
	} else if kind == "workflow" {
		for _, workflow := range a.state.Workflows {
			if workflow.ID == targetID {
				for _, jobID := range workflow.JobIDs {
					addJob(jobID, &result)
				}
				break
			}
		}
	}
	return result
}

func (a *App) jobDirectory(runID string, job Job) (string, func(), error) {
	noop := func() {}
	if job.RepositoryID == "" {
		dir, err := a.safeDir(job.Dir)
		return dir, noop, err
	}
	a.mu.Lock()
	var commit string
	for _, run := range a.state.Runs {
		if run.ID == runID {
			for _, source := range run.Sources {
				if source.JobID == job.ID && source.RepositoryID == job.RepositoryID {
					commit = source.Commit
					break
				}
			}
			break
		}
	}
	a.mu.Unlock()
	if !commitID.MatchString(commit) {
		return "", noop, errors.New("repository is not synced; sync it before running this job")
	}
	root := a.repositoryPath(job.RepositoryID)
	checkout := filepath.Join(filepath.Dir(a.dataFile), "checkouts", runID, id())
	if err := os.MkdirAll(filepath.Dir(checkout), 0700); err != nil {
		return "", noop, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a.repoMu.Lock()
	_, err := gitCommand(ctx, "-C", root, "worktree", "add", "--detach", checkout, commit)
	a.repoMu.Unlock()
	if err != nil {
		return "", noop, err
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		a.repoMu.Lock()
		_, err := gitCommand(ctx, "-C", root, "worktree", "remove", "--force", checkout)
		a.repoMu.Unlock()
		if err != nil {
			log.Printf("remove repository worktree: %v", err)
		}
	}
	dir, err := safeDirInRoot(checkout, job.Dir)
	if err != nil {
		cleanup()
		return "", noop, err
	}
	a.updateRun(runID, func(run *Run) { run.Log += "Source revision: " + commit + "\n" })
	return dir, cleanup, nil
}
