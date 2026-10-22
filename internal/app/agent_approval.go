package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/agent"
)

func TerminalAgentAuthorizer(scanner *bufio.Scanner, safeMode bool) agent.ToolAuthorizer {
	return func(_ context.Context, definition agent.ToolDefinition, raw json.RawMessage) error {
		deletions := terminalDeletionPaths(definition.Name, raw)
		if len(deletions) > 0 {
			if safeMode {
				return fmt.Errorf("safe mode blocks deletion of %s", strings.Join(deletions, ", "))
			}
			fmt.Println()
			fmt.Println("FUZECLI CONFIRMATION REQUIRED")
			fmt.Println("The AI requested permanent deletion of these workspace paths:")
			for _, path := range deletions {
				fmt.Printf("  - %s\n", path)
			}
			if !terminalConfirm(scanner, "Delete only these exact paths? [y/N]: ") {
				return errors.New("user declined deletion; no files were deleted")
			}
		}

		required, reason := agent.RequiresToolApproval(definition.Name, raw)
		if required && !(len(deletions) > 0 && terminalRequestContainsOnlyDeletes(raw)) {
			fmt.Println()
			fmt.Println("FUZECLI ACTION APPROVAL REQUIRED")
			fmt.Printf("Tool: %s\n", definition.Name)
			fmt.Printf("Action: %s\n", terminalApprovalSummary(definition.Name, raw))
			if reason != "" {
				fmt.Printf("Reason: %s\n", reason)
			}
			if !terminalConfirm(scanner, "Allow FuzeCLI to perform this action? [y/N]: ") {
				return errors.New("user declined the action; FuzeCLI did not execute it")
			}
		}
		return nil
	}
}

func terminalConfirm(scanner *bufio.Scanner, prompt string) bool {
	if scanner == nil {
		fmt.Println("No interactive confirmation input is available; action blocked.")
		return false
	}
	fmt.Print(prompt)
	if !scanner.Scan() {
		fmt.Println("No confirmation received; action blocked.")
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func terminalDeletionPaths(name string, raw json.RawMessage) []string {
	if name != "workspace.apply_edits" {
		return nil
	}
	var request struct {
		Files []struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return nil
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(request.Files))
	for _, file := range request.Files {
		path := strings.TrimSpace(strings.ReplaceAll(file.Path, "\\", "/"))
		if file.Action == "delete" && path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func terminalRequestContainsOnlyDeletes(raw json.RawMessage) bool {
	var request struct {
		Files []struct {
			Action string `json:"action"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &request) != nil || len(request.Files) == 0 {
		return false
	}
	for _, file := range request.Files {
		if file.Action != "delete" {
			return false
		}
	}
	return true
}

func terminalApprovalSummary(name string, raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return "review the requested operation before allowing it"
	}
	switch name {
	case "workspace.apply_edits":
		var request struct {
			Files []struct {
				Path   string `json:"path"`
				Action string `json:"action"`
			} `json:"files"`
		}
		_ = json.Unmarshal(raw, &request)
		parts := make([]string, 0, len(request.Files))
		for _, file := range request.Files {
			parts = append(parts, file.Action+" "+file.Path)
		}
		return strings.Join(parts, ", ")
	case "terminal.run":
		executable, _ := args["executable"].(string)
		arguments, _ := args["args"].([]any)
		values := make([]string, 0, len(arguments))
		for _, value := range arguments {
			values = append(values, fmt.Sprint(value))
		}
		return strings.TrimSpace(executable + " " + strings.Join(values, " "))
	default:
		for _, key := range []string{"action", "repository", "branch", "remote", "message", "number", "operation", "cwd"} {
			if value, ok := args[key]; ok {
				return fmt.Sprintf("%s=%v", key, value)
			}
		}
		encoded, err := json.Marshal(args)
		if err != nil {
			return "review the requested operation before allowing it"
		}
		summary := string(encoded)
		if len(summary) > 500 {
			summary = summary[:500] + "…"
		}
		return summary
	}
}
