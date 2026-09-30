package security

import "testing"

func TestSafeCapabilities(t *testing.T) {
	caps := SafeCapabilities()
	if !caps.Allows(ReadWorkspace) || !caps.Allows(WriteWorkspace) {
		t.Fatal("safe capability profile should allow workspace read/write")
	}
	if caps.Allows(NetworkAccess) || caps.Allows(InstallDependency) || caps.Allows(DeleteFile) {
		t.Fatal("safe capability profile must not grant elevated capabilities")
	}
}

func TestValidateWorkspacePath(t *testing.T) {
	for _, path := range []string{"../secret", "/tmp/secret", "C:\secret", "src/../secret"} {
		if err := ValidateWorkspacePath("workspace", path, false); err == nil {
			t.Fatalf("expected path rejection for %q", path)
		}
	}
	if err := ValidateWorkspacePath("workspace", "src/main.go", false); err != nil {
		t.Fatal(err)
	}
}
