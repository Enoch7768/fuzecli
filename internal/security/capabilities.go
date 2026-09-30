package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type Capability string

const (
	ReadWorkspace     Capability = "read_workspace"
	WriteWorkspace    Capability = "write_workspace"
	RunTests          Capability = "run_tests"
	RunBuild          Capability = "run_build"
	RunGit            Capability = "run_git"
	NetworkAccess     Capability = "network_access"
	InstallDependency Capability = "install_dependency"
	DeleteFile        Capability = "delete_file"
)

type CapabilitySet map[Capability]bool

func SafeCapabilities() CapabilitySet {
	return CapabilitySet{
		ReadWorkspace:  true,
		WriteWorkspace: true,
		RunTests:       true,
		RunBuild:       true,
		RunGit:         true,
	}
}

func (c CapabilitySet) Allows(capability Capability) bool {
	return c != nil && c[capability]
}

func (c CapabilitySet) Require(capability Capability) error {
	if c.Allows(capability) {
		return nil
	}
	return fmt.Errorf("capability %q is not permitted", capability)
}

func ValidateWorkspacePath(root, relative string, allowDelete bool) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("workspace root is required")
	}
	if strings.TrimSpace(relative) == "" {
		return errors.New("workspace path is required")
	}
	normalized := filepath.ToSlash(relative)
	portable := strings.ReplaceAll(relative, "\\", "/")
	if filepath.IsAbs(relative) || filepath.IsAbs(portable) || strings.HasPrefix(normalized, "/") || strings.HasPrefix(portable, "/") || strings.HasPrefix(normalized, "../") || strings.HasPrefix(portable, "../") || strings.Contains(normalized, "/../") || strings.Contains(portable, "/../") || filepath.VolumeName(relative) != "" || windowsVolumeName(portable) != "" {
		return fmt.Errorf("workspace path escapes the workspace: %q", relative)
	}
	if strings.ContainsRune(relative, rune(0)) {
		return errors.New("workspace path contains a NUL byte")
	}
	if !allowDelete && strings.EqualFold(filepath.Base(relative), ".git") {
		return errors.New("protected workspace path")
	}
	return nil
}


func windowsVolumeName(path string) string {
	if len(path) >= 2 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' {
		return path[:2]
	}
	if strings.HasPrefix(path, "//") {
		return "//"
	}
	return ""
}
