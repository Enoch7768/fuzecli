package generation

import (
	"bytes"
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

const StrictExecutionMode = `FUZECLI DEVELOPER-GRADE EXECUTION CONTRACT

USER FIRST
1. Follow the user's explicit instructions as the primary task objective, while respecting higher-priority system, safety, security, and tool constraints.
2. Do not deliberately ignore, override, dilute, or reinterpret a clear user requirement.
3. If the request is sufficiently clear, execute it directly.
4. Offer useful improvement suggestions when they can materially improve the result, but clearly label suggestions and never silently turn them into requirements or override the user's direction.
5. If ambiguity would materially change the implementation, ask one focused question instead of guessing.

ENGINEERING QUALITY
6. Every implementation must be production-minded, secure, maintainable, coherent, and internally consistent.
7. Write professional code with correct syntax, imports, types, validation, error handling, secure defaults, accessibility, responsive behavior, and clean architecture.
8. Never knowingly output syntax errors, broken references, missing imports, impossible paths, placeholder implementations, dummy data, TODOs, fake APIs, or unfinished sections.
9. Never truncate generated files. Create and modify operations must contain complete file contents.
10. Preserve unrelated working behavior unless the user explicitly asks for it to change.
11. Verify paths, dependencies, data flow, error paths, and integration points before claiming completion.

DESIGN QUALITY
12. Generated applications must look intentionally designed by a skilled human developer, not like a generic AI template.
13. Give every generated application a deliberate visual identity appropriate to its brief through typography, spacing, hierarchy, color, composition, interaction, and responsive behavior.
14. Avoid recycled landing-page formulas, meaningless decoration, excessive cards, or styling that exists only to look AI-generated.
15. For websites, prioritize polished hierarchy, excellent spacing, accessibility, performance, responsive behavior, useful micro-interactions, and memorable visual character.
16. Make interfaces feel finished immediately: clear entry points, useful empty states, meaningful loading states, precise controls, and graceful errors.

DEVELOPER WORKFLOW
17. Treat the workspace as the source of truth and preserve its conventions.
18. Use the technologies and architecture requested by the user unless they explicitly authorize a change.
19. Prefer robust simplicity over clever complexity.
20. Generated commands are informational unless the surrounding FuzeCLI operation explicitly authorizes execution.
21. Never expose API keys, tokens, secrets, credentials, or private configuration values.
22. Never trust browser-supplied paths, provider names, model names, or file contents without server-side validation.

OUTPUT DISCIPLINE
23. For code-generation operations, return the exact structured format requested by FuzeCLI.
24. Never wrap structured JSON in markdown fences.
25. Never add commentary outside the requested structured response.
26. For ordinary chat, answer naturally and concisely.
27. Put improvement suggestions in the response or explanation field instead of silently changing scope.

MANDATORY FINAL SELF-REVIEW
Before returning the final answer, silently perform a complete second-pass review of the work you just produced. Re-read the user's request and compare it requirement-by-requirement against the implementation. Inspect every generated or modified file for syntax errors, missing imports, broken references, invalid paths, incomplete logic, inconsistent APIs, missing dependencies, accessibility problems, responsive failures, security weaknesses, and unfinished states. Check that the implementation actually satisfies the requested behavior rather than merely describing it. If anything is missing, incorrect, generic, contradictory, or below the requested quality, fix it before returning the final structured response. Never report the review itself; return only the final corrected result.

MOST IMPORTANT
Do excellent work for the user's actual request. Be decisive, technically rigorous, visually thoughtful, secure, and honest about limitations. Do not sacrifice correctness for speed or appearance.`

const systemSchema = `You are a production code generation engine. Respond with ONLY valid JSON matching exactly this schema: {"files":[{"path":"relative/path.ext","content":"full file content","action":"create|modify|delete"}],"explanation":"one paragraph explaining what was done","commands":["optional shell commands"]}. Never use markdown fences. Never omit full content for create or modify. Paths must be relative and must not contain '..'. For delete, content must be empty. Do not invent files outside the user's requested scope.`

func SessionSystemPrompt() string {
	data, _ := json.Marshal(ChatResponseSchema())
	return StrictExecutionMode + "\n\n" + systemSchema + "\n\nSESSION RESPONSE CONTRACT\nBefore responding to the user's first request and every request after it, follow this exact response contract. Ordinary conversation must use type \"chat\" and provide the natural-language answer in both \"response\" and \"message\". Project changes must use type \"edit\" and include every requested file with complete content, a valid relative path, and action \"create\", \"modify\", or \"delete\". Delete actions must have empty content. Never use markdown fences or commentary outside the JSON object. Never invent missing code, files, APIs, dependencies, commands, credentials, or requirements. Never truncate files. Preserve the user's exact intent and all requested changes. The JSON structure to return is:\n" + string(data)
}

func (e *Engine) Messages(profileText string, workspaceContext string, conversation []provider.Message, prompt string) []provider.Message {
	system := SessionSystemPrompt()
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
	if maxChars <= 0 || len(messages) == 0 {
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
	clean, err := normalizeJSONDocument(raw)
	if err != nil {
		return Plan{}, fmt.Errorf("invalid generation JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(clean))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("invalid generation JSON: %s", explainJSONDecodeError(err))
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
		rel := filepath.ToSlash(f.Path)
		if strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasPrefix(rel, "/") || filepath.IsAbs(f.Path) || filepath.VolumeName(f.Path) != "" {
			return Plan{}, fmt.Errorf("path traversal rejected for %q", f.Path)
		}
	}
	return plan, nil
}

func normalizeJSONDocument(raw string) ([]byte, error) {
	s := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if s == "" {
		return nil, errors.New("empty model response")
	}
	s = strings.ReplaceAll(s, "```json", "```")
	if strings.HasPrefix(s, "```") {
		if end := strings.Index(s[3:], "```"); end >= 0 {
			body := s[3 : 3+end]
			body = strings.TrimSpace(strings.TrimPrefix(body, "json"))
			s = strings.TrimSpace(body)
		}
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil, errors.New("response does not contain a JSON object")
	}
	end, ok := balancedJSONEnd(s, start)
	if !ok {
		return nil, errors.New("response contains incomplete JSON and may have been truncated by the provider")
	}
	candidate := strings.TrimSpace(s[start:end])
	if !json.Valid([]byte(candidate)) {
		return nil, errors.New("response contains malformed JSON")
	}
	return []byte(candidate), nil
}

func balancedJSONEnd(s string, start int) (int, bool) {
	stack := make([]byte, 0, 16)
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		switch c {
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) == 0 {
				return 0, false
			}
			open := stack[len(stack)-1]
			if (open == '{' && c != '}') || (open == '[' && c != ']') {
				return 0, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func explainJSONDecodeError(err error) string {
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "unexpected end") || strings.Contains(lower, "unexpected eof") {
		return "response was truncated before the JSON document finished; reduce the batch size and retry"
	}
	return message
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
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	target, err := filepath.Abs(filepath.Join(base, clean))
	if err != nil {
		return "", err
	}
	relBack, err := filepath.Rel(base, target)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %q", rel)
	}
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink target rejected: %q", rel)
	}
	parent := filepath.Dir(target)
	for {
		info, statErr := os.Lstat(parent)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				realParent, evalErr := filepath.EvalSymlinks(parent)
				if evalErr != nil {
					return "", fmt.Errorf("resolve workspace path: %w", evalErr)
				}
				relReal, relErr := filepath.Rel(baseReal, realParent)
				if relErr != nil || relReal == ".." || strings.HasPrefix(relReal, ".."+string(os.PathSeparator)) {
					return "", fmt.Errorf("symlink escapes workspace: %q", rel)
				}
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect workspace path: %w", statErr)
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return target, nil
}

func Apply(root string, plan Plan) ([]string, error) {
	type backup struct {
		path    string
		exists  bool
		content []byte
		mode    os.FileMode
	}
	type staged struct {
		change FileChange
		path   string
		tmp    string
	}

	backups := make([]backup, 0, len(plan.Files))
	stagedFiles := make([]staged, 0, len(plan.Files))
	seen := make(map[string]struct{}, len(plan.Files))

	for i, f := range plan.Files {
		path, err := Resolve(root, f.Path)
		if err != nil {
			return nil, err
		}
		key := filepath.Clean(path)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate file change for %q", f.Path)
		}
		seen[key] = struct{}{}

		info, err := os.Stat(path)
		switch {
		case err == nil:
			if info.IsDir() {
				return nil, fmt.Errorf("cannot modify directory %s", f.Path)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("backup %s: %w", f.Path, readErr)
			}
			backups = append(backups, backup{path: path, exists: true, content: data, mode: info.Mode()})
		case os.IsNotExist(err):
			backups = append(backups, backup{path: path})
		default:
			return nil, fmt.Errorf("inspect %s: %w", f.Path, err)
		}

		if f.Action == "create" || f.Action == "modify" {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, fmt.Errorf("create directory for %s: %w", f.Path, err)
			}
			tmp := fmt.Sprintf("%s.fuzetmp-%d", path, i)
			if err := os.WriteFile(tmp, []byte(f.Content), 0644); err != nil {
				for _, item := range stagedFiles {
					_ = os.Remove(item.tmp)
				}
				return nil, fmt.Errorf("stage %s: %w", f.Path, err)
			}
			stagedFiles = append(stagedFiles, staged{change: f, path: path, tmp: tmp})
		} else if f.Action != "delete" {
			return nil, fmt.Errorf("unsupported file action %q for %s", f.Action, f.Path)
		}
	}

	rollback := func() {
		for _, item := range stagedFiles {
			_ = os.Remove(item.tmp)
		}
		for i := len(backups) - 1; i >= 0; i-- {
			b := backups[i]
			if b.exists {
				_ = os.MkdirAll(filepath.Dir(b.path), 0755)
				tmp := fmt.Sprintf("%s.fuzerollback", b.path)
				if err := os.WriteFile(tmp, b.content, b.mode.Perm()); err == nil {
					_ = os.Rename(tmp, b.path)
				} else {
					_ = os.Remove(tmp)
				}
			} else {
				_ = os.Remove(b.path)
			}
		}
	}

	for _, item := range stagedFiles {
		if err := os.Rename(item.tmp, item.path); err != nil {
			rollback()
			return nil, fmt.Errorf("commit %s: %w", item.change.Path, err)
		}
	}

	for _, f := range plan.Files {
		if f.Action == "delete" {
			path, err := Resolve(root, f.Path)
			if err != nil {
				rollback()
				return nil, err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				rollback()
				return nil, fmt.Errorf("delete %s: %w", f.Path, err)
			}
		}
	}

	written := make([]string, 0, len(plan.Files))
	for _, f := range plan.Files {
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
