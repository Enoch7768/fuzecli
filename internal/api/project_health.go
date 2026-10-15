package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type projectHealthCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type projectHealthResponse struct {
	Workspace string               `json:"workspace"`
	Overall   string               `json:"overall"`
	Manifests []string             `json:"manifests"`
	Checks    []projectHealthCheck `json:"checks"`
}

func (s *Server) projectHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.service == nil || strings.TrimSpace(s.service.Root()) == "" {
		writeError(w, http.StatusServiceUnavailable, "workspace is not initialized")
		return
	}
	root := s.service.Root()
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "workspace directory is unavailable")
		return
	}

	manifestNames := []string{
		"go.mod", "package.json", "composer.json", "pyproject.toml",
		"requirements.txt", "Pipfile", "Cargo.toml", "pom.xml",
		"build.gradle", "build.gradle.kts", "Makefile",
	}
	manifests := make([]string, 0, 4)
	present := make(map[string]bool, len(manifestNames))
	for _, name := range manifestNames {
		if fileExists(filepath.Join(root, name)) {
			manifests = append(manifests, name)
			present[name] = true
		}
	}
	sort.Strings(manifests)

	checks := []projectHealthCheck{
		{ID: "workspace", Label: "Workspace", Status: "ok", Detail: "Workspace directory is accessible."},
	}
	if fileExists(filepath.Join(root, ".git")) {
		checks = append(checks, projectHealthCheck{ID: "git", Label: "Git repository", Status: "ok", Detail: "Git metadata is present."})
	} else {
		checks = append(checks, projectHealthCheck{ID: "git", Label: "Git repository", Status: "info", Detail: "No .git directory or worktree marker was found at the workspace root."})
	}
	if len(manifests) > 0 {
		checks = append(checks, projectHealthCheck{ID: "manifest", Label: "Project configuration", Status: "ok", Detail: "Detected: " + strings.Join(manifests, ", ")})
	} else {
		checks = append(checks, projectHealthCheck{ID: "manifest", Label: "Project configuration", Status: "attention", Detail: "No common project manifest was found at the workspace root."})
	}

	type runtimeRequirement struct {
		id       string
		label    string
		commands []string
		required bool
	}
	requirements := []runtimeRequirement{
		{id: "go", label: "Go toolchain", commands: []string{"go"}, required: present["go.mod"]},
		{id: "node", label: "Node.js", commands: []string{"node"}, required: present["package.json"]},
		{id: "npm", label: "npm", commands: []string{"npm"}, required: present["package.json"]},
		{id: "php", label: "PHP", commands: []string{"php"}, required: present["composer.json"]},
		{id: "python", label: "Python", commands: []string{"python", "python3"}, required: present["pyproject.toml"] || present["requirements.txt"] || present["Pipfile"]},
		{id: "cargo", label: "Rust toolchain", commands: []string{"cargo"}, required: present["Cargo.toml"]},
		{id: "java", label: "Java", commands: []string{"java"}, required: present["pom.xml"] || present["build.gradle"] || present["build.gradle.kts"]},
		{id: "make", label: "Make", commands: []string{"make"}, required: present["Makefile"]},
	}
	for _, requirement := range requirements {
		if !requirement.required {
			continue
		}
		found := ""
		for _, command := range requirement.commands {
			if path, lookErr := exec.LookPath(command); lookErr == nil {
				found = path
				break
			}
		}
		if found != "" {
			checks = append(checks, projectHealthCheck{ID: requirement.id, Label: requirement.label, Status: "ok", Detail: "Available at " + found})
		} else {
			checks = append(checks, projectHealthCheck{ID: requirement.id, Label: requirement.label, Status: "attention", Detail: "Required command not found on PATH."})
		}
	}

	testCount, testErr := countProjectTests(root)
	if testErr != nil {
		checks = append(checks, projectHealthCheck{ID: "tests", Label: "Test files", Status: "info", Detail: "Could not scan test files: " + testErr.Error()})
	} else if testCount > 0 {
		checks = append(checks, projectHealthCheck{ID: "tests", Label: "Test files", Status: "ok", Detail: "Found " + itoa(testCount) + " likely test file(s). Tests were not executed."})
	} else {
		checks = append(checks, projectHealthCheck{ID: "tests", Label: "Test files", Status: "info", Detail: "No common test-file naming patterns were found. Tests were not executed."})
	}

	overall := "healthy"
	for _, check := range checks {
		if check.Status == "attention" {
			overall = "attention"
			break
		}
	}
	writeJSON(w, http.StatusOK, projectHealthResponse{
		Workspace: root,
		Overall:   overall,
		Manifests: manifests,
		Checks:    checks,
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countProjectTests(root string) (int, error) {
	count := 0
	visited := 0
	skipDirs := map[string]bool{
		".git": true, ".aicli": true, "node_modules": true, "vendor": true,
		"dist": true, "build": true, "target": true, ".venv": true, "venv": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if entry.IsDir() && path != root && skipDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		visited++
		if visited > 20000 {
			return filepath.SkipAll
		}
		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, ".test.js") ||
			strings.HasSuffix(name, ".spec.js") ||
			strings.HasSuffix(name, ".test.ts") ||
			strings.HasSuffix(name, ".spec.ts") ||
			strings.HasPrefix(name, "test_") ||
			strings.HasSuffix(name, "_test.py") ||
			strings.HasSuffix(name, "Test.java") {
			count++
		}
		return nil
	})
	return count, err
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	i := len(buffer)
	for value > 0 {
		i--
		buffer[i] = digits[value%10]
		value /= 10
	}
	return string(buffer[i:])
}
