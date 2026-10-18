package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func signedGitHubRequest(path, event, delivery, body, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GitHub-Event", event)
	r.Header.Set("X-GitHub-Delivery", delivery)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func TestGitHubWebhookSetupSignatureAndReplay(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main"}}
	w := httptest.NewRecorder()
	a.repositoryWebhookSettings(w, httptest.NewRequest(http.MethodPost, "/api/repositories/repo1/webhook", nil), "repo1")
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	var setup struct {
		Secret string `json:"secret"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &setup); err != nil || len(setup.Secret) != 64 {
		t.Fatalf("setup response: %v", err)
	}
	if strings.Contains(string(mustReadFile(t, a.dataFile)), setup.Secret) {
		t.Fatal("secret leaked into state")
	}
	if setup.Path != "/hooks/github/repo1" {
		t.Fatal(setup.Path)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	controlID := signedGitHubRequest(setup.Path, "ping", "bad\x1bid", ping, setup.Secret)
	w = httptest.NewRecorder()
	a.githubWebhook(w, controlID)
	if w.Code != 400 {
		t.Fatalf("control-character delivery ID accepted: %d", w.Code)
	}
	oversized := signedGitHubRequest(setup.Path, "ping", "oversized", strings.Repeat("x", 1<<20+1), setup.Secret)
	w = httptest.NewRecorder()
	a.githubWebhook(w, oversized)
	if w.Code != 413 {
		t.Fatalf("oversized webhook returned %d", w.Code)
	}
	unsigned := signedGitHubRequest(setup.Path, "ping", "unsigned", ping, setup.Secret)
	unsigned.Header.Del("X-Hub-Signature-256")
	w = httptest.NewRecorder()
	a.githubWebhook(w, unsigned)
	if w.Code != 401 {
		t.Fatalf("unsigned event accepted: %d", w.Code)
	}
	for _, tc := range []struct {
		name, event, delivery, body, secret string
		want                                int
	}{
		{"bad signature", "ping", "one", ping, "wrong", 401},
		{"ping", "ping", "one", ping, setup.Secret, 202},
		{"duplicate", "ping", "one", ping, setup.Secret, 200},
		{"delivery ID reused", "push", "one", `{"ref":"refs/heads/dev","repository":{"clone_url":"https://github.com/team/infra.git","default_branch":"main"}}`, setup.Secret, 409},
		{"wrong repository", "push", "two", `{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/other/infra.git","default_branch":"main"}}`, setup.Secret, 403},
		{"other branch", "push", "three", `{"ref":"refs/heads/dev","repository":{"clone_url":"https://github.com/team/infra.git","default_branch":"main"}}`, setup.Secret, 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			a.githubWebhook(w, signedGitHubRequest(setup.Path, tc.event, tc.delivery, tc.body, tc.secret))
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
	if len(a.state.WebhookDeliveries) != 2 {
		t.Fatalf("recorded %d deliveries", len(a.state.WebhookDeliveries))
	}
	restarted := testApp(t)
	restarted.dataFile = a.dataFile
	if err := json.Unmarshal(mustReadFile(t, a.dataFile), &restarted.state); err != nil {
		t.Fatal(err)
	}
	if err := a.closeDeliveryLedger(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	restarted.githubWebhook(w, signedGitHubRequest(setup.Path, "ping", "one", ping, setup.Secret))
	if w.Code != 200 {
		t.Fatalf("duplicate after restart: %d", w.Code)
	}
	if err := restarted.closeDeliveryLedger(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.repositoryWebhookSettings(w, httptest.NewRequest(http.MethodPost, "/api/repositories/repo1/webhook", nil), "repo1")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	a.githubWebhook(w, signedGitHubRequest(setup.Path, "ping", "four", ping, setup.Secret))
	if w.Code != 401 {
		t.Fatalf("old secret accepted after rotation: %d", w.Code)
	}
	w = httptest.NewRecorder()
	a.repositoryWebhookSettings(w, httptest.NewRequest(http.MethodDelete, "/api/repositories/repo1/webhook", nil), "repo1")
	if w.Code != 200 || a.state.Repositories[0].WebhookEnabled {
		t.Fatal("disable failed")
	}
}

func TestWebhookSettingsRollBackWhenStateCannotBeSaved(t *testing.T) {
	fresh := testApp(t)
	fresh.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git"}}
	if err := os.Mkdir(fresh.dataFile+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	failedSetup := httptest.NewRecorder()
	fresh.repositoryWebhookSettings(failedSetup, httptest.NewRequest(http.MethodPost, "/api/repositories/repo1/webhook", nil), "repo1")
	if failedSetup.Code != 503 || fresh.state.Repositories[0].WebhookEnabled || len(fresh.state.Audit) != 0 {
		t.Fatalf("failed setup was accepted: %d", failedSetup.Code)
	}
	if _, err := os.Stat(fresh.webhookSecretPath("repo1")); !os.IsNotExist(err) {
		t.Fatalf("failed setup left a webhook secret: %v", err)
	}

	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git"}}
	request := func(method string) int {
		w := httptest.NewRecorder()
		a.repositoryWebhookSettings(w, httptest.NewRequest(method, "/api/repositories/repo1/webhook", nil), "repo1")
		return w.Code
	}
	if code := request(http.MethodPost); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	secret := string(mustReadFile(t, a.webhookSecretPath("repo1")))
	auditCount := len(a.state.Audit)
	if err := os.Mkdir(a.dataFile+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if code := request(http.MethodPost); code != 503 {
		t.Fatalf("rotation with failed save: %d", code)
	}
	if got := string(mustReadFile(t, a.webhookSecretPath("repo1"))); got != secret {
		t.Fatal("failed rotation changed the active secret")
	}
	if code := request(http.MethodDelete); code != 503 {
		t.Fatalf("disable with failed save: %d", code)
	}
	if !a.state.Repositories[0].WebhookEnabled || len(a.state.Audit) != auditCount || string(mustReadFile(t, a.webhookSecretPath("repo1"))) != secret {
		t.Fatal("failed webhook settings change was not rolled back")
	}
}

func TestWebhookTriggerRollsBackWhenStateCannotBeSaved(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", WebhookEnabled: true}}
	a.state.Jobs = []Job{{ID: "job1", Name: "Plan", RepositoryID: "repo1"}}
	if err := os.Mkdir(a.dataFile+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.repositoryWebhookTrigger(w, httptest.NewRequest(http.MethodPut, "/api/repositories/repo1/trigger", strings.NewReader(`{"targetType":"job","targetId":"job1"}`)), "repo1")
	if w.Code != 503 || a.state.Repositories[0].WebhookTargetID != "" || len(a.state.Audit) != 0 {
		t.Fatalf("failed trigger save was accepted: %d, %+v", w.Code, a.state.Repositories[0])
	}
}

func TestWebhookReplayAfterRecentHistoryEviction(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	path := "/hooks/github/repo1"
	deliver := func(id string) int {
		w := httptest.NewRecorder()
		a.githubWebhook(w, signedGitHubRequest(path, "ping", id, ping, secret))
		return w.Code
	}
	if code := deliver("old"); code != 202 {
		t.Fatalf("first delivery: %d", code)
	}
	for i := 0; i < 1000; i++ {
		a.state.WebhookDeliveries = append(a.state.WebhookDeliveries, WebhookDelivery{RepositoryID: "repo1", ID: fmt.Sprintf("recent-%d", i)})
	}
	if code := deliver("new"); code != 202 {
		t.Fatalf("new delivery: %d", code)
	}
	if len(a.state.WebhookDeliveries) != 1000 || a.state.WebhookDeliveries[0].ID == "old" {
		t.Fatal("old delivery was not evicted from recent history")
	}
	if code := deliver("old"); code != 200 {
		t.Fatalf("old delivery replayed after eviction: %d", code)
	}
	differentEvent := `{"ref":"refs/heads/dev","repository":{"clone_url":"https://github.com/team/infra.git","default_branch":"main"}}`
	w := httptest.NewRecorder()
	a.githubWebhook(w, signedGitHubRequest(path, "push", "old", differentEvent, secret))
	if w.Code != 409 {
		t.Fatalf("reused delivery ID accepted for another event: %d", w.Code)
	}
	if err := a.closeDeliveryLedger(); err != nil {
		t.Fatal(err)
	}
	restarted := testApp(t)
	restarted.dataFile = a.dataFile
	if err := json.Unmarshal(mustReadFile(t, a.dataFile), &restarted.state); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	restarted.githubWebhook(w, signedGitHubRequest(path, "ping", "old", ping, secret))
	if w.Code != 200 {
		t.Fatalf("old delivery replayed after restart: %d", w.Code)
	}
}

func TestWebhookLedgerBackfillsExistingState(t *testing.T) {
	a := testApp(t)
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "before-upgrade", Event: "ping", Status: "ignored"}}
	if err := a.ensureDeliveryLedgerLocked(); err != nil {
		t.Fatal(err)
	}
	got, found, err := a.ledgerDeliveryLocked("repo1", "before-upgrade")
	if err != nil || !found || got.Status != "ignored" {
		t.Fatalf("existing delivery was not backfilled: found=%v delivery=%+v err=%v", found, got, err)
	}
}

func TestWebhookFailedDeliveryCanRetryAfterEviction(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	deliver := func(id string) int {
		w := httptest.NewRecorder()
		a.githubWebhook(w, signedGitHubRequest("/hooks/github/repo1", "ping", id, ping, secret))
		return w.Code
	}
	if code := deliver("failed"); code != 202 {
		t.Fatal(code)
	}
	a.state.WebhookDeliveries[0].Status = "failed"
	if err := a.putLedgerDeliveryLocked(a.state.WebhookDeliveries[0]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		a.state.WebhookDeliveries = append(a.state.WebhookDeliveries, WebhookDelivery{RepositoryID: "repo1", ID: fmt.Sprintf("recent-%d", i)})
	}
	if code := deliver("new"); code != 202 {
		t.Fatal(code)
	}
	if code := deliver("failed"); code != 202 {
		t.Fatalf("failed delivery was not retried after eviction: %d", code)
	}
	if code := deliver("failed"); code != 200 {
		t.Fatalf("retried delivery was processed twice: %d", code)
	}
}

func TestWebhookRetryAfterEvictionKeepsOriginalTarget(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true, WebhookTargetType: "job", WebhookTargetID: "new"}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.ensureDeliveryLedgerLocked(); err != nil {
		t.Fatal(err)
	}
	previous := WebhookDelivery{RepositoryID: "repo1", ID: "retried", Event: "push", Ref: "refs/heads/dev", TargetType: "job", TargetID: "old", Status: "failed"}
	if err := a.putLedgerDeliveryLocked(previous); err != nil {
		t.Fatal(err)
	}
	body := `{"ref":"refs/heads/dev","repository":{"clone_url":"https://github.com/team/infra.git","default_branch":"main"}}`
	w := httptest.NewRecorder()
	a.githubWebhook(w, signedGitHubRequest("/hooks/github/repo1", "push", "retried", body, secret))
	if w.Code != 202 || len(a.state.WebhookDeliveries) != 1 || a.state.WebhookDeliveries[0].TargetID != "old" {
		t.Fatalf("retry changed target: %d, %+v", w.Code, a.state.WebhookDeliveries)
	}
}

func TestWebhookRejectsUnavailableLedger(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(a.dataFile), "webhook-deliveries.db"), 0700); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	w := httptest.NewRecorder()
	a.githubWebhook(w, signedGitHubRequest("/hooks/github/repo1", "ping", "one", ping, secret))
	if w.Code != 503 || len(a.state.WebhookDeliveries) != 0 {
		t.Fatalf("ledger failure accepted delivery: %d, %+v", w.Code, a.state.WebhookDeliveries)
	}
}

func TestWebhookFailedStateSaveReleasesDeliveryReservation(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.dataFile+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	deliver := func() int {
		w := httptest.NewRecorder()
		a.githubWebhook(w, signedGitHubRequest("/hooks/github/repo1", "ping", "one", ping, secret))
		return w.Code
	}
	if code := deliver(); code != 503 || len(a.state.WebhookDeliveries) != 0 {
		t.Fatalf("failed state save accepted delivery: %d", code)
	}
	if _, found, err := a.ledgerDeliveryLocked("repo1", "one"); err != nil || found {
		t.Fatalf("failed state save left a reservation: found=%v err=%v", found, err)
	}
	if err := os.Remove(a.dataFile + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if code := deliver(); code != 202 {
		t.Fatalf("retry after recovered state save: %d", code)
	}
}

func TestWebhookRateLimitDoesNotRecordRejectedDelivery(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	a.webhookLimits = map[string]*rate.Limiter{"repo1": rate.NewLimiter(0, 1)}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	first := httptest.NewRecorder()
	a.githubWebhook(first, signedGitHubRequest("/hooks/github/repo1", "ping", "one", ping, secret))
	if first.Code != 202 {
		t.Fatalf("first delivery: %d", first.Code)
	}
	second := httptest.NewRecorder()
	a.githubWebhook(second, signedGitHubRequest("/hooks/github/repo1", "ping", "two", ping, secret))
	if second.Code != 429 || second.Header().Get("Retry-After") != "1" || len(a.state.WebhookDeliveries) != 1 {
		t.Fatalf("rate limit failed: %d, %q, %+v", second.Code, second.Header().Get("Retry-After"), a.state.WebhookDeliveries)
	}
}

func TestConcurrentWebhookReplayRecordsOneDelivery(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", URL: "https://github.com/team/infra.git", Ref: "main", WebhookEnabled: true}}
	secret := "test-webhook-secret"
	if err := os.MkdirAll(filepath.Dir(a.webhookSecretPath("repo1")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.webhookSecretPath("repo1"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	ping := `{"repository":{"clone_url":"https://github.com/team/infra.git"}}`
	const requests = 24
	codes := make(chan int, requests)
	var workers sync.WaitGroup
	for i := 0; i < requests; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			w := httptest.NewRecorder()
			a.githubWebhook(w, signedGitHubRequest("/hooks/github/repo1", "ping", "same-id", ping, secret))
			codes <- w.Code
		}()
	}
	workers.Wait()
	close(codes)
	accepted, duplicate := 0, 0
	for code := range codes {
		switch code {
		case 202:
			accepted++
		case 200:
			duplicate++
		default:
			t.Fatalf("unexpected concurrent response: %d", code)
		}
	}
	if accepted != 1 || duplicate != requests-1 || len(a.state.WebhookDeliveries) != 1 {
		t.Fatalf("concurrent replay accepted %d, duplicated %d, recorded %d", accepted, duplicate, len(a.state.WebhookDeliveries))
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMatchingPushRef(t *testing.T) {
	for _, tc := range []struct {
		configured, ref, branch string
		want                    bool
	}{
		{"", "refs/heads/main", "main", true},
		{"", "refs/heads/feature", "main", false},
		{"main", "refs/heads/main", "main", true},
		{"v1", "refs/tags/v1", "main", true},
		{"0123456789012345678901234567890123456789", "refs/heads/main", "main", false},
	} {
		if got := matchingPushRef(tc.configured, tc.ref, tc.branch); got != tc.want {
			t.Fatalf("%q %q: got %v", tc.configured, tc.ref, got)
		}
	}
}

func TestWebhookTriggerRequiresMatchingRepository(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", WebhookEnabled: true}}
	a.state.Jobs = []Job{{ID: "same", Name: "Same", RepositoryID: "repo1"}, {ID: "other", Name: "Other", RepositoryID: "repo2"}}
	a.state.Workflows = []Workflow{{ID: "mixed", Name: "Mixed", JobIDs: []string{"same", "other"}}, {ID: "valid", Name: "Valid", JobIDs: []string{"same"}}}
	for _, tc := range []struct {
		kind, id string
		code     int
	}{{"job", "other", 400}, {"workflow", "mixed", 400}, {"workflow", "valid", 200}, {"job", "same", 200}} {
		body := `{"targetType":"` + tc.kind + `","targetId":"` + tc.id + `"}`
		w := httptest.NewRecorder()
		a.repositoryWebhookTrigger(w, httptest.NewRequest(http.MethodPut, "/api/repositories/repo1/trigger", strings.NewReader(body)), "repo1")
		if w.Code != tc.code {
			t.Fatalf("%s %s: got %d want %d: %s", tc.kind, tc.id, w.Code, tc.code, w.Body)
		}
	}
}

func TestWebhookPushPinsCommitAndWaitsForApproval(t *testing.T) {
	a := testApp(t)
	commit := strings.Repeat("a", 40)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", Revision: commit, WebhookEnabled: true, WebhookTargetType: "job", WebhookTargetID: "job1"}}
	a.state.Jobs = []Job{{ID: "job1", Name: "Plan", RepositoryID: "repo1", Type: "shell", Command: "echo plan", TimeoutSec: 5}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "delivery1", Event: "push", Ref: "refs/heads/main", After: commit, TargetType: "job", TargetID: "job1", Status: "processing"}}
	a.finishWebhookPush("repo1", "delivery1", nil)
	a.finishWebhookPush("repo1", "delivery1", nil)
	if len(a.state.Runs) != 1 {
		t.Fatalf("got %d runs, want one", len(a.state.Runs))
	}
	run := a.state.Runs[0]
	if run.Status != "waiting_approval" || len(run.Sources) != 1 || run.Sources[0].Commit != commit || a.state.WebhookDeliveries[0].RunID != run.ID {
		t.Fatalf("bad triggered run: %#v, delivery %#v", run, a.state.WebhookDeliveries[0])
	}
	var saved State
	if err := json.Unmarshal(mustReadFile(t, a.dataFile), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Runs) != 1 || saved.Runs[0].Status != "waiting_approval" || saved.WebhookDeliveries[0].Status != "succeeded" {
		t.Fatal("run and delivery not persisted together")
	}
}

func TestWebhookSupersededPushDoesNotRun(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", Revision: strings.Repeat("b", 40), WebhookEnabled: true, WebhookTargetType: "job", WebhookTargetID: "job1"}}
	a.state.Jobs = []Job{{ID: "job1", Name: "Plan", RepositoryID: "repo1"}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "old", After: strings.Repeat("a", 40), TargetType: "job", TargetID: "job1", Status: "processing"}}
	a.finishWebhookPush("repo1", "old", nil)
	if len(a.state.Runs) != 0 || a.state.WebhookDeliveries[0].Status != "superseded" || a.state.WebhookDeliveries[0].Error == "" {
		t.Fatal("superseded push triggered a run")
	}
}

func TestWebhookDeliveryRetainsFailureDetail(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", WebhookEnabled: true}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "failed", Status: "processing"}}
	a.finishWebhookPush("repo1", "failed", errors.New("Git fetch failed"))
	if got := a.state.WebhookDeliveries[0]; got.Status != "failed" || got.Error != "Git fetch failed" {
		t.Fatalf("delivery failure detail lost: %+v", got)
	}
	var persisted State
	if err := json.Unmarshal(mustReadFile(t, a.dataFile), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.WebhookDeliveries[0].Error != "Git fetch failed" {
		t.Fatal("delivery error was not persisted")
	}
}

func TestOlderWebhookCompletionDoesNotReplaceLatestStatus(t *testing.T) {
	a := testApp(t)
	oldAt := time.Now().UTC().Add(-time.Minute)
	newAt := time.Now().UTC()
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", WebhookEnabled: true, WebhookLastDeliveryAt: newAt, WebhookLastStatus: "processing"}}
	a.state.WebhookDeliveries = []WebhookDelivery{
		{RepositoryID: "repo1", ID: "old", Status: "processing", ReceivedAt: oldAt},
		{RepositoryID: "repo1", ID: "new", Status: "processing", ReceivedAt: newAt},
	}
	a.finishWebhookPush("repo1", "old", errors.New("old sync failed"))
	if a.state.WebhookDeliveries[0].Error != "old sync failed" || a.state.Repositories[0].WebhookLastStatus != "processing" || a.state.Repositories[0].WebhookLastError != "" {
		t.Fatalf("old completion replaced newer summary: %+v", a.state.Repositories[0])
	}
	a.finishWebhookPush("repo1", "new", nil)
	if a.state.Repositories[0].WebhookLastStatus != "succeeded" || a.state.Repositories[0].WebhookLastError != "" {
		t.Fatalf("latest status was not recorded: %+v", a.state.Repositories[0])
	}
}

func TestInterruptedWebhookRecoveryUpdatesLatestSummary(t *testing.T) {
	a := testApp(t)
	oldAt := time.Now().UTC().Add(-time.Minute)
	newAt := time.Now().UTC()
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", WebhookLastDeliveryAt: newAt, WebhookLastStatus: "processing"}}
	a.state.WebhookDeliveries = []WebhookDelivery{
		{RepositoryID: "repo1", ID: "old", Status: "processing", ReceivedAt: oldAt},
		{RepositoryID: "repo1", ID: "new", Status: "processing", ReceivedAt: newAt},
	}
	a.recoverInterruptedLocked()
	if a.state.Repositories[0].WebhookLastStatus != "failed" || a.state.Repositories[0].WebhookLastError == "" {
		t.Fatalf("latest repository summary was not recovered: %+v", a.state.Repositories[0])
	}
	for _, delivery := range a.state.WebhookDeliveries {
		if delivery.Status != "failed" || delivery.Error == "" {
			t.Fatalf("interrupted delivery was not recovered: %+v", delivery)
		}
	}
}

func TestWebhookChangedTriggerDoesNotRun(t *testing.T) {
	a := testApp(t)
	commit := strings.Repeat("a", 40)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", Revision: commit, WebhookEnabled: true, WebhookTargetType: "job", WebhookTargetID: "new"}}
	a.state.Jobs = []Job{{ID: "old", Name: "Old", RepositoryID: "repo1"}, {ID: "new", Name: "New", RepositoryID: "repo1"}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "changed", After: commit, TargetType: "job", TargetID: "old", Status: "processing"}}
	a.finishWebhookPush("repo1", "changed", nil)
	if len(a.state.Runs) != 0 || a.state.WebhookDeliveries[0].Status != "ignored" {
		t.Fatal("changed trigger created a run")
	}
}

func TestWebhookWorkflowTriggerAlwaysWaitsForApproval(t *testing.T) {
	a := testApp(t)
	commit := strings.Repeat("c", 40)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", Revision: commit, WebhookEnabled: true, WebhookTargetType: "workflow", WebhookTargetID: "wf1"}}
	a.state.Jobs = []Job{{ID: "job1", Name: "Plan", RepositoryID: "repo1"}}
	a.state.Workflows = []Workflow{{ID: "wf1", Name: "Pipeline", JobIDs: []string{"job1"}, RequireApproval: false}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "wf-delivery", After: commit, TargetType: "workflow", TargetID: "wf1", Status: "processing"}}
	a.finishWebhookPush("repo1", "wf-delivery", nil)
	if len(a.state.Runs) != 1 || a.state.Runs[0].TargetType != "workflow" || a.state.Runs[0].Status != "waiting_approval" {
		t.Fatalf("unexpected workflow run: %#v", a.state.Runs)
	}
}

func TestWebhookTestRunRequiresConfiguredTrigger(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1", Name: "Source", Revision: strings.Repeat("a", 40), WebhookEnabled: true}}
	w := httptest.NewRecorder()
	a.repositoryWebhookTest(w, httptest.NewRequest(http.MethodPost, "/api/repositories/repo1/trigger/test", nil), "repo1")
	if w.Code != 409 {
		t.Fatalf("unconfigured test: %d", w.Code)
	}
	a.state.Jobs = []Job{{ID: "job1", Name: "Plan", RepositoryID: "repo1"}}
	a.state.Repositories[0].WebhookTargetType, a.state.Repositories[0].WebhookTargetID = "job", "job1"
	w = httptest.NewRecorder()
	a.repositoryWebhookTest(w, httptest.NewRequest(http.MethodPost, "/api/repositories/repo1/trigger/test", nil), "repo1")
	if w.Code != 202 || len(a.state.Runs) != 1 || a.state.Runs[0].Status != "waiting_approval" {
		t.Fatalf("test run: %d %s", w.Code, w.Body)
	}
}

func TestWebhookDeliveryHistoryIsScopedAndNewestFirst(t *testing.T) {
	a := testApp(t)
	a.state.Repositories = []Repository{{ID: "repo1"}, {ID: "repo2"}}
	a.state.WebhookDeliveries = []WebhookDelivery{{RepositoryID: "repo1", ID: "old"}, {RepositoryID: "repo2", ID: "foreign"}, {RepositoryID: "repo1", ID: "new"}}
	w := httptest.NewRecorder()
	a.repositoryWebhookDeliveries(w, httptest.NewRequest(http.MethodGet, "/api/repositories/repo1/deliveries", nil), "repo1")
	if w.Code != 200 {
		t.Fatalf("history: %d", w.Code)
	}
	var got []WebhookDelivery
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("wrong delivery history: %#v", got)
	}
}
