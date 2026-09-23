package security

import "testing"

func TestAuthorizeAllowlistedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		risk Risk
	}{
		{"go", []string{"test", "./..."}, RiskLow},
		{"npm", []string{"test", "--", "--runInBand"}, RiskLow},
		{"npx", []string{"--yes", "tsc", "--noEmit"}, RiskMedium},
		{"php", []string{"-l", "index.php"}, RiskLow},
		{"python", []string{"-m", "py_compile"}, RiskLow},
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
