package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if strings.Contains(filepath.Clean(root), string(filepath.Separator)+".."+string(filepath.Separator)) {
		t.Fatal("unexpected test root")
	}
}
