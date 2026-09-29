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
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
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
	sub := &studioSubscriber{ch: make(chan StudioEvent, 64)}
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
	system := generation.SessionSystemPrompt() + "\n\nYou are FuzeCLI, a practical coding assistant. Use the session response contract above for every response and never ask the user to provide source files that FuzeCLI already supplied."
	if workspaceContext != "" { system += "\nRelevant workspace files read from disk:\n" + workspaceContext }
	if len(attachments) > 0 {
		system += "\nThe user attached the following workspace files for this request. These attachments are authoritative input. The provider receives their extracted UTF-8 text directly in the request. MIME type and size are included so you know exactly what is available. Binary or unsupported formats are rejected instead of silently omitted:\n"
		for _, file := range attachments { system += "\n" + attachmentSummary(file) + "\n" }
	}
	engine := generation.Engine{Registry:s.App.Registry, Profile:&s.App.Profile, MaxContextChars:120000}
	msgs := engine.Messages(s.App.Profile.Condensed(), workspaceContext, history, req.Prompt)
	msgs[0].Content = system
	if err := s.App.Store.AddMessage(provider.Message{Role:"user", Content:req.Prompt}); err != nil { return ChatResponse{}, err }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.started", Phase:"generation", Message:"Generating response", Provider:name, Model:model, Percent:15})
	stream, streamErr := s.App.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model:model, Temperature:0.3, MaxTokens:32768, JSONMode:true, JSONSchema:generation.ChatResponseSchema(), BillingMode:req.BillingMode, RequestID:requestID})
	var raw strings.Builder
	if streamErr == nil {
		for chunk := range stream {
			if chunk.Error != nil {
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.failed", Phase:"generation", Message:chunk.Error.Error(), Provider:name, Model:model})
				return ChatResponse{}, chunk.Error
			}
			if chunk.Delta != "" {
				raw.WriteString(chunk.Delta)
				s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.chunk", Phase:"generation", Message:chunk.Delta, Provider:name, Model:model, Percent:45})
			}
		}
	} else {
		s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.streaming_unavailable", Phase:"generation", Message:"Provider streaming unavailable; using standard response"})
		resp, err := s.App.Registry.Send(ctx, name, msgs, provider.RequestOptions{Model:model, Temperature:0.3, MaxTokens:32768, JSONMode:true, JSONSchema:generation.ChatResponseSchema(), BillingMode:req.BillingMode, RequestID:requestID})
		if err != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.failed", Phase:"generation", Message:err.Error(), Provider:name, Model:model}); return ChatResponse{}, err }
		raw.WriteString(resp.Content)
	}
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.completed", Phase:"validation", Message:"Response generated; validating structured output", Provider:name, Model:model, Percent:62})
	parsed, parseErr := engine.ParseChatResponse(ctx, raw.String())
	if parseErr != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"validation.failed", Phase:"validation", Message:parseErr.Error(), Provider:name, Model:model}); return ChatResponse{}, fmt.Errorf("complete assistant response could not be validated: %w", parseErr) }
	content := raw.String()
	if parsed.Response != "" { content = parsed.Response } else if parsed.Message != "" { content = parsed.Message } else if parsed.Explanation != "" { content = parsed.Explanation }
	if err := s.App.Store.AddMessage(provider.Message{Role:"assistant", Content:content}); err != nil { return ChatResponse{}, err }
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"validation.completed", Phase:"validation", Message:"Structured response validated", Provider:name, Model:model, Percent:72})
	result := ChatResponse{Content:content, Provider:name, Model:model}
	if parsed.Plan != nil && req.Apply {
		s.emitEvent(StudioEvent{RequestID:requestID, Type:"apply.started", Phase:"files", Message:"Applying workspace changes", Percent:78})
		written, applyErr := generation.ApplyChatPlan(s.App.Store.Root, *parsed.Plan)
		if applyErr != nil { s.emitEvent(StudioEvent{RequestID:requestID, Type:"apply.failed", Phase:"files", Message:applyErr.Error()}); return ChatResponse{}, applyErr }
		if err := s.App.Store.MarkTouched(written); err != nil { return ChatResponse{}, err }
		_ = s.App.Store.RefreshHashes(written)
		result.WrittenFiles = written
		result.Applied = true
		for _, path := range written { s.emitEvent(StudioEvent{RequestID:requestID, Type:"generation.file_applied", Phase:"files", Message:"File applied", Path:path, Percent:84}) }
	}
	s.emitEvent(StudioEvent{RequestID:requestID, Type:"session.completed", Phase:"complete", Message:"Request completed", Provider:result.Provider, Model:result.Model, Percent:100})
	return result, nil
}

