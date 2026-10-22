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
		if exact(args, "test", "./...") || exact(args, "vet", "./...") || exact(args, "build", "./...") || exact(args, "test", "-run", "^$", "./...") {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go test and vet are allowlisted verification commands."}
		}
	case "gofmt":
		if exact(args, "-l", ".") {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go formatting inspection is an allowlisted verification command."}
		}
	case "npm", "pnpm", "bun":
		if exact(args, "run", "test") || exact(args, "run", "typecheck") || exact(args, "run", "build") || exact(args, "run", "lint") || exact(args, "run", "format:check") {
			return Decision{Allowed: true, Risk: RiskMedium, Reason: "Declared project verification script is allowlisted."}
		}
		if name == "npm" && (exact(args, "test") || exact(args, "test", "--", "--runInBand")) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "npm test is an allowlisted verification command."}
		}
	case "yarn":
		if exact(args, "test") || exact(args, "typecheck") || exact(args, "build") || exact(args, "lint") || exact(args, "format:check") {
			return Decision{Allowed: true, Risk: RiskMedium, Reason: "Declared project verification script is allowlisted."}
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
