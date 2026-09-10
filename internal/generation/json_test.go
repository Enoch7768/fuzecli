package generation

import (
	"strings"
	"testing"
)

func TestParsePlanAcceptsFencedJSONAndTrailingText(t *testing.T) {
	raw := "Here is the result:\n```json\n{\"files\":[{\"path\":\"index.html\",\"content\":\"hello\",\"action\":\"create\"}],\"explanation\":\"done\",\"commands\":[]}\n```\nCompleted."
	plan, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("ParsePlan returned error: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != "index.html" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestParsePlanDetectsTruncatedJSON(t *testing.T) {
	raw := `{"files":[{"path":"index.html","content":"<html>`
	_, err := ParsePlan(raw)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "incomplete") {
		t.Fatalf("expected incomplete JSON error, got %v", err)
	}
}

func TestNextBatchRespectsDependencies(t *testing.T) {
	plan := ProjectPlan{Files: []PlannedFile{
		{Path: "app.go", Status: "pending"},
		{Path: "config.go", Dependencies: []string{"app.go"}, Status: "pending"},
		{Path: "index.html", Status: "pending"},
	}}
	batch := NextBatch(plan)
	if len(batch) != 2 || batch[0].Path != "app.go" || batch[1].Path != "index.html" {
		t.Fatalf("unexpected dependency-aware batch: %#v", batch)
	}
}
