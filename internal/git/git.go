package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type FileStatus struct {
	Index    byte   `json:"index"`
	Worktree byte   `json:"worktree"`
	Path     string `json:"path"`
	Original string `json:"original,omitempty"`
}

type Status struct {
	Branch string       `json:"branch"`
	Files  []FileStatus `json:"files"`
	Clean  bool         `json:"clean"`
}

func StatusOf(root string) (Status, error) {
	out, err := run(root, "status", "--porcelain=v1", "--branch")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(string(out)), nil
}

func Diff(root string, staged bool) (string, error) {
	if staged {
		out, err := run(root, "diff", "--cached", "--")
		return string(out), err
	}
	out, err := run(root, "diff", "--")
	return string(out), err
}

func parseStatus(output string) Status {
	result := Status{Clean: true}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			result.Branch = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if len(line) < 3 {
			continue
		}
		entry := FileStatus{Index: line[0], Worktree: line[1]}
		path := strings.TrimSpace(line[3:])
		if entry.Index == 'R' || entry.Worktree == 'R' || entry.Index == 'C' || entry.Worktree == 'C' {
			parts := strings.SplitN(path, " -> ", 2)
			if len(parts) == 2 {
				entry.Original = parts[0]
				entry.Path = parts[1]
			} else {
				entry.Path = path
			}
		} else {
			entry.Path = path
		}
		result.Files = append(result.Files, entry)
	}
	result.Clean = len(result.Files) == 0
	return result
}

func run(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return nil, fmt.Errorf("git %v: %w: %s", args, err, detail)
		}
		return nil, fmt.Errorf("git %v: %w", args, err)
	}
	return bytes.TrimSpace(out), nil
}
