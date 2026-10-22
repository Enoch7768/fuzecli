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
		if exact(args, "test", "./...") || exact(args, "vet", "./...") || exact(args, "build", "./...") || exact(args, "test", "-run", "^$", "./...") || safeGoPackageCommand(args) {
			return Decision{Allowed: true, Risk: RiskLow, Reason: "Go test and vet are allowlisted verification commands."}
		}
	case "gofmt":
		if exact(args, "-l", ".") || safeFormatFileList(args) {
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

func safeGoPackageCommand(args []string) bool {
	if len(args)<2{return false}
	command:=args[0];packages:=args[1:]
	if command=="test"&&len(args)>=4&&args[1]=="-run"&&args[2]=="^$"{packages=args[3:]}
	if command!="test"&&command!="vet"&&command!="build"{return false}
	if len(packages)==0{return false}
	for _,pkg:=range packages{if !safeRelativePath(pkg)||!strings.HasPrefix(pkg,"./"){return false}}
	return true
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
