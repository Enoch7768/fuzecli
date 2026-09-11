package diagnostics

import (
	"bufio"
	"fmt"
	"regexp"
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
	Kind    Kind
	File    string
	Line    int
	Column  int
	Message string
	Raw     string
}

func Parse(output string) []Diagnostic {
	var result []Diagnostic
	seen := map[string]struct{}{}
	re := regexp.MustCompile(`^(.+?):([0-9]+)(?::([0-9]+))?:\s*(.+)$`)
	s := bufio.NewScanner(strings.NewReader(output))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		m := re.FindStringSubmatch(line)
		d := Diagnostic{Message: line, Raw: line, Kind: classify(line)}
		if len(m) == 5 {
			fmt.Sscanf(m[2], "%d", &d.Line)
			if m[3] != "" {
				fmt.Sscanf(m[3], "%d", &d.Column)
			}
			d.File = m[1]
			d.Message = m[4]
		}
		key := fmt.Sprintf("%s:%d:%d:%s", d.File, d.Line, d.Column, d.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, d)
	}
	return result
}

func classify(s string) Kind {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "test failed"), strings.Contains(l, "--- fail"), strings.Contains(l, "failed tests"):
		return Test
	case strings.Contains(l, "panic"), strings.Contains(l, "exception"), strings.Contains(l, "traceback"):
		return Runtime
	case strings.Contains(l, "error:"), strings.Contains(l, "undefined"), strings.Contains(l, "cannot find"):
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
