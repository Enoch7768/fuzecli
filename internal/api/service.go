package api

import (
	"sync"
	"time"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"mime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/agent"
	"github.com/Enoch7768/fuzecli/internal/verify"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/platform"
)

const MaxAttachmentBytes = 2 << 20
const MaxAttachmentTotalBytes = 8 << 20

type Attachment struct {
	Path string `json:"path"`
	Content string `json:"content"`
	MIMEType string `json:"mime_type"`
	SizeBytes int64 `json:"size_bytes"`
}

type ChatRequest struct {
	RequestID string `json:"request_id,omitempty"`
	Prompt   string   `json:"prompt"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Files    []string `json:"files,omitempty"`
	Apply    bool     `json:"apply,omitempty"`
	BillingMode string `json:"billing_mode,omitempty"`
}

type ChatResponse struct {
	Content       string         `json:"content"`
	Provider      string         `json:"provider"`
	Model         string         `json:"model"`
	PromptTokens  int            `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	TotalTokens   int            `json:"total_tokens"`
	UsageEstimated bool          `json:"usage_estimated,omitempty"`
	WrittenFiles  []string       `json:"written_files,omitempty"`
	Applied       bool           `json:"applied"`
}

type StudioEvent struct {
	RequestID string `json:"request_id"`
	Type string `json:"type"`
	Phase string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model string `json:"model,omitempty"`
	Path string `json:"path,omitempty"`
	Percent int `json:"percent,omitempty"`
	PromptTokens int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens int `json:"total_tokens,omitempty"`
	UsageEstimated bool `json:"usage_estimated,omitempty"`
	CostUSD float64 `json:"cost_usd,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type studioSubscriber struct { ch chan StudioEvent }

type Service struct {
	App *app.App
	eventMu sync.Mutex
	eventSubscribers map[*studioSubscriber]struct{}
}

func (s *Service) emitEvent(event StudioEvent) {
	if s == nil { return }
	if event.Timestamp.IsZero() { event.Timestamp = time.Now() }
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for sub := range s.eventSubscribers {
		select { case sub.ch <- event: default: }
	}
}

func (s *Service) SubscribeStudioEvents() (<-chan StudioEvent, func()) {
	sub := &studioSubscriber{ch: make(chan StudioEvent, 1024)}
	s.eventMu.Lock()
	if s.eventSubscribers == nil { s.eventSubscribers = make(map[*studioSubscriber]struct{}) }
	s.eventSubscribers[sub] = struct{}{}
	s.eventMu.Unlock()
	return sub.ch, func() {
		s.eventMu.Lock()
		if _, ok := s.eventSubscribers[sub]; ok { delete(s.eventSubscribers, sub); close(sub.ch) }
		s.eventMu.Unlock()
	}
}

func NewService(a *app.App) *Service {
	return &Service{App: a, eventSubscribers: make(map[*studioSubscriber]struct{})}
}

func chatSystemPrompt(apply bool) string {
	system := generation.SessionSystemPrompt() + "\n\nYou are FuzeCLI, a practical coding assistant. Use the actual workspace context when available, keep answers direct and clear, and never ask the user to provide source files that FuzeCLI already supplied."
	if apply {
		return system + "\n\nSTRUCTURED RESPONSE CONTRACT: This request may change workspace files. Return one JSON object only, using type=chat for explanation or type=edit with files containing path, action, and complete content for file changes. Never truncate file content. Do not include shell commands for automatic execution."
	}
	return system + "\n\nRESPONSE CONTRACT: This is a conversation or analysis-only request. Answer naturally in ordinary text; do not wrap the answer in JSON or follow a JSON schema. Do not modify files or return a structured edit plan."
}

func chatRequestOptions(model, billingMode, requestID string, structured bool) provider.RequestOptions {
	options := provider.RequestOptions{
		Model: model,
		Temperature: 0.3,
		MaxTokens: 32768,
		BillingMode: billingMode,
		RequestID: requestID,
	}
	if structured {
		options.JSONMode = true
		options.JSONSchema = generation.ChatResponseSchema()
	}
	return options
}

func (s *Service) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if s == nil || s.App == nil || s.App.Store == nil { return ChatResponse{}, errors.New("workspace not initialized") }
	if strings.TrimSpace(req.Prompt) == "" { return ChatResponse{}, errors.New("prompt is required") }
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" { requestID = fmt.Sprintf("studio-%d", time.Now().UnixNano()) }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"session.started", Phase:"preparing", Message:"Preparing workspace context", Percent:3})
	name, model, err := s.App.ProviderAndModel(req.Provider, req.Model)
	if err != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.failed", Phase:"provider", Message:err.Error()}); return ChatResponse{}, err }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"provider.selected", Phase:"provider", Message:"Provider selected", Provider:name, Model:model, Percent:10})
	history, err := s.App.Store.History(400); if err != nil { return ChatResponse{}, err }
	workspaceContext, err := s.App.Store.WorkspaceContext(); if err != nil { return ChatResponse{}, err }
	attachments, err := s.ReadAttachments(req.Files); if err != nil { return ChatResponse{}, err }
	system := chatSystemPrompt(req.Apply)
	if workspaceContext != "" { system += "\nRelevant workspace files read from disk:\n" + workspaceContext }
	if s.App.Platform != nil {
		rules, ruleErr := platform.LoadRules(s.App.Store.Root)
		if ruleErr != nil { return ChatResponse{}, fmt.Errorf("load project rules: %w", ruleErr) }
		skills, skillErr := platform.LoadSkills(s.App.Store.Root)
		if skillErr != nil { return ChatResponse{}, fmt.Errorf("load project skills: %w", skillErr) }
		extra, contextErr := s.App.Platform.Context(ctx, req.Prompt, rules, skills)
		if contextErr != nil { return ChatResponse{}, fmt.Errorf("load project platform context: %w", contextErr) }
		if extra != "" { system += "\nFuze project platform context:\n" + extra }
	}
	if len(attachments) > 0 {
		system += "\nThe user attached the following workspace files for this request. These attachments are authoritative input. The provider receives their extracted UTF-8 text directly in the request. MIME type and size are included so you know exactly what is available. Binary or unsupported formats are rejected instead of silently omitted:\n"
		for _, file := range attachments { system += "\n" + attachmentSummary(file) + "\n" }
	}
	engine := generation.Engine{Registry:s.App.Registry, Profile:&s.App.Profile, MaxContextChars:240000}
	msgs := engine.MessagesWithSystem(system, s.App.Profile.Condensed(), history, req.Prompt)
	if err := s.App.Store.AddMessage(provider.Message{Role:"user", Content:req.Prompt}); err != nil { return ChatResponse{}, err }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.started", Phase:"generation", Message:"Generating response", Provider:name, Model:model, Percent:15})
	stream, streamErr := s.App.Registry.Stream(ctx, name, msgs, chatRequestOptions(model, req.BillingMode, requestID, req.Apply))
	var raw strings.Builder
	var usage provider.Usage
	if streamErr == nil {
		for chunk := range stream {
			if chunk.Error != nil {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.failed", Phase:"generation", Message:chunk.Error.Error(), Provider:name, Model:model})
				return ChatResponse{}, chunk.Error
			}
			usage.PromptTokens += chunk.Usage.PromptTokens
			usage.CompletionTokens += chunk.Usage.CompletionTokens
			usage.TotalTokens += chunk.Usage.TotalTokens
			usage.CostUSD += chunk.Usage.CostUSD
			usage.Estimated = usage.Estimated || chunk.Usage.Estimated
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || chunk.Usage.TotalTokens > 0 || chunk.Usage.CostUSD > 0 {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.usage", Phase:"generation", Message:"Usage updated", Provider:name, Model:model, PromptTokens:usage.PromptTokens, CompletionTokens:usage.CompletionTokens, TotalTokens:usage.TotalTokens, UsageEstimated:usage.Estimated, CostUSD:usage.CostUSD, Percent:58})
			}
			if chunk.Delta != "" {
				raw.WriteString(chunk.Delta)
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.chunk", Phase:"generation", Message:chunk.Delta, Provider:name, Model:model, Percent:45})
			}
		}
	} else {
		s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.streaming_unavailable", Phase:"generation", Message:"Provider streaming unavailable; using standard response"})
		resp, err := s.App.Registry.Send(ctx, name, msgs, chatRequestOptions(model, req.BillingMode, requestID, req.Apply))
		if err != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.failed", Phase:"generation", Message:err.Error(), Provider:name, Model:model}); return ChatResponse{}, err }
		raw.WriteString(resp.Content)
		usage = resp.Usage
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 || usage.TotalTokens > 0 || usage.CostUSD > 0 {
			s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.usage", Phase:"generation", Message:"Usage received", Provider:name, Model:model, PromptTokens:usage.PromptTokens, CompletionTokens:usage.CompletionTokens, TotalTokens:usage.TotalTokens, UsageEstimated:usage.Estimated, CostUSD:usage.CostUSD, Percent:58})
		}
	}
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.completed", Phase:"validation", Message:"Response generated; validating structured output", Provider:name, Model:model, Percent:62})
	parsed, parseErr := engine.ParseChatResponse(ctx, raw.String())
	if parseErr != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"validation.failed", Phase:"validation", Message:parseErr.Error(), Provider:name, Model:model}); return ChatResponse{}, chatDiagnosticError(fmt.Errorf("complete assistant response could not be validated: %w", parseErr), raw.String(), s.App.Config) }
	content := raw.String()
	if parsed.Response != "" { content = parsed.Response } else if parsed.Message != "" { content = parsed.Message } else if parsed.Explanation != "" { content = parsed.Explanation }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"validation.completed", Phase:"validation", Message:"Structured response validated", Provider:name, Model:model, Percent:72})
	result := ChatResponse{Content:content, Provider:name, Model:model, PromptTokens:usage.PromptTokens, CompletionTokens:usage.CompletionTokens, TotalTokens:usage.TotalTokens, UsageEstimated:usage.Estimated}
	if parsed.Plan != nil && req.Apply {
		repairBudget := s.App.Config.Verification.SelfCorrectionAttempts
		s.emitEvent(StudioEvent{RequestID:requestID, Type:"verification.budget", Phase:"verification", Message:fmt.Sprintf("Verification budget: %d repair attempt(s)", repairBudget), Percent:76})
		var written []string
		var verification agent.VerificationResult
		for attempt := 0; attempt <= repairBudget; attempt++ {
			s.emitEvent(StudioEvent{RequestID:requestID, Type:"verification.started", Phase:"verification", Message:fmt.Sprintf("Verification pass %d/%d", attempt+1, repairBudget+1), Percent:78})
			executor := agent.Executor{Root:s.App.Store.Root}
			var executeErr error
			verification, executeErr = executor.Execute(*parsed.Plan, func() agent.VerificationResult {
				paths := make([]string, 0, len(parsed.Plan.Files))
				for _, file := range parsed.Plan.Files { paths = append(paths, file.Path) }
				vr, verifyErr := verify.Detect(s.App.Store.Root, paths)
				if verifyErr != nil { return agent.VerificationResult{Passed:false, Output:verifyErr.Error()} }
				return agent.VerificationResult{Passed:vr.Passed, Output:vr.Output}
			})
			if executeErr != nil && strings.Contains(executeErr.Error(), "rollback failed:") {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"apply.failed", Phase:"rollback", Message:executeErr.Error()})
				return ChatResponse{}, fmt.Errorf("workspace rollback failed; stop further edits and inspect the workspace: %w", executeErr)
			}
			if executeErr != nil && !strings.HasPrefix(executeErr.Error(), "verification failed:") {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"apply.failed", Phase:"files", Message:executeErr.Error()})
				return ChatResponse{}, fmt.Errorf("workspace transaction failed: %w", executeErr)
			}
			if executeErr != nil && strings.TrimSpace(verification.Output) == "" {
				verification.Output = executeErr.Error()
			}
			if verification.Passed {
				written = verification.Changed
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"verification.completed", Phase:"verification", Message:"Verification passed; workspace changes committed", Percent:86})
				break
			}
			diagnostic := strings.TrimSpace(verification.Output)
			if diagnostic == "" { diagnostic = "Verification failed without diagnostic output." }
			if len(diagnostic) > 1600 { diagnostic = diagnostic[:1600] + "\\n[diagnostic truncated]" }
			s.emitEvent(StudioEvent{RequestID:requestID, Type:"verification.failed", Phase:"verification", Message:diagnostic, Percent:80})
			if attempt == repairBudget {
				err := fmt.Errorf("verification failed after %d repair attempt(s). Last diagnostics:\\n%s", attempt, diagnostic)
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"apply.failed", Phase:"verification", Message:err.Error()})
				return ChatResponse{}, err
			}
			s.emitEvent(StudioEvent{RequestID:requestID, Type:"repair.started", Phase:"repair", Message:fmt.Sprintf("Repair %d/%d using verification diagnostics", attempt+1, repairBudget), Percent:82})
			correction := generation.BuildCorrectionPrompt(*parsed.Plan, verification.Output)
			repairMessages := append([]provider.Message(nil), msgs...)
			repairMessages = append(repairMessages,
				provider.Message{Role:"assistant", Content:raw.String()},
				provider.Message{Role:"user", Content:correction},
			)
			repairOptions := chatRequestOptions(model, req.BillingMode, requestID, true)
			repairOptions.Temperature = 0
			repaired, repairErr := s.App.Registry.Send(ctx, name, repairMessages, repairOptions)
			if repairErr != nil {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"repair.failed", Phase:"repair", Message:repairErr.Error()})
				return ChatResponse{}, fmt.Errorf("repair attempt %d/%d failed: %w; verification diagnostics: %s", attempt+1, repairBudget, repairErr, diagnostic)
			}
			repairedResponse, repairedParseErr := engine.ParseChatResponse(ctx, repaired.Content)
			if repairedParseErr != nil || repairedResponse.Plan == nil {
				message := "repair response did not contain a valid edit plan"
				if repairedParseErr != nil { message = repairedParseErr.Error() }
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"repair.failed", Phase:"repair", Message:message})
				return ChatResponse{}, fmt.Errorf("repair attempt %d/%d returned an invalid plan: %s", attempt+1, repairBudget, message)
			}
			raw.Reset()
			raw.WriteString(repaired.Content)
			parsed = repairedResponse
			usage.PromptTokens += repaired.Usage.PromptTokens
			usage.CompletionTokens += repaired.Usage.CompletionTokens
			usage.TotalTokens += repaired.Usage.TotalTokens
			usage.CostUSD += repaired.Usage.CostUSD
			usage.Estimated = usage.Estimated || repaired.Usage.Estimated
			result.Provider = repaired.ProviderName
			result.Model = repaired.Model
		}
		if err := s.App.Store.MarkTouched(written); err != nil { return ChatResponse{}, err }
		if err := s.App.Store.RefreshHashes(written); err != nil { return ChatResponse{}, err }
		result.WrittenFiles = written
		result.Applied = true
		for _, path := range written { s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.file_applied", Phase:"files", Message:"Verified file applied", Path:path, Percent:94}) }
	}
	if parsed.Response != "" {
		content = parsed.Response
	} else if parsed.Message != "" {
		content = parsed.Message
	} else if parsed.Explanation != "" {
		content = parsed.Explanation
	} else {
		content = raw.String()
	}
	result.Content = content
	result.PromptTokens = usage.PromptTokens
	result.CompletionTokens = usage.CompletionTokens
	result.TotalTokens = usage.TotalTokens
	result.UsageEstimated = usage.Estimated
	if err := s.App.Store.AddMessage(provider.Message{Role:"assistant", Content:content}); err != nil { return ChatResponse{}, err }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"session.completed", Phase:"complete", Message:"Request completed", Provider:result.Provider, Model:result.Model, PromptTokens:result.PromptTokens, CompletionTokens:result.CompletionTokens, TotalTokens:result.TotalTokens, UsageEstimated:result.UsageEstimated, CostUSD:usage.CostUSD, Percent:100})
	return result, nil
}
func chatDiagnosticError(err error, raw string, cfg config.Config) error {
	if err == nil { return err }
	if current, loadErr := config.Load(); loadErr == nil { cfg = current }
	if !cfg.DebugMode { return err }
	for _, p := range cfg.Providers {
		if key := strings.TrimSpace(p.APIKey); key != "" { raw = strings.ReplaceAll(raw, key, "[REDACTED_API_KEY]") }
	}
	if strings.TrimSpace(raw) == "" { return fmt.Errorf("%w\nDEBUG MODE: no raw AI response was received", err) }
	return fmt.Errorf("%w\n\nDEBUG MODE - RAW AI RESPONSE:\n%s", err, raw)
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
	if len(data) > MaxAttachmentBytes { return errors.New("uploaded file is larger than 2 MiB") }
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
		return "", fmt.Errorf("file %s is larger than 2 MiB", rel)
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
	var total int64
	seen := make(map[string]struct{}, len(paths))
	for _, rel := range paths {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" { continue }
		if _, ok := seen[rel]; ok { continue }
		seen[rel] = struct{}{}
		content, err := s.ReadFile(rel)
		if err != nil { return nil, err }
		path, err := generation.Resolve(s.App.Store.Root, rel)
		if err != nil { return nil, err }
		info, err := os.Stat(path)
		if err != nil { return nil, err }
		total += info.Size()
		if total > MaxAttachmentTotalBytes {
			return nil, fmt.Errorf("selected attachments exceed the 8 MiB total limit")
		}
		files = append(files, Attachment{Path: rel, Content: content, MIMEType: mime.TypeByExtension(filepath.Ext(rel)), SizeBytes: info.Size()})
	}
	return files, nil
}

func attachmentSummary(file Attachment) string {
	kind := file.MIMEType
	if kind == "" { kind = "text/plain" }
	return fmt.Sprintf("ATTACHMENT: %s\nMIME: %s\nSIZE: %d bytes\nBEGIN_ATTACHMENT\n%s\nEND_ATTACHMENT", file.Path, kind, file.SizeBytes, file.Content)
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
	capabilities, capErr := s.App.Registry.Capabilities(name)
	if capErr != nil { return nil, capErr }
	return map[string]any{"provider": name, "models": clean, "default_model": s.App.Registry.DefaultModel(name), "capabilities": capabilities}, nil
}

func (s *Service) Telemetry() (provider.TelemetrySnapshot, error) {
	if s == nil || s.App == nil || s.App.Registry == nil {
		return provider.TelemetrySnapshot{}, errors.New("application not initialized")
	}
	snapshot := s.App.Registry.Telemetry()
	if s.App.Store != nil {
		events, err := s.App.Store.TelemetryEvents(500)
		if err != nil { return provider.TelemetrySnapshot{}, err }
		if len(events) > 0 { snapshot.Events = events }
	}
	return snapshot, nil
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
	return map[string]any{"default_provider": c.DefaultProvider, "providers": providers, "workspace": s.Root(), "debug_mode": c.DebugMode}, nil
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


func (s *Service) PlatformState() (map[string]any, error) {
	if s == nil || s.App == nil || s.App.Platform == nil || s.App.Store == nil {
		return nil, errors.New("platform not initialized")
	}
	memory, err := platform.LoadMemory(s.App.Store.Root)
	if err != nil { return nil, err }
	rules, err := platform.LoadRules(s.App.Store.Root)
	if err != nil { return nil, err }
	skills, err := platform.LoadSkills(s.App.Store.Root)
	if err != nil { return nil, err }
	graph, err := platform.BuildGraph(s.App.Store.Root)
	if err != nil { return nil, err }
	return map[string]any{"memory": memory, "rules": rules, "skills": skills, "graph": graph, "workspaces": s.App.Platform.Workspaces.List(), "jobs": s.App.Platform.Jobs.List()}, nil
}

func (s *Service) SaveMemory(memory platform.ProjectMemory) error {
	if s == nil || s.App == nil || s.App.Store == nil { return errors.New("workspace not initialized") }
	return platform.SaveMemory(s.App.Store.Root, memory)
}

func (s *Service) StartBackgroundJob(ctx context.Context, req ChatRequest) (platform.BackgroundJob, error) {
	if s == nil || s.App == nil || s.App.Platform == nil {
		return platform.BackgroundJob{}, errors.New("platform not initialized")
	}
	if ctx == nil {
		return platform.BackgroundJob{}, errors.New("job context is required")
	}
	jobContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	job, err := s.App.Platform.Jobs.Start(jobContext, req.Prompt, func(ctx context.Context, progress func(string)) (string, error) {
		defer cancel()
		progress("agent started")
		req.Apply = false
		result, err := s.Chat(ctx, req)
		if err != nil {
			return "", err
		}
		progress("response generated")
		return result.Content, nil
	})
	if err != nil {
		cancel()
		return platform.BackgroundJob{}, err
	}
	return job, nil
}

func (s *Service) Job(id string) (platform.BackgroundJob, bool) {
	if s == nil || s.App == nil || s.App.Platform == nil { return platform.BackgroundJob{}, false }
	return s.App.Platform.Jobs.Get(id)
}

func (s *Service) Autocomplete(ctx context.Context, providerName, model, language, prefix, suffix string) (string, error) {
	if s == nil || s.App == nil { return "", errors.New("application not initialized") }
	return platform.Complete(ctx, s.App.Registry, providerName, model, language, prefix, suffix)
}

func (s *Service) InspectURL(ctx context.Context, rawURL string) (platform.BrowserSession, error) {
	return platform.InspectURL(ctx, rawURL)
}

func (s *Service) ReviewDiff(diff string) platform.Review { return platform.ReviewDiff(diff) }

func (s *Service) ContextGraph() (platform.ContextGraph, error) {
	if s == nil || s.App == nil || s.App.Store == nil { return platform.ContextGraph{}, errors.New("workspace not initialized") }
	return platform.BuildGraph(s.App.Store.Root)
}

func (s *Service) SaveProof(proof platform.Proof) (platform.Proof, error) {
	if s == nil || s.App == nil || s.App.Store == nil { return platform.Proof{}, errors.New("workspace not initialized") }
	return platform.SaveProof(s.App.Store.Root, proof)
}
