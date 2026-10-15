package diagnostics

import (
	"strings"
	"testing"
)

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

func TestParseTestFailure(t *testing.T) {
	got := Parse("--- FAIL: TestExample (0.00s)\n")
	if len(got) != 1 || got[0].Kind != Test {
		t.Fatalf("unexpected test diagnostic: %+v", got)
	}
}

func TestParseTypeScriptDiagnostic(t *testing.T) {
	got := Parse("src/app.ts(12,4): error TS2322: Type 'string' is not assignable to type 'number'.")
	if len(got) != 1 {
		t.Fatalf("expected one TypeScript diagnostic, got %+v", got)
	}
	d := got[0]
	if d.File != "src/app.ts" || d.Line != 12 || d.Column != 4 || d.Kind != Compile {
		t.Fatalf("unexpected TypeScript diagnostic: %+v", d)
	}
	if !strings.Contains(d.Message, "TS2322") {
		t.Fatalf("TypeScript error code missing from message: %q", d.Message)
	}
}

func TestParseESLintDiagnostic(t *testing.T) {
	got := Parse("src/app.js\n  7:3  error  'missing' is not defined  no-undef\n")
	if len(got) != 1 {
		t.Fatalf("expected one ESLint diagnostic, got %+v", got)
	}
	d := got[0]
	if d.File != "src/app.js" || d.Line != 7 || d.Column != 3 || d.Kind != Compile {
		t.Fatalf("unexpected ESLint diagnostic: %+v", d)
	}
	if !strings.Contains(d.Message, "no-undef") {
		t.Fatalf("ESLint rule missing from message: %q", d.Message)
	}
}

func TestParsePythonTracebackFrame(t *testing.T) {
	got := Parse("Traceback (most recent call last):\n  File \"app/main.py\", line 27, in run\nValueError: bad input\n")
	if len(got) != 3 {
		t.Fatalf("expected traceback, frame, and exception diagnostics, got %+v", got)
	}
	var frame *Diagnostic
	for i := range got {
		if got[i].File == "app/main.py" {
			frame = &got[i]
		}
	}
	if frame == nil || frame.Line != 27 || frame.Kind != Runtime {
		t.Fatalf("Python traceback frame was not located: %+v", got)
	}
}

func TestParseLongCompilerLine(t *testing.T) {
	line := "main.go:1:2: " + strings.Repeat("x", 100*1024)
	got := Parse(line)
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("long diagnostic was not parsed: count=%d", len(got))
	}
}
