package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fuzecli/internal/generation"
)

func Preview(plan generation.Plan, root string) {
	fmt.Println("\nChanges:")
	for _, f := range plan.Files {
		path := filepath.Join(root, filepath.FromSlash(f.Path))
		old := []byte{}
		if b, err := os.ReadFile(path); err == nil {
			old = b
		}
		fmt.Printf("\n--- %s (%s) ---\n", f.Path, f.Action)
		showDiff(string(old), f.Content, f.Action)
	}
	if plan.Explanation != "" {
		fmt.Printf("\nExplanation: %s\n", plan.Explanation)
	}
	if len(plan.Commands) > 0 {
		fmt.Println("Suggested commands:")
		for _, c := range plan.Commands {
			fmt.Printf("  %s\n", c)
		}
	}
}
func showDiff(old, new, action string) {
	if action == "delete" {
		for _, l := range strings.Split(old, "\n") {
			if l != "" {
				fmt.Printf("- %s\n", l)
			}
		}
		return
	}
	if old == new {
		fmt.Println("(no textual changes)")
		return
	}
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")
	max := len(oldLines)
	if len(newLines) > max {
		max = len(newLines)
	}
	for i := 0; i < max; i++ {
		var a, b string
		if i < len(oldLines) {
			a = oldLines[i]
		}
		if i < len(newLines) {
			b = newLines[i]
		}
		switch {
		case i >= len(oldLines):
			fmt.Printf("+ %s\n", b)
		case i >= len(newLines):
			fmt.Printf("- %s\n", a)
		case a == b:
			fmt.Printf("  %s\n", a)
		default:
			fmt.Printf("- %s\n+ %s\n", a, b)
		}
	}
}
func Confirm(plan generation.Plan, root string) (generation.Plan, error) {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Apply changes? [y/n/edit]: ")
		line, err := r.ReadString('\n')
		if err != nil {
			return plan, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return plan, nil
		case "n", "no":
			return generation.Plan{}, fmt.Errorf("changes cancelled by user")
		case "edit":
			edited, err := editProposal(plan, root)
			if err != nil {
				return plan, err
			}
			plan = edited
			Preview(plan, root)
		default:
			fmt.Println("Please answer y, n, or edit.")
		}
	}
}
func editProposal(plan generation.Plan, root string) (generation.Plan, error) {
	editor := os.Getenv("FUZECLI_EDITOR")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return plan, fmt.Errorf("edit requested but FUZECLI_EDITOR/EDITOR is not set")
	}
	dir := filepath.Join(root, ".aicli", "proposal")
	if err := os.RemoveAll(dir); err != nil {
		return plan, err
	}
	for _, f := range plan.Files {
		if f.Action == "delete" {
			continue
		}
		path, err := generation.Resolve(dir, f.Path)
		if err != nil {
			return plan, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return plan, err
		}
		if err := os.WriteFile(path, []byte(f.Content), 0600); err != nil {
			return plan, err
		}
		cmd := exec.Command(editor, path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return plan, fmt.Errorf("editor failed: %w", err)
		}
		updated, err := os.ReadFile(path)
		if err != nil {
			return plan, err
		}
		f.Content = string(updated)
	}
	return plan, nil
}
