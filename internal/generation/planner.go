package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type PlannedFile struct {
	Path         string   `json:"path"`
	Purpose      string   `json:"purpose"`
	Dependencies []string `json:"dependencies"`
	Status       string   `json:"status"`
}

type ProjectPlan struct {
	PromptHash string        `json:"prompt_hash"`
	Prompt     string        `json:"prompt"`
	Project    string        `json:"project"`
	Summary    string        `json:"summary"`
	Files      []PlannedFile `json:"files"`
	CreatedAt  string        `json:"created_at"`
}

const PlannerBatchSize = 3

func ShouldUsePlanner(prompt string) bool {
	text := strings.TrimSpace(prompt)

	if len(text) >= 3000 {
		return true
	}

	if strings.Count(text, "\n") >= 20 {
		return true
	}

	lower := strings.ToLower(text)

	terms := []string{
		"full project",
		"complete project",
		"entire website",
		"entire application",
		"full website",
		"full application",
		"admin panel",
		"authentication",
		"database",
		"crud",
		"user accounts",
		"multiple pages",
		"all features",
	}

	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}

	return false
}

func PromptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

func PlannerSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project": map[string]any{
				"type": "string",
			},
			"summary": map[string]any{
				"type": "string",
			},
			"files": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type": "string",
						},
						"purpose": map[string]any{
							"type": "string",
						},
						"dependencies": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
						},
						"status": map[string]any{
							"type": "string",
							"enum": []string{
								"pending",
								"completed",
							},
						},
					},
					"required": []string{
						"path",
						"purpose",
						"dependencies",
						"status",
					},
					"propertyOrdering": []string{
						"path",
						"purpose",
						"dependencies",
						"status",
					},
				},
			},
		},
		"required": []string{
			"project",
			"summary",
			"files",
		},
		"propertyOrdering": []string{
			"project",
			"summary",
			"files",
		},
	}
}

func PlannerPrompt(
	originalPrompt string,
	workspaceContext string,
) []provider.Message {
	system := `You are the FuzeCLI project planning engine.

Analyze the user's complete software request and create a precise implementation manifest.

Return ONLY valid JSON matching the supplied schema.

Rules:
- Plan the entire requested project.
- Include every source file required for the requested functionality.
- Include HTML, CSS, JavaScript, backend, database, configuration and supporting files when required.
- Do not invent unnecessary files.
- Use relative workspace paths only.
- Never use absolute paths.
- Never use '..' path segments.
- Group functionality into files that can be generated independently.
- Keep dependencies accurate.
- Mark every newly planned file as "pending".
- Do not generate file contents yet.
- Do not return markdown.
- Do not omit important files simply to make the plan smaller.`

	if workspaceContext != "" {
		system += "\nExisting workspace context:\n" + workspaceContext
	}

	return []provider.Message{
		{
			Role:    "system",
			Content: system,
		},
		{
			Role:    "user",
			Content: originalPrompt,
		},
	}
}

func ParseProjectPlan(raw string) (ProjectPlan, error) {
	var plan ProjectPlan

	decoder := json.NewDecoder(
		strings.NewReader(raw),
	)

	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&plan); err != nil {
		return ProjectPlan{}, fmt.Errorf(
			"invalid project planner JSON: %w",
			err,
		)
	}

	var trailing any

	err := decoder.Decode(&trailing)

	if err == nil {
		return ProjectPlan{}, fmt.Errorf(
			"project planner JSON contains trailing data",
		)
	}

	if err != io.EOF {
		return ProjectPlan{}, fmt.Errorf(
			"invalid project planner trailing JSON: %w",
			err,
		)
	}

	if plan.Project == "" {
		return ProjectPlan{}, fmt.Errorf(
			"project planner returned no project name",
		)
	}

	if len(plan.Files) == 0 {
		return ProjectPlan{}, fmt.Errorf(
			"project planner returned no files",
		)
	}

	for i := range plan.Files {
		file := &plan.Files[i]

		file.Path = filepath.ToSlash(
			strings.TrimSpace(file.Path),
		)

		if file.Path == "" {
			return ProjectPlan{}, fmt.Errorf(
				"planned file %d has an empty path",
				i,
			)
		}

		if filepath.IsAbs(file.Path) ||
			strings.HasPrefix(file.Path, "/") ||
			strings.HasPrefix(file.Path, "//") ||
			strings.Contains(file.Path, "../") {
			return ProjectPlan{}, fmt.Errorf(
				"unsafe planned path %q",
				file.Path,
			)
		}

		if file.Status == "" {
			file.Status = "pending"
		}

		if file.Status != "pending" &&
			file.Status != "completed" {
			return ProjectPlan{}, fmt.Errorf(
				"planned file %q has invalid status %q",
				file.Path,
				file.Status,
			)
		}
	}

	return plan, nil
}

func NewProjectPlan(
	prompt string,
	plan ProjectPlan,
) ProjectPlan {
	for i := range plan.Files {
		if plan.Files[i].Status == "" {
			plan.Files[i].Status = "pending"
		}
	}

	plan.Prompt = prompt
	plan.PromptHash = PromptHash(prompt)

	if plan.CreatedAt == "" {
		plan.CreatedAt = time.Now().
			UTC().
			Format(time.RFC3339)
	}

	return plan
}

