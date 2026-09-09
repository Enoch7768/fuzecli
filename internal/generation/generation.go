package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/profile"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

type FileChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Action  string `json:"action"`
}
type Plan struct {
	Files       []FileChange `json:"files"`
	Explanation string       `json:"explanation"`
	Commands    []string     `json:"commands"`
}

type Engine struct {
	Registry        *provider.Registry
	Profile         *profile.Profile
	MaxContextChars int
}

const systemSchema = `You are a production code generation engine. Respond with ONLY valid JSON matching exactly this schema: {"files":[{"path":"relative/path.ext","content":"full file content","action":"create|modify|delete"}],"explanation":"one paragraph explaining what was done","commands":["optional shell commands"]}. Never use markdown fences. Never omit full content for create or modify. Paths must be relative and must not contain '..'. For delete, content must be empty. Do not invent files outside the user's requested scope.`

func (e *Engine) Messages(profileText string, workspaceContext string, conversation []provider.Message, prompt string) []provider.Message {
	system := systemSchema
	if profileText != "" {
		system += "\nDeveloper profile:\n" + profileText
	}
	if workspaceContext != "" {
		system += "\nWorkspace context:\n" + workspaceContext
	}
	msgs := []provider.Message{{Role: "system", Content: system}}
	msgs = append(msgs, conversation...)
	msgs = append(msgs, provider.Message{Role: "user", Content: prompt})
	return trimMessages(msgs, e.MaxContextChars)
}

func trimMessages(messages []provider.Message, maxChars int) []provider.Message {
	if maxChars <= 0 {
		return messages
	}
	total := 0
	for _, m := range messages {
		total += len(m.Content)
	}
	if total <= maxChars {
		return messages
	}
	out := []provider.Message{messages[0]}
	used := len(messages[0].Content)
	for i := len(messages) - 1; i >= 1; i-- {
		m := messages[i]
		if used+len(m.Content) > maxChars {
			continue
		}
		out = append([]provider.Message{m}, out...)
		used += len(m.Content)
	}
	return out
}

func ParsePlan(raw string) (Plan, error) {
	var plan Plan
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("invalid generation JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Plan{}, errors.New("generation JSON contains trailing data")
		}
		return Plan{}, fmt.Errorf("invalid trailing generation JSON: %w", err)
	}
	if len(plan.Files) == 0 {
		return Plan{}, errors.New("generation JSON contains no files")
	}
	for i, f := range plan.Files {
		if f.Path == "" {
			return Plan{}, fmt.Errorf("file %d has empty path", i)
		}
		if f.Action != "create" && f.Action != "modify" && f.Action != "delete" {
			return Plan{}, fmt.Errorf("file %d has invalid action %q", i, f.Action)
		}
		if f.Action == "delete" && f.Content != "" {
			return Plan{}, fmt.Errorf("file %d delete action must have empty content", i)
		}
		if strings.Contains(filepath.ToSlash(f.Path), "../") || strings.HasPrefix(filepath.ToSlash(f.Path), "../") {
			return Plan{}, fmt.Errorf("path traversal rejected for %q", f.Path)
		}
	}
	return plan, nil
}

func Resolve(root, rel string) (string, error) {
	normalized := filepath.ToSlash(rel)
	clean := filepath.Clean(rel)
	volume := filepath.VolumeName(rel)
	rooted := filepath.IsAbs(rel) || strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || volume != ""
	if clean == "." || rooted || strings.Contains(normalized, "../") {
		return "", fmt.Errorf("unsafe workspace path: %q", rel)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil {
		return "", err
	}
	relBack, err := filepath.Rel(base, target)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %q", rel)
	}
	return target, nil
}

func Apply(root string, plan Plan) ([]string, error) {
	written := []string{}
	for _, f := range plan.Files {
		path, err := Resolve(root, f.Path)
		if err != nil {
			return written, err
		}
		switch f.Action {
		case "delete":
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return written, fmt.Errorf("delete %s: %w", f.Path, err)
			}
		case "create", "modify":
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return written, fmt.Errorf("create directory for %s: %w", f.Path, err)
			}
			tmp := path + ".fuzetmp"
			if err := os.WriteFile(tmp, []byte(f.Content), 0644); err != nil {
				return written, fmt.Errorf("write %s: %w", f.Path, err)
			}
			if err := os.Rename(tmp, path); err != nil {
				_ = os.Remove(tmp)
				return written, fmt.Errorf("replace %s: %w", f.Path, err)
			}
		}
		written = append(written, f.Path)
	}
	return written, nil
}

func HashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func BuildCorrectionPrompt(plan Plan, verificationOutput string) string {
	b, _ := json.Marshal(plan)
	return fmt.Sprintf("Fix the generated implementation. Keep all valid work and correct the verification failures below. Return ONLY the same JSON schema. Previous plan: %s\nVerification failures:\n%s", string(b), verificationOutput)
}
