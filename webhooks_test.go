package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
	w = httptest.NewRecorder()
	restarted.githubWebhook(w, signedGitHubRequest(setup.Path, "ping", "one", ping, setup.Secret))
	if w.Code != 200 {
		t.Fatalf("duplicate after restart: %d", w.Code)
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
	if len(a.state.Runs) != 0 || a.state.WebhookDeliveries[0].Status != "superseded" {
		t.Fatal("superseded push triggered a run")
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