func ProjectPlanPath(root string) string {
	return filepath.Join(
		root,
		".aicli",
		"project-plan.json",
	)
}

func SaveProjectPlan(
	root string,
	plan ProjectPlan,
) error {
	dir := filepath.Join(
		root,
		".aicli",
	)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(
		plan,
		"",
		"  ",
	)
	if err != nil {
		return fmt.Errorf(
			"encode project plan: %w",
			err,
		)
	}

	path := ProjectPlanPath(root)
	temp := path + ".tmp"

	if err := os.WriteFile(
		temp,
		data,
		0644,
	); err != nil {
		return fmt.Errorf(
			"write project plan: %w",
			err,
		)
	}

	if err := os.Rename(
		temp,
		path,
	); err != nil {
		_ = os.Remove(temp)

		return fmt.Errorf(
			"save project plan: %w",
			err,
		)
	}

	return nil
}

func LoadProjectPlan(
	root string,
	prompt string,
) (*ProjectPlan, error) {
	data, err := os.ReadFile(
		ProjectPlanPath(root),
	)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var plan ProjectPlan

	if err := json.Unmarshal(
		data,
		&plan,
	); err != nil {
		return nil, fmt.Errorf(
			"decode existing project plan: %w",
			err,
		)
	}

	if plan.PromptHash != PromptHash(prompt) {
		return nil, nil
	}

	return &plan, nil
}

func PendingFiles(
	plan ProjectPlan,
) []PlannedFile {
	pending := make(
		[]PlannedFile,
		0,
	)

	for _, file := range plan.Files {
		if file.Status != "completed" {
			pending = append(
				pending,
				file,
			)
		}
	}

	return pending
}

func NextBatch(
	plan ProjectPlan,
) []PlannedFile {
	pending := PendingFiles(plan)

	if len(pending) <= PlannerBatchSize {
		return pending
	}

	return pending[:PlannerBatchSize]
}

func MarkBatchCompleted(
	plan *ProjectPlan,
	paths []string,
) {
	completed := make(
		map[string]struct{},
		len(paths),
	)

	for _, path := range paths {
		normalized := filepath.ToSlash(path)
		completed[normalized] = struct{}{}
	}

	for i := range plan.Files {
		normalized := filepath.ToSlash(
			plan.Files[i].Path,
		)

		if _, ok := completed[normalized]; ok {
			plan.Files[i].Status = "completed"
		}
	}
}

func BuildBatchPrompt(
	originalPrompt string,
	plan ProjectPlan,
	batch []PlannedFile,
) string {
	type batchFile struct {
		Path         string   `json:"path"`
		Purpose      string   `json:"purpose"`
		Dependencies []string `json:"dependencies"`
	}

	files := make(
		[]batchFile,
		0,
		len(batch),
	)

	for _, file := range batch {
		files = append(
			files,
			batchFile{
				Path:         file.Path,
				Purpose:      file.Purpose,
				Dependencies: file.Dependencies,
			},
		)
	}

	data, _ := json.MarshalIndent(
		files,
		"",
		"  ",
	)

	return fmt.Sprintf(
		`Implement the next generation batch for this project.

Original project request:
%s

Project:
%s

Project summary:
%s

Generate ONLY these planned files:
%s

Rules:
- Return ONLY valid JSON using the FuzeCLI file-generation schema.
- Do not return markdown.
- Do not generate files outside this batch.
- Every requested path must be returned.
- Keep existing valid workspace code intact.
- Read relevant existing workspace files before deciding how these files integrate.
- Use full file contents for create and modify actions.
- Never use absolute paths.
- Never use '..' path segments.
- Respect the existing project architecture.
- Make the implementation functional and production-quality.
`,
		originalPrompt,
		plan.Project,
		plan.Summary,
		string(data),
	)
}

func BatchPaths(
	batch []PlannedFile,
) map[string]struct{} {
	result := make(
		map[string]struct{},
		len(batch),
	)

	for _, file := range batch {
		result[filepath.ToSlash(file.Path)] = struct{}{}
	}

	return result
}

func ValidateBatch(
	plan Plan,
	batch []PlannedFile,
) error {
	expected := BatchPaths(batch)

	seen := make(
		map[string]struct{},
		len(plan.Files),
	)

	for _, file := range plan.Files {
		path := filepath.ToSlash(
			file.Path,
		)

		if _, ok := expected[path]; !ok {
			return fmt.Errorf(
				"generation returned unexpected file %q",
				file.Path,
			)
		}

		if _, ok := seen[path]; ok {
			return fmt.Errorf(
				"generation returned duplicate file %q",
				file.Path,
			)
		}

		seen[path] = struct{}{}
	}

	for _, file := range batch {
		path := filepath.ToSlash(
			file.Path,
		)

		if _, ok := seen[path]; !ok {
			return fmt.Errorf(
				"generation omitted planned file %q",
				file.Path,
			)
		}
	}

	return nil
}
