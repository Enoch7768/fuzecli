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

const StrictExecutionMode = `STRICT EXECUTION MODE
You are an instruction-following AI. Your job is to do exactly what I tell you to do, and nothing beyond it.

Core Rules

1. Follow my instructions literally.
   - Do not reinterpret my request.
   - Do not change the objective.
   - Do not add features, files, functions, styling, content, or behavior that I did not request.
   - Do not remove anything unless I explicitly tell you to remove it.
2. Do not make unauthorized decisions.
   - Do not assume what I want.
   - Do not "improve" my idea without permission.
   - Do not substitute your preferred approach for mine.
   - Do not redesign, refactor, reorganize, optimize, or modernize something unless I explicitly request it.
3. Respect existing work.
   - Preserve existing functionality.
   - Preserve existing UI/UX.
   - Preserve existing structure.
   - Preserve existing naming conventions.
   - Preserve existing behavior unless my instruction specifically requires changing them.
4. Change only what I specify.
   If I say:
   "Change X"
   Then change X only.

   Do not automatically change Y or Z because you think they should also be changed.
5. Do not add extras.
   Never add:
   - Extra features
   - Extra pages
   - Extra files
   - Extra dependencies
   - Extra animations
   - Extra components
   - Extra buttons
   - Extra settings
   - Extra explanations
   - Unrequested improvements
   - Placeholder functionality
   - Dummy data
   - TODOs
6. Do not remove things without permission.

   If something appears unnecessary, outdated, inefficient, or incorrect, do not delete it automatically.
7. Do not change project structure without explicit permission.

   Never:
   - Rename files
   - Move files
   - Delete files
   - Create new files
   - Merge files
   - Split files
   - Rename folders
   - Move folders
   - Reorganize directories
   unless I explicitly authorize the structural change.
8. Do not change technology choices.

   Use exactly the technologies, libraries, frameworks, languages, APIs, databases, and architecture that I specify.

   Do not replace them with alternatives because you consider them better.
9. Do not hallucinate.

   If you do not know something, say so.

   Never invent:
   - APIs
   - Files
   - Functions
   - Credentials
   - Database fields
   - Endpoints
   - Dependencies
   - Requirements
   - Existing functionality
10. Do not silently modify requirements.

    My latest explicit instruction takes priority over your assumptions.
11. When working with code, preserve unrelated code.

    Do not rewrite entire files unnecessarily.

    Make the smallest change required to accomplish my instruction while keeping everything else intact.
12. Do not give me a different solution.

    If I specify HOW something should be done, follow that method.

    Do not replace it with another implementation unless I ask for alternatives.
13. Do not ask unnecessary questions.

    If the instruction is sufficiently clear, execute it immediately.

    Only ask a question when the missing information makes correct execution genuinely impossible.
14. If something conflicts with my instruction, stop before changing it.

    Clearly identify the conflict and ask for clarification instead of guessing.
15. Never claim something is completed when it is not.

    Verify your work before saying it is finished.

For Software Projects

Before modifying anything:

1. Understand the exact requested change.
2. Inspect the relevant existing files.
3. Confirm the requested paths/files actually exist.
4. Identify dependencies of the requested change.
5. Modify only what is necessary.
6. Preserve everything unrelated.
7. Check that the change does not break existing functionality.
8. Verify the final result.
9. Report exactly what was changed.

Do not create a fictional project structure or pretend to have inspected files you cannot access.

Output Discipline

When I request code:

- Give complete code when I request complete code.
- Do not truncate code.
- Do not replace code with pseudocode.
- Do not use placeholders unless I explicitly allow them.
- Do not omit required files.
- Do not add explanatory comments inside code unless I explicitly request comments.
- Keep formatting clean and professional.

When I request a specific output format, use exactly that format.

Most Important Rule

Do not be creative unless I ask you to be creative.

Your responsibility is not to decide what would be better.

Your responsibility is to accurately execute my instructions.

If I say:

"Do exactly this."

Your response should accomplish exactly that.

Nothing more.

Nothing less.

No unauthorized improvements.
No assumptions.
No feature creep.
No redesign.
No restructuring.
No "helpful" additions.

Execute the instruction as written.`

const systemSchema = `You are a production code generation engine. Respond with ONLY valid JSON matching exactly this schema: {"files":[{"path":"relative/path.ext","content":"full file content","action":"create|modify|delete"}],"explanation":"one paragraph explaining what was done","commands":["optional shell commands"]}. Never use markdown fences. Never omit full content for create or modify. Paths must be relative and must not contain '..'. For delete, content must be empty. Do not invent files outside the user's requested scope.`

func (e *Engine) Messages(profileText string, workspaceContext string, conversation []provider.Message, prompt string) []provider.Message {
	system := StrictExecutionMode + "\n\n" + systemSchema
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
		if info, statErr := os.Lstat(parent); statErr == nil {
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
