package app

import (
	"fmt"

	gitworkspace "github.com/Enoch7768/fuzecli/internal/git"
)

func GitStatus(root string) error {
	status, err := gitworkspace.StatusOf(root)
	if err != nil {
		return err
	}
	if status.Clean {
		fmt.Printf("Git: clean (%s)\n", status.Branch)
		return nil
	}
	fmt.Printf("Git branch: %s\n", status.Branch)
	for _, file := range status.Files {
		fmt.Printf("  %c%c %s\n", file.Index, file.Worktree, file.Path)
	}
	return nil
}

func GitDiff(root string) error {
	out, err := gitworkspace.Diff(root, false)
	if err != nil {
		return err
	}
	if out == "" {
		fmt.Println("Git: no unstaged diff")
		return nil
	}
	fmt.Print(out)
	return nil
}


func GitDiffText(root string) (string, error) {
	return gitworkspace.Diff(root, false)
}
