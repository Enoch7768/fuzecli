package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

const MaxAttachmentBytes = 65536

type Attachment struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Prompt   string   `json:"prompt"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Files    []string `json:"files,omitempty"`
	Apply    bool     `json:"apply,omitempty"`
	BillingMode string `json:"billing_mode,omitempty"`
}

type ChatResponse struct {
	Content      string   `json:"content"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	WrittenFiles []string `json:"written_files,omitempty"`
	Applied      bool     `json:"applied"`
}

type Service struct {
	App *app.App
}

func NewService(a *app.App) *Service {
	return &Service{App: a}
}

func (s *Service) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if s == nil || s.App == nil || s.App.Store == nil {
		return ChatResponse{}, errors.New("workspace not initialized")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return ChatResponse{}, errors.New("prompt is required")
	}
	name, model, err := s.App.ProviderAndModel(req.Provider, req.Model)
	if err != nil {
		return ChatResponse{}, err
	}
	history, err := s.App.Store.History(400)
	if err != nil {
		return ChatResponse{}, err
	}
	workspaceContext, err := s.App.Store.WorkspaceContext()
	if err != nil {
		return ChatResponse{}, err
	}
	attachments, err := s.ReadAttachments(req.Files)
	if err != nil {
		return ChatResponse{}, err
	}
	system := generation.SessionSystemPrompt() + "\n\nYou are FuzeCLI, a practical coding assistant. Use the session response contract above for every response and never ask the user to provide source files that FuzeCLI already supplied."
	if workspaceContext != "" {
		system += "\nRelevant workspace files read from disk:\n" + workspaceContext
	}
	if len(attachments) > 0 {
		system += "\nThe user attached the following workspace files for this request. These attachments are authoritative input and must be treated as available context; do not ask the user to re-upload or paste them. The provider receives their text content directly in the request:\n"
		for _, file := range attachments {
			system += "\nATTACHMENT: " + file.Path + "\nBEGIN_ATTACHMENT\n" + file.Content + "\nEND_ATTACHMENT\n"
		}
	}
	engine := generation.Engine{Registry: s.App.Registry, Profile: &s.App.Profile, MaxContextChars: 120000}
	msgs := engine.Messages(s.App.Profile.Condensed(), workspaceContext, history, req.Prompt)
	msgs[0].Content = system
	if err := s.App.Store.AddMessage(provider.Message{Role: "user", Content: req.Prompt}); err != nil {
		return ChatResponse{}, err
	}
	resp, err := s.App.Registry.Send(ctx, name, msgs, provider.RequestOptions{Model: model, Temperature: 0.3, MaxTokens: 32768, JSONMode: true, JSONSchema: generation.ChatResponseSchema(), BillingMode: req.BillingMode})
	if err != nil {
		return ChatResponse{}, err
	}
	parsed, parseErr := engine.ParseChatResponse(ctx, resp.Content)
	if parseErr != nil {
		return ChatResponse{}, fmt.Errorf("complete assistant response could not be validated: %w", parseErr)
	}
	content := resp.Content
	if parsed.Response != "" {
		content = parsed.Response
	} else if parsed.Message != "" {
		content = parsed.Message
	} else if parsed.Explanation != "" {
		content = parsed.Explanation
	}
	if err := s.App.Store.AddMessage(provider.Message{Role: "assistant", Content: content}); err != nil {
		return ChatResponse{}, err
	}
	result := ChatResponse{Content: content, Provider: resp.ProviderName, Model: resp.Model}
	if result.Provider == "" {
		result.Provider = name
	}
	if result.Model == "" {
		result.Model = model
	}
	if parsed.Plan != nil && req.Apply {
		written, applyErr := generation.ApplyChatPlan(s.App.Store.Root, *parsed.Plan)
		if applyErr != nil {
			return ChatResponse{}, applyErr
		}
		if err := s.App.Store.MarkTouched(written); err != nil {
			return ChatResponse{}, err
		}
		_ = s.App.Store.RefreshHashes(written)
		result.WrittenFiles = written
		result.Applied = true
	}
	return result, nil
}

func (s *Service) RefreshMemory() error {
	if s == nil || s.App == nil || s.App.Store == nil {
		return errors.New("workspace not initialized")
	}
	_, err := s.App.Store.History(400)
	return err
}

func (s *Service) WriteUploadedFile(rel string, data []byte) error {
	if s == nil || s.App == nil || s.App.Store == nil { return errors.New("workspace not initialized") }
	if len(data) > 10<<20 { return errors.New("uploaded file is larger than 10 MiB") }
	clean := filepath.ToSlash(strings.TrimSpace(rel))
	if clean == "" || filepath.Base(clean) != filepath.Base(rel) || strings.Contains(clean, "..") { return errors.New("invalid uploaded filename") }
	path, err := generation.Resolve(s.App.Store.Root, clean)
	if err != nil { return err }
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() { return fmt.Errorf("%s is a directory", clean) }
	if err := os.WriteFile(path, data, 0600); err != nil { return fmt.Errorf("write uploaded file: %w", err) }
	if err := s.App.Store.MarkTouched([]string{clean}); err != nil { return err }
	return s.App.Store.RefreshHashes([]string{clean})
}

