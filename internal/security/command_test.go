package security

import "testing"

func TestAuthorizeAllowlistedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		risk Risk
	}{
		{"go", []string{"test", "./..."}, RiskLow},
		{"go", []string{"test", "-run", "^TestRead$", "./internal/agent"}, RiskLow},
		{"go", []string{"test", "-short", "./..."}, RiskLow},
		{"npm", []string{"test", "--", "tests/auth.test.ts", "-t", "login"}, RiskMedium},
		{"python", []string{"-m", "pytest", "tests/test_auth.py", "-k", "login"}, RiskMedium},
		{"go", []string{"vet", "./..."}, RiskLow},
		{"gofmt", []string{"-l", "."}, RiskLow},
		{"npm", []string{"test", "--", "--runInBand"}, RiskLow},
		{"npm", []string{"run", "test"}, RiskMedium},
		{"pnpm", []string{"run", "test"}, RiskMedium},
		{"yarn", []string{"test"}, RiskMedium},
		{"bun", []string{"run", "typecheck"}, RiskMedium},
		{"npx", []string{"--yes", "tsc", "--noEmit"}, RiskMedium},
		{"php", []string{"-l", "index.php"}, RiskLow},
		{"python", []string{"-m", "py_compile"}, RiskLow},
		{"python", []string{"-m", "compileall", "-q", "."}, RiskMedium},
	}

	for _, tt := range tests {
		d := Authorize(Command{Name: tt.name, Args: tt.args})
		if !d.Allowed {
			t.Fatalf("%s unexpectedly blocked: %s", tt.name, d.Reason)
		}
		if d.Risk != tt.risk {
			t.Fatalf("%s risk = %s, want %s", tt.name, d.Risk, tt.risk)
		}
	}
}

func TestAuthorizeBlocksUnapprovedCommands(t *testing.T) {
	for _, command := range []Command{
		{Name: "rm", Args: []string{"-rf", "."}},
		{Name: "sh", Args: []string{"-c", "go test ./..."}},
		{Name: "go", Args: []string{"run", "script.go"}},
		{Name: "npm", Args: []string{"install", "package"}},
	} {
		d := Authorize(command)
		if d.Allowed {
			t.Fatalf("command unexpectedly allowed: %s %v", command.Name, command.Args)
		}
	}
}

func TestAuthorizeRejectsShellMetacharacters(t *testing.T) {
	d := Authorize(Command{Name: "php", Args: []string{"-l", "index.php; echo unsafe"}})
	if d.Allowed {
		t.Fatal("expected shell metacharacters to be rejected")
	}
}

func TestAuthorizeManagedDevelopmentProcesses(t *testing.T) {
	for _, command := range []Command{
		{Name:"npm", Args:[]string{"run","dev"}},
		{Name:"pnpm", Args:[]string{"run","start"}},
		{Name:"node", Args:[]string{"server.js"}},
		{Name:"python", Args:[]string{"server.py"}},
		{Name:"go", Args:[]string{"run","."}},
		{Name:"php", Args:[]string{"-S","127.0.0.1:8080","-t","public"}},
		{Name:"python3", Args:[]string{"-m","http.server","8080","--bind","127.0.0.1"}},
	} {
		if decision := AuthorizeManagedProcess(command); !decision.Allowed { t.Errorf("%s %v should be allowlisted: %s",command.Name,command.Args,decision.Reason) }
	}
	for _, command := range []Command{
		{Name:"node", Args:[]string{"../../evil.js"}},
		{Name:"python", Args:[]string{"-c","import os; os.remove('important')"}},
		{Name:"go", Args:[]string{"run","../outside.go"}},
		{Name:"php", Args:[]string{"-S","0.0.0.0:8080"}},
		{Name:"sh", Args:[]string{"-c","npm run dev"}},
	} {
		if decision := AuthorizeManagedProcess(command); decision.Allowed { t.Errorf("%s %v should be blocked",command.Name,command.Args) }
	}
}
