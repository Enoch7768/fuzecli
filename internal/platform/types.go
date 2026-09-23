package platform

import "time"

type Mode string

const (
 ModeAsk Mode = "ask"
 ModePlan Mode = "plan"
 ModeAgent Mode = "agent"
 ModeAutopilot Mode = "autopilot"
)

type ProjectMemory struct {
 Architecture string `json:"architecture,omitempty"`
 Decisions []string `json:"decisions,omitempty"`
 Conventions []string `json:"conventions,omitempty"`
 Dependencies []string `json:"dependencies,omitempty"`
 KnownIssues []string `json:"known_issues,omitempty"`
 Terminology []string `json:"terminology,omitempty"`
 UpdatedAt time.Time `json:"updated_at"`
}
type Rule struct { Name string `json:"name"`; Content string `json:"content"` }
type Skill struct { Name string `json:"name"`; Path string `json:"path"`; Content string `json:"content"` }
type Workspace struct { Name string `json:"name"`; Root string `json:"root"` }
type GraphNode struct { Path string `json:"path"`; Language string `json:"language,omitempty"`; Symbols []string `json:"symbols,omitempty"`; Imports []string `json:"imports,omitempty"` }
type GraphEdge struct { From string `json:"from"`; To string `json:"to"`; Kind string `json:"kind"` }
type ContextGraph struct { Nodes []GraphNode `json:"nodes"`; Edges []GraphEdge `json:"edges"` }
type JobStatus string
const ( JobQueued JobStatus = "queued"; JobRunning JobStatus = "running"; JobSucceeded JobStatus = "succeeded"; JobFailed JobStatus = "failed"; JobCancelled JobStatus = "cancelled" )
type BackgroundJob struct { ID string `json:"id"`; Prompt string `json:"prompt"`; Status JobStatus `json:"status"`; Progress []string `json:"progress,omitempty"`; Result string `json:"result,omitempty"`; Error string `json:"error,omitempty"`; StartedAt time.Time `json:"started_at"`; FinishedAt *time.Time `json:"finished_at,omitempty"` }
type Proof struct { ID string `json:"id"`; Goal string `json:"goal"`; Provider string `json:"provider,omitempty"`; Model string `json:"model,omitempty"`; ChangedFiles []string `json:"changed_files,omitempty"`; TestsPassed bool `json:"tests_passed"`; BuildPassed bool `json:"build_passed"`; SecurityPassed bool `json:"security_passed"`; VisualPassed bool `json:"visual_passed"`; AccessibilityPassed bool `json:"accessibility_passed"`; Notes []string `json:"notes,omitempty"`; CreatedAt time.Time `json:"created_at"` }
type ReviewFinding struct { Severity string `json:"severity"`; Path string `json:"path,omitempty"`; Line int `json:"line,omitempty"`; Message string `json:"message"` }
type Review struct { Summary string `json:"summary"`; Findings []ReviewFinding `json:"findings,omitempty"`; Passed bool `json:"passed"` }
type BenchmarkResult struct { Task string `json:"task"`; Provider string `json:"provider"`; Model string `json:"model"`; Success bool `json:"success"`; Latency time.Duration `json:"latency"`; Tokens uint64 `json:"tokens"` }
