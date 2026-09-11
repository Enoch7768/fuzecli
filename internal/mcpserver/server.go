package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/api"
	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ChatInput struct {
	Prompt   string   `json:"prompt" jsonschema:"prompt to send to FuzeCLI"`
	Files    []string `json:"files,omitempty" jsonschema:"workspace-relative files to attach"`
	Provider string   `json:"provider,omitempty" jsonschema:"provider name"`
	Model    string   `json:"model,omitempty" jsonschema:"model name"`
}

type ChatOutput struct {
	Content      string   `json:"content"`
	WrittenFiles []string `json:"written_files,omitempty"`
	Applied      bool     `json:"applied"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
}

type FileInput struct {
	Path string `json:"path" jsonschema:"workspace-relative file path"`
}

type FileOutput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ListInput struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"optional workspace-relative prefix"`
}

type ListOutput struct {
	Files []string `json:"files"`
}

func Run(ctx context.Context, a *app.App) error {
	service := api.NewService(a)
	server := mcp.NewServer(&mcp.Implementation{Name: "fuzecli", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "fuze_chat", Description: "Send a prompt to FuzeCLI and automatically apply valid file-generation JSON."}, func(ctx context.Context, req *mcp.CallToolRequest, input ChatInput) (*mcp.CallToolResult, ChatOutput, error) {
		result, err := service.Chat(ctx, api.ChatRequest{Prompt: input.Prompt, Files: input.Files, Provider: input.Provider, Model: input.Model, Apply: true})
		if err != nil {
			return nil, ChatOutput{}, err
		}
		return nil, ChatOutput{Content: result.Content, WrittenFiles: result.WrittenFiles, Applied: result.Applied, Provider: result.Provider, Model: result.Model}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fuze_read_file", Description: "Read a UTF-8 text file from the active FuzeCLI workspace."}, func(ctx context.Context, req *mcp.CallToolRequest, input FileInput) (*mcp.CallToolResult, FileOutput, error) {
		_ = ctx
		content, err := service.ReadFile(input.Path)
		if err != nil {
			return nil, FileOutput{}, err
		}
		return nil, FileOutput{Path: strings.ReplaceAll(input.Path, "\\", "/"), Content: content}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fuze_list_files", Description: "List workspace files, excluding .git and .aicli."}, func(ctx context.Context, req *mcp.CallToolRequest, input ListInput) (*mcp.CallToolResult, ListOutput, error) {
		_ = ctx
		files, err := service.ListFiles(input.Prefix)
		if err != nil {
			return nil, ListOutput{}, err
		}
		return nil, ListOutput{Files: files}, nil
	})
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server failed: %w", err)
	}
	return nil
}

func EncodeOutput(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
