package agent

import "testing"

func TestTraceBoundsAndCopies(t *testing.T) {
	trace := NewTrace(2)
	trace.Add(TaskEvent{TaskID: "1", Stage: StageUnderstand, Message: "a"})
	trace.Add(TaskEvent{TaskID: "1", Stage: StagePlan, Message: "b"})
	trace.Add(TaskEvent{TaskID: "1", Stage: StageVerify, Message: "c"})
	events := trace.Events()
	if len(events) != 2 || events[0].Message != "b" || events[1].Message != "c" {
		t.Fatalf("unexpected trace: %#v", events)
	}
	events[0].Message = "changed"
	if trace.Events()[0].Message != "b" {
		t.Fatal("trace returned internal storage")
	}
}
