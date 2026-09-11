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
	history, err = s.App.CompactHistoryForAPI(ctx, name, model, history)
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
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant. Use natural language for ordinary conversation. For requests that create, modify, or delete project files, return ONLY one valid JSON object with this shape: {\"files\":[{\"path\":\"relative/path.ext\",\"line_start\":1,\"line_end\":1000,\"content\":\"full file content\"}],\"explanation\":\"brief explanation\",\"commands\":[]}. The action field is optional; when absent, FuzeCLI infers create or modify from the workspace. Never use markdown fences around generation JSON."
	if workspaceContext != "" {
		system += "\nRelevant workspace files read from disk:\n" + workspaceContext
	}
	if len(attachments) > 0 {
		system += "\nUser-attached files:\n"
		for _, file := range attachments {
			system += "\n--- " + file.Path + " ---\n" + file.Content + "\n--- end " + file.Path + " ---\n"
		}
	}
	engine := generation.Engine{Registry: s.App.Registry, Profile: &s.App.Profile, MaxContextChars: 120000}
	msgs := engine.Messages(s.App.Profile.Condensed(), workspaceContext, history, req.Prompt)
	msgs[0].Content = system
	if err := s.App.Store.AddMessage(provider.Message{Role: "user", Content: req.Prompt}); err != nil {
		return ChatResponse{}, err
	}
	resp, err := s.App.Registry.Send(ctx, name, msgs, provider.RequestOptions{Model: model, Temperature: 0.3, MaxTokens: 16000})
	if err != nil {
		return ChatResponse{}, err
	}
	if err := s.App.Store.AddMessage(provider.Message{Role: "assistant", Content: resp.Content}); err != nil {
		return ChatResponse{}, err
	}
	result := ChatResponse{Content: resp.Content, Provider: resp.ProviderName, Model: resp.Model}
	if result.Provider == "" {
		result.Provider = name
	}
	if result.Model == "" {
		result.Model = model
	}
	if plan, parseErr := generation.ParseChatPlan(resp.Content); parseErr == nil && req.Apply {
		written, applyErr := generation.ApplyChatPlan(s.App.Store.Root, plan)
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

func (s *Service) Root() string {
	if s == nil || s.App == nil || s.App.Store == nil {
		return ""
	}
	return s.App.Store.Root
}
