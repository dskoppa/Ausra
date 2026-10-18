package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"golang.org/x/time/rate"
)

// Delivery IDs are scoped to a repository and retained across restarts.
type WebhookDelivery struct {
	RepositoryID string    `json:"repositoryId"`
	ID           string    `json:"id"`
	Event        string    `json:"event"`
	Ref          string    `json:"ref,omitempty"`
	After        string    `json:"after,omitempty"`
	TargetType   string    `json:"targetType,omitempty"`
	TargetID     string    `json:"targetId,omitempty"`
	RunID        string    `json:"runId,omitempty"`
	Error        string    `json:"error,omitempty"`
	Status       string    `json:"status"`
	ReceivedAt   time.Time `json:"receivedAt"`
}

func (a *App) webhookSecretPath(repoID string) string {
	return filepath.Join(filepath.Dir(a.dataFile), "webhook-secrets", repoID)
}

func writeWebhookSecret(path string, secret []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, secret, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func restoreWebhookSecret(path string, secret []byte, existed bool) error {
	if existed {
		return writeWebhookSecret(path, secret)
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (a *App) repositoryWebhookSettings(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	repo := a.repositoryLocked(repoID)
	if repo == nil {
		bad(w, 404, "repository not found")
		return
	}
	u, err := url.Parse(repo.URL)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		bad(w, 400, "GitHub webhooks require a github.com repository URL")
		return
	}
	path := a.webhookSecretPath(repoID)
	previousSecret, readErr := os.ReadFile(path)
	secretExisted := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		bad(w, 500, "could not read webhook secret")
		return
	}
	previousRepo, previousAudit := *repo, a.state.Audit
	if r.Method == http.MethodDelete {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			bad(w, 500, "could not remove webhook secret")
			return
		}
		repo.WebhookEnabled = false
		repo.WebhookLastError = ""
		repo.WebhookTargetType, repo.WebhookTargetID = "", ""
		a.auditLocked("repository.webhook_disabled", repo.Name)
		if err := a.saveLocked(); err != nil {
			*repo, a.state.Audit = previousRepo, previousAudit
			if restoreErr := restoreWebhookSecret(path, previousSecret, secretExisted); restoreErr != nil {
				log.Fatalf("restore webhook secret: %v", restoreErr)
			}
			bad(w, 503, "could not save webhook settings")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		bad(w, 500, "could not create webhook secret")
		return
	}
	secret := hex.EncodeToString(secretBytes)
	if err := writeWebhookSecret(path, []byte(secret)); err != nil {
		bad(w, 500, "could not save webhook secret")
		return
	}
	repo.WebhookEnabled = true
	repo.WebhookLastError = ""
	a.auditLocked("repository.webhook_enabled", repo.Name)
	if err := a.saveLocked(); err != nil {
		*repo, a.state.Audit = previousRepo, previousAudit
		if restoreErr := restoreWebhookSecret(path, previousSecret, secretExisted); restoreErr != nil {
			log.Fatalf("restore webhook secret: %v", restoreErr)
		}
		bad(w, 503, "could not save webhook settings")
		return
	}
	respond(w, 200, map[string]string{"secret": secret, "path": "/hooks/github/" + repoID})
}

func (a *App) repositoryWebhookTrigger(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		bad(w, 405, "method not allowed")
		return
	}
	var input struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
	}
	if r.Method == http.MethodPut && decode(r, &input) != nil {
		bad(w, 400, "invalid JSON")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	repo := a.repositoryLocked(repoID)
	if repo == nil {
		bad(w, 404, "repository not found")
		return
	}
	if r.Method == http.MethodPut {
		if !repo.WebhookEnabled {
			bad(w, 409, "enable the GitHub webhook first")
			return
		}
		if _, ok := a.webhookTargetLocked(repoID, input.TargetType, input.TargetID); !ok {
			bad(w, 400, "choose a job using this repository or a workflow with jobs from this repository")
			return
		}
	}
	previousRepo, previousAudit := *repo, a.state.Audit
	repo.WebhookTargetType, repo.WebhookTargetID = input.TargetType, input.TargetID
	if r.Method == http.MethodDelete {
		repo.WebhookTargetType, repo.WebhookTargetID = "", ""
	}
	a.auditLocked("repository.webhook_trigger_changed", repo.Name+" -> "+repo.WebhookTargetType+":"+repo.WebhookTargetID)
	if err := a.saveLocked(); err != nil {
		*repo, a.state.Audit = previousRepo, previousAudit
		bad(w, 503, "could not save webhook trigger")
		return
	}
	respond(w, 200, repo)
}

