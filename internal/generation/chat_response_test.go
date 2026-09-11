package generation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseChatPlanAcceptsLineRangesWithoutAction(t *testing.T) {
	raw := `{"files":[{"path":"script.js","line_start":1,"line_end":1000,"content":"const cart = [];"}],"explanation":"updated cart logic","commands":[]}`
	plan, err := ParseChatPlan(raw)
	if err != nil {
		t.Fatalf("ParseChatPlan returned error: %v", err)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("expected one file, got %d", len(plan.Files))
	}
	if plan.Files[0].Path != "script.js" || plan.Files[0].Content != "const cart = [];" {
		t.Fatalf("unexpected file: %#v", plan.Files[0])
	}
}

func TestApplyChatPlanInfersCreateAndModify(t *testing.T) {
	root := t.TempDir()
	createPlan := Plan{Files: []FileChange{{Path: "script.js", Content: "const cart = [];"}}}
	written, err := ApplyChatPlan(root, createPlan)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if len(written) != 1 || written[0] != "script.js" {
		t.Fatalf("unexpected written files: %#v", written)
	}
	path := filepath.Join(root, "script.js")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(b) != "const cart = [];" {
		t.Fatalf("created content = %q", string(b))
	}
	modifyPlan := Plan{Files: []FileChange{{Path: "script.js", Content: "const cart = [1, 2];"}}}
	if _, err := ApplyChatPlan(root, modifyPlan); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	if string(b) != "const cart = [1, 2];" {
		t.Fatalf("modified content = %q", string(b))
	}
}
