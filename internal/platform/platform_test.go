package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryRoundTrip(t *testing.T) {
	root := t.TempDir()
	want := ProjectMemory{Architecture: "service graph", Decisions: []string{"transactions"}}
	if err := SaveMemory(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMemory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Architecture != want.Architecture || len(got.Decisions) != 1 {
		t.Fatalf("unexpected memory: %+v", got)
	}
}

func TestWorkspaceManager(t *testing.T) {
	root := t.TempDir()
	manager := NewWorkspaceManager()
	if _, err := manager.Add("demo", root); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 1 {
		t.Fatal("workspace was not added")
	}
	manager.Remove("demo")
	if len(manager.List()) != 0 {
		t.Fatal("workspace was not removed")
	}
}

func TestReviewDiff(t *testing.T) {
	review := ReviewDiff(`+ api_key = "1234567890123456"`)
	if review.Passed {
		t.Fatal("secret finding should fail review")
	}
}

func TestJobRunner(t *testing.T) {
	runner := NewJobRunner()
	job, err := runner.Start(context.Background(), "test", func(ctx context.Context, progress func(string)) (string, error) {
		progress("done")
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		got, ok := runner.Get(job.ID)
		if ok && got.Status == JobSucceeded {
			if got.Result != "ok" {
				t.Fatal(got.Result)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestJobRunnerReturnsIndependentSnapshot(t *testing.T) {
	runner := NewJobRunner()
	started := make(chan struct{})
	release := make(chan struct{})
	job, err := runner.Start(context.Background(), "snapshot", func(context.Context, func(string)) (string, error) {
		close(started)
		<-release
		return "complete", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if job.Status != JobQueued {
		t.Fatalf("start snapshot changed after return: %q", job.Status)
	}
	close(release)
}

func TestProof(t *testing.T) {
	root := t.TempDir()
	proof, err := SaveProof(root, Proof{Goal: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".aicli", "proofs", proof.ID+".json")); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProof(root, proof.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != proof.ID {
		t.Fatalf("loaded proof ID = %q, want %q", loaded.ID, proof.ID)
	}
}

func TestProofRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"../escape", "..", "nested/id", "nested\\id", "proof.json"} {
		if _, err := SaveProof(root, Proof{ID: id, Goal: "test"}); err == nil {
			t.Errorf("SaveProof accepted invalid ID %q", id)
		}
		if _, err := LoadProof(root, id); err == nil {
			t.Errorf("LoadProof accepted invalid ID %q", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected path outside proof directory; err=%v", err)
	}
}


func TestGraphRejectsImportsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside"), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	if got := resolveImport(root, "main.go", "../outside.go"); got != "" {
		t.Fatalf("expected outside import to be rejected, got %q", got)
	}
	if got := resolveImport(root, "main.go", outside); got != "" {
		t.Fatalf("expected absolute import to be rejected, got %q", got)
	}
}


func TestPlatformStorageRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "private.json")
	if err := os.WriteFile(external, []byte(`{"architecture":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".aicli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "memory.json")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := LoadMemory(root); err == nil {
		t.Fatal("LoadMemory followed a symlink outside the workspace")
	}
	if err := SaveMemory(root, ProjectMemory{Architecture: "overwrite"}); err == nil {
		t.Fatal("SaveMemory accepted a symlinked target")
	}
	data, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"architecture":"outside"}` {
		t.Fatalf("external target was modified: %s", data)
	}
}

func TestBrowserInspectionRejectsNonLocalTargets(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.com",
		"http://127.0.0.1@example.com",
		"file:///etc/passwd",
		"http://192.0.2.1",
	} {
		if _, err := InspectURL(context.Background(), rawURL); err == nil {
			t.Errorf("InspectURL accepted unsafe URL %q", rawURL)
		}
	}
	if _, err := dialLoopback(context.Background(), "tcp", "192.0.2.1:80"); err == nil {
		t.Fatal("dialLoopback accepted a non-loopback IP")
	}
}


func TestRulesRejectSymlinksAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	rulesDir := filepath.Join(root, ".aicli", "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "private.md")
	if err := os.WriteFile(external, []byte("private content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(rulesDir, "external.md")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := LoadRules(root); err == nil {
		t.Fatal("LoadRules followed a symlink outside the workspace")
	}
	if err := os.Remove(filepath.Join(rulesDir, "external.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "large.md"), make([]byte, (64<<10)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRules(root); err == nil {
		t.Fatal("LoadRules accepted a file larger than its configured limit")
	}
}