func (a *App) repositoryWebhookTest(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	repo := a.repositoryLocked(repoID)
	if repo == nil {
		bad(w, 404, "repository not found")
		return
	}
	if !repo.WebhookEnabled || repo.Revision == "" || repo.WebhookTargetType == "" {
		bad(w, 409, "enable the webhook, choose a trigger, and sync the repository first")
		return
	}
	name, ok := a.webhookTargetLocked(repoID, repo.WebhookTargetType, repo.WebhookTargetID)
	if !ok {
		bad(w, 409, "trigger target is unavailable")
		return
	}
	previousRuns, previousAudit := a.state.Runs, a.state.Audit
	run := a.queueWebhookApprovalLocked(repo.WebhookTargetType, repo.WebhookTargetID, name, "webhook test: "+repo.Name, "Test trigger @ "+repo.Revision)
	if err := a.saveLocked(); err != nil {
		a.state.Runs, a.state.Audit = previousRuns, previousAudit
		bad(w, 503, "could not save test run")
		return
	}
	respond(w, 202, run)
}

func (a *App) repositoryWebhookDeliveries(w http.ResponseWriter, r *http.Request, repoID string) {
	if r.Method != http.MethodGet {
		bad(w, 405, "method not allowed")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.repositoryLocked(repoID) == nil {
		bad(w, 404, "repository not found")
		return
	}
	deliveries := make([]WebhookDelivery, 0, 20)
	for i := len(a.state.WebhookDeliveries) - 1; i >= 0 && len(deliveries) < 20; i-- {
		if a.state.WebhookDeliveries[i].RepositoryID == repoID {
			deliveries = append(deliveries, a.state.WebhookDeliveries[i])
		}
	}
	respond(w, 200, deliveries)
}

func (a *App) queueWebhookApprovalLocked(kind, targetID, name, by, detail string) Run {
	run := a.startRunLocked(kind, targetID, name, by)
	run.Status = "waiting_approval"
	run.Log = detail + "\nWaiting for approval\n"
	a.state.Runs[len(a.state.Runs)-1] = run
	return run
}

func (a *App) webhookTargetLocked(repoID, kind, targetID string) (string, bool) {
	if kind == "job" {
		for _, job := range a.state.Jobs {
			if job.ID == targetID && job.RepositoryID == repoID {
				return job.Name, true
			}
		}
	}
	if kind == "workflow" {
		for _, wf := range a.state.Workflows {
			if wf.ID != targetID {
				continue
			}
			usesRepo := false
			for _, jid := range wf.JobIDs {
				found := false
				for _, job := range a.state.Jobs {
					if job.ID != jid {
						continue
					}
					found = true
					if job.RepositoryID != "" && job.RepositoryID != repoID {
						return "", false
					}
					if job.RepositoryID == repoID {
						usesRepo = true
					}
					break
				}
				if !found {
					return "", false
				}
			}
			return wf.Name, usesRepo
		}
	}
	return "", false
}

func matchingGitHubRepository(configured, cloneURL string) bool {
	a, errA := url.Parse(configured)
	b, errB := url.Parse(cloneURL)
	if errA != nil || errB != nil || !strings.EqualFold(a.Hostname(), "github.com") || !strings.EqualFold(b.Hostname(), "github.com") {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(a.Path, ".git"), strings.TrimSuffix(b.Path, ".git"))
}

func matchingPushRef(configured, ref, defaultBranch string) bool {
	if configured == "" {
		return defaultBranch != "" && ref == "refs/heads/"+defaultBranch
	}
	if commitID.MatchString(configured) {
		return false
	}
	return ref == "refs/heads/"+configured || ref == "refs/tags/"+configured
}

func (a *App) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, 405, "method not allowed")
		return
	}
	repoID := strings.TrimPrefix(r.URL.Path, "/hooks/github/")
	if repoID == "" || strings.Contains(repoID, "/") {
		bad(w, 404, "webhook not found")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		bad(w, 415, "use application/json")
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if len(deliveryID) < 1 || len(deliveryID) > 100 || strings.IndexFunc(deliveryID, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
		bad(w, 400, "invalid delivery ID")
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if len(signature) != len("sha256=")+64 || !strings.HasPrefix(signature, "sha256=") {
		bad(w, 401, "SHA-256 webhook signature required")
		return
	}
	a.mu.Lock()
	repo := a.repositoryLocked(repoID)
	if repo == nil || !repo.WebhookEnabled {
		a.mu.Unlock()
		bad(w, 404, "webhook not found")
		return
	}
	if a.webhookLimits == nil {
		a.webhookLimits = make(map[string]*rate.Limiter)
	}
	limiter := a.webhookLimits[repoID]
	if limiter == nil {
		limiter = rate.NewLimiter(10, 100)
		a.webhookLimits[repoID] = limiter
	}
	if !limiter.Allow() {
		a.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		bad(w, 429, "webhook rate limit exceeded")
		return
	}
	secret, err := os.ReadFile(a.webhookSecretPath(repoID))
	configuredURL, configuredRef := repo.URL, repo.Ref
	a.mu.Unlock()
	if err != nil || len(secret) == 0 {
		bad(w, 503, "webhook secret unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	payload, err := github.ValidatePayload(r, secret)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			bad(w, 413, "webhook payload exceeds 1 MiB")
			return
		}
		bad(w, 401, "invalid webhook signature or payload")
		return
	}
	eventType := github.WebHookType(r)
	var event any
	if eventType == "push" || eventType == "ping" {
		event, err = github.ParseWebHook(eventType, payload)
		if err != nil {
			bad(w, 400, "invalid webhook event")
			return
		}
	}
	ref := ""
	after := ""
	shouldSync := false
	if push, ok := event.(*github.PushEvent); ok {
		if push.Repo == nil || !matchingGitHubRepository(configuredURL, push.Repo.GetCloneURL()) {
			bad(w, 403, "event repository does not match")
			return
		}
		ref = push.GetRef()
		shouldSync = !push.GetDeleted() && matchingPushRef(configuredRef, ref, push.Repo.GetDefaultBranch())
		if shouldSync {
			after = strings.ToLower(push.GetAfter())
			if !commitID.MatchString(after) {
				bad(w, 400, "push after commit is missing or invalid")
				return
			}
		}
	}
	if ping, ok := event.(*github.PingEvent); ok && ping.Repo != nil && !matchingGitHubRepository(configuredURL, ping.Repo.GetCloneURL()) {
		bad(w, 403, "event repository does not match")
		return
	}
	a.mu.Lock()
	repo = a.repositoryLocked(repoID)
	currentSecret, secretErr := os.ReadFile(a.webhookSecretPath(repoID))
	if repo == nil || !repo.WebhookEnabled || repo.URL != configuredURL || repo.Ref != configuredRef || secretErr != nil || subtle.ConstantTimeCompare(secret, currentSecret) != 1 {
		a.mu.Unlock()
		bad(w, 409, "webhook configuration changed; retry delivery")
		return
	}
	if err := a.ensureDeliveryLedgerLocked(); err != nil {
		a.mu.Unlock()
		bad(w, 503, "delivery ledger unavailable")
		return
	}
	previousLedger, ledgerExists, err := a.ledgerDeliveryLocked(repoID, deliveryID)
	if err != nil {
		a.mu.Unlock()
		bad(w, 503, "delivery ledger unavailable")
		return
	}
	if ledgerExists && (previousLedger.Event != eventType || previousLedger.Ref != ref || previousLedger.After != after) {
		a.mu.Unlock()
		bad(w, 409, "delivery ID was already used for a different event")
		return
	}
	for i := range a.state.WebhookDeliveries {
		d := &a.state.WebhookDeliveries[i]
		if d.RepositoryID == repoID && d.ID == deliveryID {
			if d.Event != eventType || d.Ref != ref || d.After != after {
				a.mu.Unlock()
				bad(w, 409, "delivery ID was already used for a different event")
				return
			}
			if d.Status != "failed" {
				a.mu.Unlock()
				respond(w, 200, map[string]string{"status": "duplicate"})
				return
			}
			previousError := repo.WebhookLastError
			previousStatus := repo.WebhookLastStatus
			previousAt := repo.WebhookLastDeliveryAt
			previousDelivery := *d
			if err := a.reserveLedgerDeliveryLocked(previousDelivery); err != nil {
				a.mu.Unlock()
				bad(w, 503, "could not reserve delivery")
				return
			}
			d.Status = "processing"
			d.After = after
			d.Error = ""
			d.ReceivedAt = time.Now().UTC()
			repo.WebhookLastDeliveryAt = d.ReceivedAt
			repo.WebhookLastError = ""
			if !shouldSync {
				d.Status = "ignored"
			}
			resultStatus := d.Status
			repo.WebhookLastStatus = resultStatus
			if err := a.saveLocked(); err != nil {
				*d = previousDelivery
				repo.WebhookLastError = previousError
				repo.WebhookLastStatus = previousStatus
				repo.WebhookLastDeliveryAt = previousAt
				if restoreErr := a.restoreLedgerDeliveryLocked(repoID, deliveryID, previousLedger, ledgerExists); restoreErr != nil {
					log.Fatalf("restore webhook delivery ledger: %v", restoreErr)
				}
				a.mu.Unlock()
				bad(w, 503, "could not record delivery")
				return
			}
			a.recordLedgerDeliveryLocked(*d)
			a.mu.Unlock()
			if shouldSync {
				go a.processWebhookPush(repoID, deliveryID)
			}
			respond(w, 202, map[string]string{"status": resultStatus})
			return
		}
	}
	if ledgerExists && previousLedger.Status != "failed" && previousLedger.Status != "reserved" {
		a.mu.Unlock()
		respond(w, 200, map[string]string{"status": "duplicate"})
		return
	}
	status := "ignored"
	if shouldSync {
		status = "processing"
	}
	previousDeliveries := a.state.WebhookDeliveries
	previousAt := repo.WebhookLastDeliveryAt
	previousStatus := repo.WebhookLastStatus
	targetType, targetID := repo.WebhookTargetType, repo.WebhookTargetID
	if ledgerExists {
		// A retried delivery keeps the target selected when it was first
		// accepted, even after its UI history entry has been evicted.
		targetType, targetID = previousLedger.TargetType, previousLedger.TargetID
	}
	delivery := WebhookDelivery{RepositoryID: repoID, ID: deliveryID, Event: eventType, Ref: ref, After: after, TargetType: targetType, TargetID: targetID, Status: status, ReceivedAt: time.Now().UTC()}
	if err := a.reserveLedgerDeliveryLocked(delivery); err != nil {
		a.mu.Unlock()
		bad(w, 503, "could not reserve delivery")
		return
	}
	a.state.WebhookDeliveries = append(append([]WebhookDelivery{}, previousDeliveries...), delivery)
	if len(a.state.WebhookDeliveries) > 1000 {
		a.state.WebhookDeliveries = a.state.WebhookDeliveries[len(a.state.WebhookDeliveries)-1000:]
	}
	repo.WebhookLastDeliveryAt = time.Now().UTC()
	repo.WebhookLastStatus = status
	if err := a.saveLocked(); err != nil {
		a.state.WebhookDeliveries = previousDeliveries
		repo.WebhookLastDeliveryAt = previousAt
		repo.WebhookLastStatus = previousStatus
		if restoreErr := a.restoreLedgerDeliveryLocked(repoID, deliveryID, previousLedger, ledgerExists); restoreErr != nil {
			log.Fatalf("restore webhook delivery ledger: %v", restoreErr)
		}
		a.mu.Unlock()
		bad(w, 503, "could not record delivery")
		return
	}
	a.recordLedgerDeliveryLocked(delivery)
	a.mu.Unlock()
	if shouldSync {
		go a.processWebhookPush(repoID, deliveryID)
	}
	respond(w, 202, map[string]string{"status": status})
}

func (a *App) processWebhookPush(repoID, deliveryID string) {
	_, err := a.syncRepository(repoID)
	a.finishWebhookPush(repoID, deliveryID, err)
}

func (a *App) finishWebhookPush(repoID, deliveryID string, syncErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	repo := a.repositoryLocked(repoID)
	if repo == nil {
		return
	}
	for i := range a.state.WebhookDeliveries {
		d := &a.state.WebhookDeliveries[i]
		if d.RepositoryID != repoID || d.ID != deliveryID || d.Status != "processing" {
			continue
		}
		previousDelivery := *d
		previousError, previousStatus, previousRunID := repo.WebhookLastError, repo.WebhookLastStatus, repo.WebhookLastRunID
		previousRuns, previousAudit := a.state.Runs, a.state.Audit
		deliveryError := ""
		if syncErr != nil {
			d.Status = "failed"
			deliveryError = syncErr.Error()
		} else if d.TargetType != "" && (!repo.WebhookEnabled || repo.WebhookTargetType != d.TargetType || repo.WebhookTargetID != d.TargetID) {
			d.Status = "ignored"
			deliveryError = "trigger changed while syncing"
		} else if d.TargetType != "" && (!commitID.MatchString(d.After) || repo.Revision != d.After) {
			d.Status = "superseded"
			deliveryError = "push commit no longer matches the synced ref"
		} else if d.TargetType != "" {
			name, ok := a.webhookTargetLocked(repoID, d.TargetType, d.TargetID)
			if !ok {
				d.Status = "failed"
				deliveryError = "trigger target is unavailable"
			} else {
				run := a.queueWebhookApprovalLocked(d.TargetType, d.TargetID, name, "github push: "+repo.Name, "GitHub push "+d.Ref+" @ "+d.After)
				d.RunID = run.ID
				repo.WebhookLastRunID = run.ID
				d.Status = "succeeded"
			}
		} else {
			d.Status = "succeeded"
		}
		d.Error = deliveryError
		if !d.ReceivedAt.Before(repo.WebhookLastDeliveryAt) {
			repo.WebhookLastStatus = d.Status
			repo.WebhookLastError = deliveryError
		}
		a.auditLocked("repository.webhook_"+d.Status, repo.Name+" delivery "+deliveryID)
		if err := a.saveLocked(); err != nil {
			*d = previousDelivery
			repo.WebhookLastError, repo.WebhookLastStatus, repo.WebhookLastRunID = previousError, previousStatus, previousRunID
			a.state.Runs, a.state.Audit = previousRuns, previousAudit
			log.Printf("save webhook delivery: %v", err)
		} else if a.deliveryLedger != nil {
			a.recordLedgerDeliveryLocked(*d)
		}
		return
	}
}
