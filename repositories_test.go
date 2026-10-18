package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitTest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func commitTestFile(t *testing.T, root, value string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "version.txt"), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, "-C", root, "add", "version.txt")
	runGitTest(t, "-C", root, "commit", "-q", "-m", value)
	return runGitTest(t, "-C", root, "rev-parse", "HEAD")
}

func TestRepositorySyncPinsRunRevision(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(a.workspace, "source")
	runGitTest(t, "init", "-q", "-b", "main", source)
	runGitTest(t, "-C", source, "config", "user.email", "test@example.com")
	runGitTest(t, "-C", source, "config", "user.name", "Aušra Test")
	first := commitTestFile(t, source, "first")
	a.state.Repositories = []Repository{{ID: "repo-1", Name: "Test source", Project: "Default", URL: source, Ref: "main"}}
	repo, err := a.syncRepository("repo-1")
	if err != nil || repo.Revision != first {
		t.Fatalf("initial sync: %v, revision %q want %q", err, repo.Revision, first)
	}
	runGitTest(t, "-C", source, "tag", "v1")
	for _, ref := range []string{"v1", first, ""} {
		a.state.Repositories[0].Ref = ref
		resolved, err := a.syncRepository("repo-1")
		if err != nil || resolved.Revision != first {
			t.Fatalf("resolve %q: %v, revision %q want %q", ref, err, resolved.Revision, first)
		}
	}
	a.state.Repositories[0].Ref = "main"
	job := Job{ID: "job-1", Name: "Read source", Type: "shell", RepositoryID: repo.ID, Command: "cat version.txt", Dir: ".", TimeoutSec: 5}
	a.state.Jobs = []Job{job}
	run := a.startRun("job", job.ID, job.Name, "test")
	if len(run.Sources) != 1 || run.Sources[0].Commit != first {
		t.Fatalf("run did not pin source: %#v", run.Sources)
	}
	workflow := Workflow{ID: "workflow-1", Name: "Approval workflow", JobIDs: []string{job.ID}, RequireApproval: true}
	a.state.Workflows = []Workflow{workflow}
	workflowRun := a.startRun("workflow", workflow.ID, workflow.Name, "test")
	if len(workflowRun.Sources) != 1 || workflowRun.Sources[0].Commit != first {
		t.Fatalf("workflow did not pin source: %#v", workflowRun.Sources)
	}
	second := commitTestFile(t, source, "second")
	repo, err = a.syncRepository("repo-1")
	if err != nil || repo.Revision != second {
		t.Fatalf("second sync: %v, revision %q want %q", err, repo.Revision, second)
	}
	a.executeJob(run.ID, job)
	a.mu.Lock()
	finished := a.state.Runs[0]
	a.mu.Unlock()
	if finished.Status != "succeeded" || !strings.Contains(finished.Log, "first") || strings.Contains(finished.Log, "second") {
		t.Fatalf("run did not use pinned revision: %#v", finished)
	}
	a.executeWorkflow(workflowRun.ID, workflow)
	a.mu.Lock()
	workflowFinished := a.state.Runs[1]
	a.mu.Unlock()
	if workflowFinished.Status != "succeeded" || !strings.Contains(workflowFinished.Log, "first") || strings.Contains(workflowFinished.Log, "second") {
		t.Fatalf("workflow did not use approval-time revision: %#v", workflowFinished)
	}
	checkouts, err := os.ReadDir(filepath.Join(filepath.Dir(a.dataFile), "checkouts", run.ID))
	if err != nil || len(checkouts) != 0 {
		t.Fatalf("run checkout was not removed: %v, entries %d", err, len(checkouts))
	}
}

func TestRepositoryInputValidation(t *testing.T) {
	for _, raw := range []string{"file:///tmp/repo", "http://example.com/repo", "https://user:token@example.com/repo", "https://example.com/repo?token=secret", "https://example.com/"} {
		if validRepositoryURL(raw) {
			t.Fatalf("accepted unsafe repository URL: %q", raw)
		}
	}
	if !validRepositoryURL("https://example.com/org/repo.git") || !validRepositoryRef("feature/deploy") || validRepositoryRef("--upload-pack=bad") {
		t.Fatal("valid repository URL or ref classification failed")
	}
	for _, dir := range []string{"../outside", "/tmp", "a/../../outside"} {
		if validRelativeDir(dir) {
			t.Fatalf("accepted repository directory outside checkout: %q", dir)
		}
	}
}
