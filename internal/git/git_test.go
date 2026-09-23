package git

import "testing"

func TestParseStatus(t *testing.T) {
	got := parseStatus("## feature/test...origin/feature/test\n M internal/app/app.go\nA  internal/git/git.go\n?? notes.txt\n")
	if got.Branch != "feature/test...origin/feature/test" {
		t.Fatalf("unexpected branch: %q", got.Branch)
	}
	if got.Clean {
		t.Fatal("expected dirty status")
	}
	if len(got.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(got.Files))
	}
	if got.Files[0].Path != "internal/app/app.go" || got.Files[0].Worktree != 'M' {
		t.Fatalf("unexpected first status: %+v", got.Files[0])
	}
	if got.Files[1].Path != "internal/git/git.go" || got.Files[1].Index != 'A' {
		t.Fatalf("unexpected second status: %+v", got.Files[1])
	}
}

func TestParseRename(t *testing.T) {
	got := parseStatus("## main\nR  old/name.go -> new/name.go\n")
	if len(got.Files) != 1 {
		t.Fatalf("expected one file, got %d", len(got.Files))
	}
	if got.Files[0].Original != "old/name.go" || got.Files[0].Path != "new/name.go" {
		t.Fatalf("unexpected rename: %+v", got.Files[0])
	}
}

func TestParseClean(t *testing.T) {
	got := parseStatus("## main\n")
	if !got.Clean {
		t.Fatal("expected clean status")
	}
}
