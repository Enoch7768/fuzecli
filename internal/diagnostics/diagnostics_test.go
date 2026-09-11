package diagnostics

import "testing"

func TestParse(t *testing.T) {
	got := Parse("internal/app/app.go:42:7: undefined: MissingThing\ninternal/app/app.go:42:7: undefined: MissingThing\n")
	if len(got) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(got))
	}
	if got[0].File != "internal/app/app.go" || got[0].Line != 42 || got[0].Column != 7 {
		t.Fatalf("unexpected location: %+v", got[0])
	}
	if got[0].Kind != Compile {
		t.Fatalf("expected compile diagnostic, got %q", got[0].Kind)
	}
}

func TestSummaryEmpty(t *testing.T) {
	if Summary(nil) != "No structured diagnostics found." {
		t.Fatal("unexpected empty summary")
	}
}
