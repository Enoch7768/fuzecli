package diagnostics

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Kind string

const (
	Compile Kind = "compile"
	Test    Kind = "test"
	Runtime Kind = "runtime"
	Unknown Kind = "unknown"
)

type Diagnostic struct {
	Kind     Kind
	Stage    string
	Severity string
	ExitCode int
	File     string
	Line     int
	Column   int
	Message  string
	Raw      string
}

var (
	colonLocationPattern = regexp.MustCompile(`^(.+?):([0-9]+)(?::([0-9]+))?:\s*(.+)$`)
	tscLocationPattern = regexp.MustCompile(`^(.+?)\(([0-9]+),([0-9]+)\):\s*(error|warning|message)\s+([^:]+):\s*(.*)$`)
	eslintLocationPattern = regexp.MustCompile(`^\s*([0-9]+):([0-9]+)\s+(error|warning)\s+(.+?)\s+([\w-]+)\s*$`)
	pythonFramePattern = regexp.MustCompile(`^File "(.+)", line ([0-9]+)(?:, in .*)?$`)
	goTestPattern = regexp.MustCompile(`^--- (FAIL|PASS):\s+([^\s]+)`)
)

func Parse(output string) []Diagnostic {
	var result []Diagnostic
	seen := map[string]struct{}{}
	currentFile := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		if looksLikeDiagnosticFilename(raw) {
			currentFile = raw
			continue
		}
		d := parseLine(raw)
		if m := eslintLocationPattern.FindStringSubmatch(raw); len(m) == 6 && currentFile != "" {
			d.File = currentFile
			d.Line = parsePositiveInt(m[1])
			d.Column = parsePositiveInt(m[2])
			d.Severity = m[3]
			d.Message = strings.TrimSpace(m[4] + " (" + m[5] + ")")
			d.Kind = Compile
			d.Stage = stageFor(d.Kind)
		}
		key := fmt.Sprintf("%s:%s:%d:%d:%s", d.Kind, d.File, d.Line, d.Column, d.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, d)
	}
	return result
}

func looksLikeDiagnosticFilename(line string) bool {
	if strings.ContainsAny(line, " :") || strings.HasPrefix(line, "---") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(line))
	switch ext {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".py", ".php", ".rs", ".java", ".cs", ".cpp", ".c", ".h", ".rb":
		return true
	default:
		return false
	}
}

func parseLine(line string) Diagnostic {
	kind := classify(line)
	d := Diagnostic{Message: line, Raw: line, Kind: kind, Stage: stageFor(kind), Severity: severityFor(kind)}

	if m := colonLocationPattern.FindStringSubmatch(line); len(m) == 5 {
		d.File = m[1]
		d.Line = parsePositiveInt(m[2])
		d.Column = parsePositiveInt(m[3])
		d.Message = m[4]
		d.Kind = classify(d.Message + " " + line)
	} else if m := tscLocationPattern.FindStringSubmatch(line); len(m) == 7 {
		d.File = m[1]
		d.Line = parsePositiveInt(m[2])
		d.Column = parsePositiveInt(m[3])
		d.Severity = m[4]
		d.Message = strings.TrimSpace(m[5] + ": " + m[6])
		d.Kind = Compile
	} else if m := pythonFramePattern.FindStringSubmatch(line); len(m) == 3 {
		d.File = m[1]
		d.Line = parsePositiveInt(m[2])
		d.Kind = Runtime
	}
	d.Stage = stageFor(d.Kind)
	if d.Severity == "" || d.Severity == "info" {
		d.Severity = severityFor(d.Kind)
	}
	return d
}

func parsePositiveInt(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func stageFor(kind Kind) string {
	switch kind {
	case Compile, Test:
		return "verify"
	case Runtime:
		return "runtime"
	default:
		return "unknown"
	}
}

func severityFor(kind Kind) string {
	switch kind {
	case Compile, Test, Runtime:
		return "error"
	default:
		return "info"
	}
}

func classify(s string) Kind {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "test failed"), strings.Contains(l, "--- fail"), strings.Contains(l, "failed tests"), strings.Contains(l, "fail: test"):
		return Test
	case strings.Contains(l, "panic"), strings.Contains(l, "exception"), strings.Contains(l, "traceback"), strings.Contains(l, "runtime error:"):
		return Runtime
	case strings.Contains(l, "error:"), strings.Contains(l, "error ts"), strings.Contains(l, "warning ts"), strings.Contains(l, "undefined"), strings.Contains(l, "cannot find"), strings.Contains(l, " is not assignable"), strings.Contains(l, "eslint"):
		return Compile
	default:
		return Unknown
	}
}

func Summary(results []Diagnostic) string {
	if len(results) == 0 {
		return "No structured diagnostics found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic(s) found:\n", len(results))
	for _, d := range results {
		location := d.File
		if d.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, d.Line)
			if d.Column > 0 {
				location = fmt.Sprintf("%s:%d", location, d.Column)
			}
		}
		fmt.Fprintf(&b, "- [%s] %s: %s\n", d.Kind, location, d.Message)
	}
	return strings.TrimSpace(b.String())
}
