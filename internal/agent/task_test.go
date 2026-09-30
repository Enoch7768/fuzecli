package agent

import "testing"

func TestTaskLifecycle(t *testing.T) {
	var task Task
	task.Begin("task-1", "fix the build")
	if task.Stage != StageUnderstand || task.StartedAt.IsZero() {
		t.Fatal("task did not begin in understand stage")
	}
	task.Finish(true)
	if task.Stage != StageSummarize || !task.Verified || task.FinishedAt.IsZero() {
		t.Fatal("task did not finish correctly")
	}
}
