package agent

import (
	"time"

	"github.com/Enoch7768/fuzecli/internal/diagnostics"
)

type TaskStage string

const (
	StageUnderstand TaskStage = "understand"
	StagePlan       TaskStage = "plan"
	StageInspect    TaskStage = "inspect"
	StageEdit       TaskStage = "edit"
	StageFormat     TaskStage = "format"
	StageVerify     TaskStage = "verify"
	StageDiagnose   TaskStage = "diagnose"
	StageRepair     TaskStage = "repair"
	StageSummarize  TaskStage = "summarize"
)

type Task struct {
	ID           string
	Prompt       string
	Stage        TaskStage
	StartedAt    time.Time
	FinishedAt   time.Time
	ChangedFiles []string
	Diagnostics  []diagnostics.Diagnostic
	Attempts     int
	Verified     bool
}

type TaskEvent struct {
	TaskID    string
	Stage     TaskStage
	Message   string
	Attempt   int
	Timestamp time.Time
}

func (t *Task) Begin(id, prompt string) {
	t.ID = id
	t.Prompt = prompt
	t.Stage = StageUnderstand
	t.StartedAt = time.Now().UTC()
}

func (t *Task) Finish(verified bool) {
	t.Verified = verified
	t.FinishedAt = time.Now().UTC()
	t.Stage = StageSummarize
}
