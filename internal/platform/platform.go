package platform

import (
	"context"
	"errors"
	"strings"
)

type Manager struct {
	MemoryRoot string
	Workspaces *WorkspaceManager
	Jobs       *JobRunner
}

func NewManager(root string) *Manager {
	manager := &Manager{
		MemoryRoot: root,
		Workspaces: NewWorkspaceManager(),
		Jobs:       NewJobRunner(),
	}
	if strings.TrimSpace(root) != "" {
		_, _ = manager.Workspaces.Add("", root)
	}
	return manager
}

func (m *Manager) Context(ctx context.Context, prompt string, rules []Rule, skills []Skill) (string, error) {
	if m == nil {
		return "", errors.New("platform manager is nil")
	}
	if ctx == nil {
		return "", errors.New("platform context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	memory, err := LoadMemory(m.MemoryRoot)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	appendSection := func(title, content string) {
		if strings.TrimSpace(content) == "" {
			return
		}
		out.WriteString("\n")
		out.WriteString(title)
		out.WriteString(":\n")
		out.WriteString(strings.TrimSpace(content))
		out.WriteString("\n")
	}
	appendList := func(title string, values []string) {
		if len(values) == 0 {
			return
		}
		var list strings.Builder
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			list.WriteString("- ")
			list.WriteString(strings.TrimSpace(value))
			list.WriteString("\n")
		}
		appendSection(title, list.String())
	}
	appendSection("Project architecture", memory.Architecture)
	appendList("Project decisions", memory.Decisions)
	appendList("Project conventions", memory.Conventions)
	appendList("Project dependencies", memory.Dependencies)
	appendList("Known issues", memory.KnownIssues)
	appendList("Project terminology", memory.Terminology)
	if len(rules) > 0 {
		out.WriteString("\nProject rules:\n")
		for _, rule := range rules {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			out.WriteString("\n[")
			out.WriteString(rule.Name)
			out.WriteString("]\n")
			out.WriteString(rule.Content)
			out.WriteString("\n")
		}
	}
	if len(skills) > 0 {
		out.WriteString("\nAvailable skills:\n")
		for _, skill := range skills {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			out.WriteString("\n[")
			out.WriteString(skill.Name)
			out.WriteString("]\n")
			out.WriteString(skill.Content)
			out.WriteString("\n")
		}
	}
	const maxContextBytes = 48 << 10
	result := out.String()
	if len(result) > maxContextBytes {
		result = strings.ToValidUTF8(result[:maxContextBytes], "") + "\n[Project platform context truncated at 48 KiB.]"
	}
	_ = prompt
	return result, nil
}

func (m *Manager) Validate() error {
	if m == nil {
		return errors.New("platform manager is nil")
	}
	if strings.TrimSpace(m.MemoryRoot) == "" {
		return errors.New("platform manager has no workspace root")
	}
	return nil
}
