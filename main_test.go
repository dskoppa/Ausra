package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	a := &App{dataFile: filepath.Join(root, "state.json"), workspace: root, adminPass: "long-test-password", sessions: map[string]time.Time{"token": time.Now().Add(time.Hour)}, cancelRun: make(map[string]context.CancelFunc)}
	return a
}

func TestSafeDirRejectsTraversalAndSymlink(t *testing.T) {
	a := testApp(t)
	if _, err := a.safeDir("."); err != nil {
		t.Fatal(err)
	}
	if _, err := a.safeDir("../"); err == nil {
		t.Fatal("accepted directory traversal")
	}
	if err := os.Symlink(filepath.Dir(a.workspace), filepath.Join(a.workspace, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.safeDir("outside"); err == nil {
		t.Fatal("accepted symlink outside workspace")
	}
}

func TestApprovedWorkflowRunsInOrder(t *testing.T) {
	a := testApp(t)
	first := Job{ID: "first", Name: "First", Type: "shell", Command: "printf first", Dir: ".", TimeoutSec: 5}
	second := Job{ID: "second", Name: "Second", Type: "shell", Command: "printf second", Dir: ".", TimeoutSec: 5}
	a.state.Jobs = []Job{first, second}
	wf := Workflow{ID: "workflow", Name: "Test workflow", JobIDs: []string{"first", "second"}, RequireApproval: true}
	a.state.Workflows = []Workflow{wf}
	run := a.startRun("workflow", wf.ID, wf.Name, "test")
	a.updateRun(run.ID, func(r *Run) { r.Status = "waiting_approval" })
	if a.status(run.ID) != "waiting_approval" {
		t.Fatal("workflow did not wait")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/approve", nil)
	recorder := httptest.NewRecorder()
	a.runAction(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("approval returned %d: %s", recorder.Code, recorder.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.status(run.ID) == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.status(run.ID) != "succeeded" {
		t.Fatalf("run status: %s", a.status(run.ID))
	}
	a.mu.Lock()
	got := a.state.Runs[0]
	a.mu.Unlock()
	if !strings.Contains(got.Log, "first") || !strings.Contains(got.Log, "second") || strings.Index(got.Log, "first") > strings.Index(got.Log, "second") {
		t.Fatalf("wrong execution order: %q", got.Log)
	}
	b, err := os.ReadFile(a.dataFile)
	if err != nil {
		t.Fatal(err)
	}
	var persisted State
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Runs[0].Status != "succeeded" {
		t.Fatal("run was not persisted")
	}
}

func TestApprovedJobRuns(t *testing.T) {
	a := testApp(t)
	job := Job{ID: "job", Name: "Approval job", Type: "shell", Command: "printf approved", Dir: ".", TimeoutSec: 5}
	a.state.Jobs = []Job{job}
	run := a.startRun("job", job.ID, job.Name, "test")
	a.updateRun(run.ID, func(r *Run) { r.Status = "waiting_approval" })
	w := httptest.NewRecorder()
	a.runAction(w, httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/approve", nil))
	if w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && a.status(run.ID) != "succeeded" {
		time.Sleep(10 * time.Millisecond)
	}
	if a.status(run.ID) != "succeeded" {
		t.Fatalf("job status: %s", a.status(run.ID))
	}
}

func TestWorkflowStopsAfterFailedJob(t *testing.T) {
	a := testApp(t)
	a.state.Jobs = []Job{{ID: "bad", Name: "Bad", Type: "shell", Command: "exit 7", Dir: ".", TimeoutSec: 5}, {ID: "skipped", Name: "Skipped", Type: "shell", Command: "printf should-not-run", Dir: ".", TimeoutSec: 5}}
	wf := Workflow{ID: "workflow", Name: "Failure", JobIDs: []string{"bad", "skipped"}}
	run := a.startRun("workflow", wf.ID, wf.Name, "test")
	a.executeWorkflow(run.ID, wf)
	if a.status(run.ID) != "failed" {
		t.Fatalf("run status: %s", a.status(run.ID))
	}
	a.mu.Lock()
	got := a.state.Runs[0]
	a.mu.Unlock()
	if strings.Contains(got.Log, "should-not-run") || got.Step != 1 {
		t.Fatalf("workflow continued after failure: %#v", got)
	}
}

func TestAdminPasswordIsNotPassedToJobs(t *testing.T) {
	t.Setenv("AUSRA_ADMIN_PASSWORD", "secret-for-test")
	a := testApp(t)
	job := Job{ID: "env", Name: "Environment", Type: "shell", Command: "printf %s \"${AUSRA_ADMIN_PASSWORD:-missing}\"", Dir: ".", TimeoutSec: 5}
	run := a.startRun("job", job.ID, job.Name, "test")
	a.executeJob(run.ID, job)
	a.mu.Lock()
	got := a.state.Runs[0]
	a.mu.Unlock()
	if got.Status != "succeeded" || !strings.Contains(got.Log, "missing") || strings.Contains(got.Log, "secret-for-test") {
		t.Fatalf("job inherited admin password: %#v", got)
	}
}
