package app

import (
	"bytes"
	"fmt"
	"os/exec"
)

func GitStatus(root string) error {
	out, err := runGit(root, "status", "--short", "--branch")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		fmt.Println("Git: clean")
		return nil
	}
	fmt.Print(string(out))
	return nil
}

func GitDiff(root string) error {
	out, err := runGit(root, "diff", "--")
	if err != nil {
		return err
	}
	if len(out) == 0 {
		fmt.Println("Git: no unstaged diff")
		return nil
	}
	fmt.Print(string(out))
	return nil
}

func runGit(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w\n%s", args, err, bytes.TrimSpace(out))
	}
	return out, nil
}
