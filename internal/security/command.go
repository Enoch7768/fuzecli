package security

import (
	"fmt"
	"strconv"
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
		if exact(args, "test", "./...") || exact(args, "vet", "./...") || exact(args, "build", "./...") || safeGoPackageCommand(args) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go test and vet are allowlisted verification commands."}
		}
	case "gofmt":
		if exact(args, "-l", ".") || safeFormatFileList(args) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go formatting inspection is an allowlisted verification command."}
		}
	case "npm", "pnpm", "bun":
		if name == "npm" && (exact(args, "test") || exact(args, "test", "--", "--runInBand")) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "npm test is an allowlisted verification command."}
		}
		if exact(args, "run", "test") || exact(args, "run", "typecheck") || exact(args, "run", "build") || exact(args, "run", "lint") || exact(args, "run", "format:check") || safeNPMTestCommand(name, args) {
			return Decision{Allowed: true, Risk: RiskMedium, Reason: "Declared project verification script is allowlisted."}
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
	case "python", "python3":
		if exact(args, "-m", "compileall", "-q", ".") { return Decision{Allowed:true, Risk:RiskMedium, Reason:"Python bytecode compilation over the workspace is an allowlisted build check."} }
		if len(args) == 2 && args[0] == "-m" && args[1] == "py_compile" {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Python compilation is an allowlisted verification command."}
		}
		if safePythonPytest(args) { return Decision{Allowed:true, Risk:RiskMedium, Reason:"Pytest with workspace-relative paths and bounded filter arguments is allowlisted."} }
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

func safeNPMTestCommand(name string, args []string) bool {
	if len(args) == 0 { return false }
	i := 0
	if args[0] == "test" { i = 1 } else if len(args) >= 2 && args[0] == "run" && strings.HasPrefix(args[1], "test") {
		for _, r := range args[1] { if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ':' || r == '_' || r == '-') { return false } }
		i = 2
	} else { return false }
	if i == len(args) { return true }
	if args[i] != "--" { return false }
	i++
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "-t", "-k":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" || strings.ContainsAny(args[i+1], "\\;&|<>$\x00\r\n") { return false }
			i += 2
		case "--runInBand", "--run":
			i++
		default:
			if strings.HasPrefix(arg, "-") || !safeRelativePath(arg) { return false }
			i++
		}
	}
	return true
}

func safeGoPackageCommand(args []string) bool {
	if len(args) < 2 { return false }
	command := args[0]
	if command != "test" && command != "vet" && command != "build" { return false }
	i := 1
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-short":
			if command != "test" { return false }; i++
		case "-run":
			if command != "test" || i+1 >= len(args) { return false }
			value := args[i+1]
			if value == "" || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n") { return false }
			i += 2
		case "-tags":
			if command != "test" || i+1 >= len(args) || !validSimpleTagList(args[i+1]) { return false }
			i += 2
		default:
			return false
		}
	}
	if i >= len(args) { return false }
	for _, pkg := range args[i:] { if !safeRelativePath(pkg) || !strings.HasPrefix(pkg, "./") { return false } }
	return true
}

func validSimpleTagList(value string) bool {
	for _, part := range strings.Split(value, ",") {
		if part == "" { return false }
		for _, r := range part { if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.') { return false } }
	}
	return true
}

func safePythonPytest(args []string) bool {
	if len(args) < 2 || args[0] != "-m" || args[1] != "pytest" { return false }
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if arg == "-k" {
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" || strings.ContainsAny(args[i+1], "\\;&|<>$\x00\r\n") { return false }
			i++; continue
		}
		if strings.HasPrefix(arg, "-") || !safeRelativePath(arg) { return false }
	}
	return true
}

// AuthorizeManagedProcess is a narrow allowlist for long-running local development
// servers. The caller must still obtain explicit user approval before starting them.
func AuthorizeManagedProcess(command Command) Decision {
	name := strings.ToLower(strings.TrimSpace(command.Name))
	args := append([]string(nil), command.Args...)
	allowed := false
	switch name {
	case "npm", "pnpm":
		allowed = exact(args, "run", "dev") || exact(args, "run", "start") || exact(args, "start") || exact(args, "run", "serve")
	case "bun":
		allowed = exact(args, "run", "dev") || exact(args, "run", "start")
	case "go":
		allowed = len(args) == 2 && args[0] == "run" && safeRelativePath(args[1]) && strings.HasPrefix(args[1], "./") || exact(args, "run", ".")
	case "node":
		allowed = len(args) == 1 && safeRelativePath(args[0]) && (strings.HasSuffix(strings.ToLower(args[0]), ".js") || strings.HasSuffix(strings.ToLower(args[0]), ".mjs") || strings.HasSuffix(strings.ToLower(args[0]), ".cjs"))
	case "python", "python3":
		if len(args) == 1 && safeRelativePath(args[0]) && strings.HasSuffix(strings.ToLower(args[0]), ".py") { allowed = true }
		if len(args) == 2 && args[0] == "-m" && args[1] == "http.server" { allowed = true }
		if len(args) >= 2 && args[0] == "-m" && args[1] == "http.server" {
			allowed = true
			for i := 2; i < len(args); i++ { arg := args[i]; if arg == "--bind" { i++; if i >= len(args) || (args[i] != "127.0.0.1" && args[i] != "localhost") { allowed = false }; continue }; port, err := strconv.Atoi(arg); if err != nil || port < 1 || port > 65535 { allowed = false } }
		}
	case "php":
		allowed = len(args) >= 2 && args[0] == "-S" && (strings.HasPrefix(args[1], "127.0.0.1:") || strings.HasPrefix(args[1], "localhost:"))
		if allowed { for _, arg := range args[2:] { if arg == "-t" { continue }; if !safeRelativePath(arg) { allowed = false } } }
	}
	if !allowed { return Decision{Allowed:false, Risk:RiskMedium, Reason:fmt.Sprintf("managed process is not allowlisted: %s %s", name, strings.Join(args, " "))} }
	return Decision{Allowed:true, Risk:RiskMedium, Reason:"Allowlisted local development process; explicit user approval is still required."}
}

func safeFormatFileList(args []string) bool {
	if len(args)<2||args[0]!="-l"{return false}
	for _,path:=range args[1:]{if !safeRelativePath(path){return false}}
	return true
}
func safeRelativePath(path string) bool {
	if strings.TrimSpace(path)==""||strings.HasPrefix(path,"/")||strings.ContainsAny(path,"\\;&|<>$"){return false}
	path=strings.ReplaceAll(path,"\\","/")
	for _,part:=range strings.Split(path,"/"){if part==".." {return false}}
	return true
}
