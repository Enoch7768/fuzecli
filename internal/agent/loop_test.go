package agent

import (
	"context"
	"errors"
	"testing"
)

func TestLoopCompletesWithoutRepair(t *testing.T) {
	var phases []Phase
	calls := 0

	l := Loop{
		MaxRepairAttempts: 2,
		Plan:              func(context.Context) error { return nil },
		Context:           func(context.Context) error { return nil },
		Edit:              func(context.Context) error { return nil },
		Verify:            func(context.Context) error { return nil },
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

	_ = calls
}

func TestLoopRepairsAndReverifies(t *testing.T) {
	verifyCalls := 0
	repairCalls := 0

	l := Loop{
		MaxRepairAttempts: 2,
		Plan:              func(context.Context) error { return nil },
		Context:           func(context.Context) error { return nil },
		Edit:              func(context.Context) error { return nil },
		Verify: func(context.Context) error {
			verifyCalls++
			if verifyCalls == 1 {
				return errors.New("tests failed")
			}
			return nil
		},
		Diagnose: func(context.Context) error { return nil },
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
}

func TestLoopStopsWhenRepairBudgetIsExhausted(t *testing.T) {
	verifyCalls := 0
	repairCalls := 0

	l := Loop{
		MaxRepairAttempts: 1,
		Plan:              func(context.Context) error { return nil },
		Context:           func(context.Context) error { return nil },
		Edit:              func(context.Context) error { return nil },
		Verify: func(context.Context) error {
			verifyCalls++
			return errors.New("verification failed")
		},
		Diagnose: func(context.Context) error { return nil },
		Repair: func(context.Context) error {
			repairCalls++
			return nil
		},
	}

	if err := l.Run(context.Background()); err == nil {
		t.Fatal("expected verification failure")
	}
	if verifyCalls != 2 {
		t.Fatalf("unexpected verify calls: got %d want 2", verifyCalls)
	}
	if repairCalls != 1 {
		t.Fatalf("unexpected repair calls: got %d want 1", repairCalls)
	}
}

func TestLoopPropagatesStepFailure(t *testing.T) {
	want := errors.New("context unavailable")

	l := Loop{
		Plan:    func(context.Context) error { return nil },
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
	l := Loop{
		Plan: func(context.Context) error {
			called = true
			return nil
		},
	}

	if err := l.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if called {
		t.Fatal("plan step ran after cancellation")
	}
}
