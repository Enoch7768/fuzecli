package agent

import (
	"context"
	"errors"
	"fmt"
)

type Phase string

const (
	PhasePlan      Phase = "plan"
	PhaseContext   Phase = "context"
	PhaseEdit      Phase = "edit"
	PhaseVerify   Phase = "verify"
	PhaseDiagnose Phase = "diagnose"
	PhaseRepair   Phase = "repair"
	PhaseComplete Phase = "complete"
	PhaseFailed   Phase = "failed"
)

type Event struct {
	Phase   Phase
	Message string
	Attempt int
}

type StepFunc func(context.Context) error

type Loop struct {
	MaxRepairAttempts int
	Plan              StepFunc
	Context           StepFunc
	Edit              StepFunc
	Verify            StepFunc
	Diagnose          StepFunc
	Repair            StepFunc
	OnEvent           func(Event)
}

func (l *Loop) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if l.MaxRepairAttempts < 0 {
		return errors.New("max repair attempts cannot be negative")
	}

	if err := l.step(ctx, PhasePlan, 0, l.Plan); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	if err := l.step(ctx, PhaseContext, 0, l.Context); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if err := l.step(ctx, PhaseEdit, 0, l.Edit); err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	for attempt := 0; ; attempt++ {
		if err := l.step(ctx, PhaseVerify, attempt, l.Verify); err == nil {
			l.emit(Event{Phase: PhaseComplete, Attempt: attempt, Message: "agent task verified"})
			return nil
		} else if attempt >= l.MaxRepairAttempts {
			l.emit(Event{Phase: PhaseFailed, Attempt: attempt, Message: "verification failed and repair budget is exhausted"})
			return fmt.Errorf("verify failed after %d repair attempts", attempt)
		}

		if err := l.step(ctx, PhaseDiagnose, attempt, l.Diagnose); err != nil {
			return fmt.Errorf("diagnose: %w", err)
		}
		if err := l.step(ctx, PhaseRepair, attempt+1, l.Repair); err != nil {
			return fmt.Errorf("repair attempt %d: %w", attempt+1, err)
		}
	}
}

func (l *Loop) step(ctx context.Context, phase Phase, attempt int, fn StepFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.emit(Event{Phase: phase, Attempt: attempt, Message: string(phase) + " started"})
	if fn == nil {
		return nil
	}
	if err := fn(ctx); err != nil {
		return err
	}
	l.emit(Event{Phase: phase, Attempt: attempt, Message: string(phase) + " completed"})
	return nil
}

func (l *Loop) emit(event Event) {
	if l.OnEvent != nil {
		l.OnEvent(event)
	}
}
