package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLoopCompletesWithoutRepair(t *testing.T) {
	var phases []Phase
	l := Loop{
		MaxRepairAttempts: 2,
		Plan: func(context.Context) error { return nil },
		Context: func(context.Context) error { return nil },
		Edit: func(context.Context) error { return nil },
		Verify: func(context.Context) error { return nil },
		OnEvent: func(event Event) {
			if event.Message == string(event.Phase)+" started" {
				phases = append(phases, event.Phase)
			}
		},
	}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expected := []Phase{PhasePlan, PhaseContext, PhaseEdit, PhaseVerify, PhaseComplete}
	if len(phases) != len(expected)-1 {
		t.Fatalf("unexpected phase count: got %d want %d", len(phases), len(expected)-1)
	}
}

func TestLoopRepairsAndReverifies(t *testing.T) {
	verifyCalls := 0
	repairCalls := 0
	var l Loop
	l = Loop{
		MaxRepairAttempts: 2,
		Plan: func(context.Context) error { return nil },
		Context: func(context.Context) error { return nil },
		Edit: func(context.Context) error { return nil },
		Verify: func(context.Context) error {
			verifyCalls++
			if verifyCalls == 1 {
				return errors.New("tests failed")
			}
			return nil
		},
		Diagnose: func(context.Context) error {
			if l.VerificationError != "tests failed" {
				t.Errorf("diagnose received wrong verification error: %q", l.VerificationError)
			}
			return nil
		},
		Repair: func(context.Context) error {
			repairCalls++
			return nil
		},
	}
	if err := l.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if verifyCalls != 2 {
		t.Fatalf("unexpected verify calls: got %d want 2", verifyCalls)
	}
	if repairCalls != 1 {
		t.Fatalf("unexpected repair calls: got %d want 1", repairCalls)
	}
	if l.VerificationError != "" {
		t.Fatalf("verification error was not cleared after success: %q", l.VerificationError)
	}
}

func TestLoopStopsWhenRepairBudgetIsExhausted(t *testing.T) {
	verifyCalls := 0
	repairCalls := 0
	l := Loop{
		MaxRepairAttempts: 1,
		Plan: func(context.Context) error { return nil },
		Context: func(context.Context) error { return nil },
		Edit: func(context.Context) error { return nil },
		Verify: func(context.Context) error {
			verifyCalls++
			return errors.New("verification failed: missing symbol")
		},
		Diagnose: func(context.Context) error { return nil },
		Repair: func(context.Context) error { repairCalls++; return nil },
	}
	err := l.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing symbol") {
		t.Fatalf("expected detailed verification failure, got %v", err)
	}
	if l.VerificationError != "verification failed: missing symbol" {
		t.Fatalf("verification error was not retained: %q", l.VerificationError)
	}
	if verifyCalls != 2 {
		t.Fatalf("unexpected verify calls: got %d want 2", verifyCalls)
	}
	if repairCalls != 1 {
		t.Fatalf("unexpected repair calls: got %d want 1", repairCalls)
	}
}

func TestLoopEmitsStructuredVerificationFailure(t *testing.T) {
	var failed *Event
	l := Loop{
		Verify: func(context.Context) error { return errors.New("compiler exited with code 1") },
		OnEvent: func(event Event) {
			if event.Phase == PhaseVerify && event.Error != "" {
				copy := event
				failed = &copy
			}
		},
	}
	if err := l.Run(context.Background()); err == nil {
		t.Fatal("expected verification failure")
	}
	if failed == nil {
		t.Fatal("expected structured verification failure event")
	}
	if failed.Error != "compiler exited with code 1" || failed.Attempt != 0 {
		t.Fatalf("unexpected failure event: %+v", *failed)
	}
}

func TestLoopPropagatesStepFailure(t *testing.T) {
	want := errors.New("context unavailable")
	l := Loop{
		Plan: func(context.Context) error { return nil },
		Context: func(context.Context) error { return want },
	}
	err := l.Run(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped context error, got %v", err)
	}
}

func TestLoopHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	l := Loop{Plan: func(context.Context) error { called = true; return nil }}
	if err := l.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if called {
		t.Fatal("plan step ran after cancellation")
	}
}
