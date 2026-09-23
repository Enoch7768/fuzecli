package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/transaction"
)

type VerificationResult struct {
	Passed  bool
	Output  string
	Changed []string
}

type Executor struct {
	Root string
}

func (e Executor) Execute(plan generation.Plan, verify func() VerificationResult) (VerificationResult, error) {
	if e.Root == "" {
		return VerificationResult{}, fmt.Errorf("workspace root is required")
	}
	tx := transaction.New()
	changed := make([]string, 0, len(plan.Files))

	for _, file := range plan.Files {
		path, err := generation.Resolve(e.Root, file.Path)
		if err != nil {
			_ = tx.Rollback()
			return VerificationResult{}, err
		}
		switch file.Action {
		case "create", "modify":
			mode := os.FileMode(0644)
			if info, statErr := os.Stat(path); statErr == nil {
				mode = info.Mode().Perm()
			}
			if err := tx.Write(path, []byte(file.Content), mode); err != nil {
				_ = tx.Rollback()
				return VerificationResult{}, fmt.Errorf("write %s: %w", file.Path, err)
			}
			changed = append(changed, filepath.ToSlash(file.Path))
		case "delete":
			if err := tx.Delete(path); err != nil {
				_ = tx.Rollback()
				return VerificationResult{}, fmt.Errorf("delete %s: %w", file.Path, err)
			}
			changed = append(changed, filepath.ToSlash(file.Path))
		default:
			_ = tx.Rollback()
			return VerificationResult{}, fmt.Errorf("unsupported file action %q", file.Action)
		}
	}

	result := VerificationResult{Changed: changed}
	if verify != nil {
		result = verify()
		result.Changed = changed
		if !result.Passed {
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				return result, fmt.Errorf("verification failed: %s; rollback failed: %w", result.Output, rollbackErr)
			}
			return result, fmt.Errorf("verification failed: %s", result.Output)
		}
	}

	tx.Commit()
	return result, nil
}
