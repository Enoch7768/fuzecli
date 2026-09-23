package security

import (
	"fmt"
	"strings"
)

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
)

type Command struct {
	Name string
	Args []string
}

type Decision struct {
	Allowed bool
	Risk    Risk
	Reason  string
}

func Authorize(command Command) Decision {
	name := strings.ToLower(strings.TrimSpace(command.Name))
	args := append([]string(nil), command.Args...)

	switch name {
	case "go":
		if exact(args, "test", "./...") {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go test is an allowlisted verification command."}
		}
	case "npm":
		if exact(args, "test", "--", "--runInBand") {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "npm test is an allowlisted verification command."}
		}
	case "npx":
		if exact(args, "--yes", "tsc", "--noEmit") {
			return Decision{Allowed: true, Risk: RiskMedium, Reason: "TypeScript compilation is an allowlisted verification command; npx may resolve a package."}
		}
	case "php":
		if len(args) == 2 && args[0] == "-l" && args[1] != "" && !containsShellMeta(args[1]) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "PHP syntax lint is an allowlisted verification command."}
		}
	case "python":
		if len(args) == 2 && args[0] == "-m" && args[1] == "py_compile" {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Python compilation is an allowlisted verification command."}
		}
	}

	return Decision{Allowed: false, Risk: RiskMedium, Reason: fmt.Sprintf("command is not allowlisted: %s %s", name, strings.Join(args, " "))}
}

func exact(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsShellMeta(value string) bool {
	return strings.ContainsAny(value, "&|;<>$")
}
