package generation

import (
	"os"
	"strings"
	"path/filepath"
	"testing"
)

func TestParseChatResponseNormalChat(t *testing.T) {
	got, err := ParseChatResponse(`{"type":"chat","response":"Hello developer.","files":[],"explanation":"","commands":[]}`)
	if err != nil {
		t.Fatalf("ParseChatResponse returned error: %v", err)
	}
	if got.Type != "chat" || got.Response != "Hello developer." || got.Plan != nil {
		t.Fatalf("unexpected normal chat response: %#v", got)
	}
}

func TestParseChatResponseEdit(t *testing.T) {
	got, err := ParseChatResponse(`{"type":"edit","response":"","files":[{"path":"main.go","content":"package main\n","action":"modify"}],"explanation":"Updated main.","commands":[]}`)
	if err != nil {
		t.Fatalf("ParseChatResponse returned error: %v", err)
	}
	if got.Type != "edit" || got.Plan == nil || len(got.Plan.Files) != 1 {
		t.Fatalf("unexpected edit response: %#v", got)
	}
	if got.Plan.Files[0].Path != "main.go" {
		t.Fatalf("unexpected file path: %q", got.Plan.Files[0].Path)
	}
}

func TestParseChatResponseFencedJSON(t *testing.T) {
	got, err := ParseChatResponse("```json\n{\"type\":\"chat\",\"response\":\"Ready.\"}\n```")
	if err != nil {
		t.Fatalf("ParseChatResponse returned error: %v", err)
	}
	if got.Type != "chat" || got.Response != "Ready." {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestParseChatResponseIgnoresUnknownFields(t *testing.T) {
	got, err := ParseChatResponse(`{"type":"chat","response":"Hello","unexpected":true}`)
	if err != nil {
		t.Fatalf("unexpected field should not break compatible JSON: %v", err)
	}
	if got.Response != "Hello" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestParseChatPlanAcceptsLineRangesWithoutAction(t *testing.T) {
	raw := `{"files":[{"path":"script.js","line_start":1,"line_end":1000,"content":"const cart = [];"}],"explanation":"updated cart logic","commands":[]}`
	plan, err := ParseChatPlan(raw)
	if err != nil {
		t.Fatalf("ParseChatPlan returned error: %v", err)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("expected one file, got %d", len(plan.Files))
	}
	if plan.Files[0].Path != "script.js" || plan.Files[0].Content != "const cart = [];" {
		t.Fatalf("unexpected file: %#v", plan.Files[0])
	}
}

func TestParseChatPlanAcceptsFencedJSON(t *testing.T) {
	raw := "```json\n{\"files\":[{\"path\":\"index.html\",\"content\":\"<!doctype html>\"}],\"explanation\":\"created page\",\"commands\":[]}\n```"
	plan, err := ParseChatPlan(raw)
	if err != nil {
		t.Fatalf("fenced JSON rejected: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != "index.html" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestLooksLikeChatPlanPrefix(t *testing.T) {
	cases := []string{
		`{"files":[`,
		`{"response":"`,
		"```json\n{",
	}
	for _, value := range cases {
		if !LooksLikeChatPlanPrefix(value) {
			t.Fatalf("expected generation prefix: %q", value)
		}
	}
	if LooksLikeChatPlanPrefix("This is ordinary text") {
		t.Fatal("ordinary text classified as generation JSON")
	}
}

func TestParseChatPlanRejectsUnsafePaths(t *testing.T) {
	for _, raw := range []string{
		`{"files":[{"path":"../outside.txt","content":"x"}],"explanation":"x"}`,
		`{"files":[{"path":"C:\\outside.txt","content":"x"}],"explanation":"x"}`,
		`{"files":[{"path":"/outside.txt","content":"x"}],"explanation":"x"}`,
	} {
		if _, err := ParseChatPlan(raw); err == nil {
			t.Fatalf("unsafe path accepted: %s", raw)
		}
	}
}

func TestApplyChatPlanInfersCreateAndModify(t *testing.T) {
	root := t.TempDir()
	createPlan := Plan{Files: []FileChange{{Path: "script.js", Content: "const cart = [];"}}}
	written, err := ApplyChatPlan(root, createPlan)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if len(written) != 1 || written[0] != "script.js" {
		t.Fatalf("unexpected written files: %#v", written)
	}
	path := filepath.Join(root, "script.js")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(b) != "const cart = [];" {
		t.Fatalf("created content = %q", b)
	}
	modifyPlan := Plan{Files: []FileChange{{Path: "script.js", Content: "const cart = [1, 2];"}}}
	if _, err := ApplyChatPlan(root, modifyPlan); err != nil {
		t.Fatalf("modify failed: %v", err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	if string(b) != "const cart = [1, 2];" {
		t.Fatalf("modified content = %q", b)
	}
}

func TestParseChatResponseHandlesPreambleAndTrailingProse(t *testing.T) {
	raw := "Result:\n```json\n{\"type\":\"chat\",\"response\":\"done\"}\n```\nFinished."
	got, err := ParseChatResponse(raw)
	if err != nil {
		t.Fatalf("preamble/trailing prose rejected: %v", err)
	}
	if got.Response != "done" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestParseChatResponsePreservesNestedEscapedJSON(t *testing.T) {
	raw := `{"type":"edit","response":"","files":[{"path":"data.json","content":"{\"nested\":[{\"text\":\"} and ] inside a string\"}]}","action":"create","line_start":0,"line_end":0}],"explanation":"nested","commands":[]}`
	got, err := ParseChatResponse(raw)
	if err != nil {
		t.Fatalf("escaped nested JSON rejected: %v", err)
	}
	if got.Plan == nil || len(got.Plan.Files) != 1 || !strings.Contains(got.Plan.Files[0].Content, "inside a string") {
		t.Fatalf("nested content was not preserved: %#v", got)
	}
}

func TestParseChatResponseRejectsTruncatedString(t *testing.T) {
	_, err := ParseChatResponse(`{"type":"chat","response":"unterminated`)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "incomplete") {
		t.Fatalf("expected incomplete JSON error, got %v", err)
	}
}

func TestParseChatResponseRejectsMismatchedNesting(t *testing.T) {
	_, err := ParseChatResponse(`{"type":"chat","response":["broken"}`)
	if err == nil {
		t.Fatal("expected mismatched nesting rejection")
	}
}