func (s *Service) WriteFile(rel, content string) error {
	if s == nil || s.App == nil || s.App.Store == nil {
		return errors.New("workspace not initialized")
	}
	if len(content) > 2<<20 {
		return errors.New("file is larger than 2 MiB")
	}
	if _, err := generation.Resolve(s.App.Store.Root, rel); err != nil {
		return err
	}
	plan := generation.Plan{Files: []generation.FileChange{{Path: rel, Content: content, Action: "modify"}}}
	written, err := generation.Apply(s.App.Store.Root, plan)
	if err != nil {
		return err
	}
	if err := s.App.Store.MarkTouched(written); err != nil {
		return err
	}
	return s.App.Store.RefreshHashes(written)
}

func (s *Service) ReadFile(rel string) (string, error) {
	if s == nil || s.App == nil || s.App.Store == nil {
		return "", errors.New("workspace not initialized")
	}
	path, err := generation.Resolve(s.App.Store.Root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", rel)
	}
	if info.Size() > MaxAttachmentBytes {
		return "", fmt.Errorf("file %s is larger than 64 KiB", rel)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file %s is not valid UTF-8 text", rel)
	}
	return string(data), nil
}

func (s *Service) ReadAttachments(paths []string) ([]Attachment, error) {
	files := make([]Attachment, 0, len(paths))
	for _, rel := range paths {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}
		content, err := s.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		files = append(files, Attachment{Path: rel, Content: content})
	}
	return files, nil
}

func (s *Service) ListFiles(prefix string) ([]string, error) {
	if s == nil || s.App == nil || s.App.Store == nil {
		return nil, errors.New("workspace not initialized")
	}
	root := s.App.Store.Root
	prefix = filepath.ToSlash(strings.Trim(strings.TrimSpace(prefix), "/"))
	files := make([]string, 0, 128)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			rel, _ := filepath.Rel(root, path)
			name := filepath.Base(rel)
			if name == ".git" || name == ".aicli" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(rel, prefix) {
			return nil
		}
		files = append(files, rel)
		if len(files) >= 1000 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (s *Service) Models(ctx context.Context, name string) (map[string]any, error) {
	if s == nil || s.App == nil || s.App.Registry == nil {
		return nil, errors.New("application not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.App.Config.DefaultProvider
	}
	if name == "auto" {
		name = s.App.Config.DefaultProvider
	}
	models, err := s.App.Registry.ListModels(ctx, name)
	if err != nil {
		return nil, err
	}
	clean := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || strings.EqualFold(model, "default") || strings.EqualFold(model, "auto") {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		clean = append(clean, model)
	}
	sort.Strings(clean)
	return map[string]any{"provider": name, "models": clean, "default_model": s.App.Registry.DefaultModel(name)}, nil
}

func (s *Service) Telemetry() (provider.TelemetrySnapshot, error) {
	if s == nil || s.App == nil || s.App.Registry == nil {
		return provider.TelemetrySnapshot{}, errors.New("application not initialized")
	}
	return s.App.Registry.Telemetry(), nil
}

func (s *Service) Root() string {
	if s == nil || s.App == nil || s.App.Store == nil {
		return ""
	}
	return s.App.Store.Root
}

func (s *Service) History(limit int) ([]provider.Message, error) {
	if s == nil || s.App == nil || s.App.Store == nil {
		return nil, errors.New("workspace not initialized")
	}
	return s.App.Store.History(limit)
}

func (s *Service) Touched() ([]string, error) {
	if s == nil || s.App == nil || s.App.Store == nil {
		return nil, errors.New("workspace not initialized")
	}
	return s.App.Store.Touched()
}

func (s *Service) Config() (map[string]any, error) {
	if s == nil || s.App == nil {
		return nil, errors.New("application not initialized")
	}
	c, err := config.Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := c.Providers[name]
		providers = append(providers, map[string]any{
			"name":          name,
			"default_model": p.DefaultModel,
			"configured":    strings.TrimSpace(p.APIKey) != "" || name == "llamacpp",
		})
	}
	return map[string]any{"default_provider": c.DefaultProvider, "providers": providers, "workspace": s.Root()}, nil
}

func (s *Service) SetProvider(name, model string) error {
	name = strings.TrimSpace(name)
	model = strings.TrimSpace(model)
	if name == "" {
		return errors.New("provider is required")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	p, ok := c.Providers[name]
	if !ok {
		return fmt.Errorf("unknown provider %q", name)
	}
	if model != "" {
		p.DefaultModel = model
	}
	c.Providers[name] = p
	c.DefaultProvider = name
	return config.Save(c)
}
