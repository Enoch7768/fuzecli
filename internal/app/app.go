package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/profile"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/provider/anthropic"
	"github.com/Enoch7768/fuzecli/internal/provider/gemini"
	"github.com/Enoch7768/fuzecli/internal/provider/groq"
	"github.com/Enoch7768/fuzecli/internal/provider/llamacpp"
	"github.com/Enoch7768/fuzecli/internal/provider/openai"
	"github.com/Enoch7768/fuzecli/internal/ui"
	"github.com/Enoch7768/fuzecli/internal/verify"
	"github.com/Enoch7768/fuzecli/internal/workspace"
)

type App struct {
	Config   config.Config
	Registry *provider.Registry
	Store    *workspace.Store
	Profile  profile.Profile
}

func Load() (*App, error) {
	c, err := config.Load()
	if err != nil {
		return nil, err
	}

	procs := []provider.Provider{
		openai.New(
			c.Providers["openai"].APIKey,
			"",
		),
		gemini.New(
			c.Providers["gemini"].APIKey,
			"",
		),
		groq.New(
			c.Providers["groq"].APIKey,
			"",
		),
		anthropic.New(
			c.Providers["anthropic"].APIKey,
			"",
		),
		llamacpp.New(
			c.Providers["llamacpp"].BaseURL,
		),
	}

	defaults := map[string]string{}

	for name, cfg := range c.Providers {
		defaults[name] = cfg.DefaultModel
	}

	r := provider.NewRegistry(
		c.FallbackOrder,
		defaults,
		procs...,
	)

	p, err := profile.Load()
	if err != nil {
		return nil, err
	}

	return &App{
		Config:   c,
		Registry: r,
		Profile:  p,
	}, nil
}

func (a *App) AttachWorkspace(root string) error {
	s, err := workspace.Open(root)
	if err != nil {
		return err
	}

	a.Store = s
	return nil
}

func (a *App) Close() {
	if a.Store != nil {
		_ = a.Store.Close()
	}
}

func (a *App) ProviderAndModel(
	name,
	model string,
) (string, string, error) {
	if name == "" {
		name = a.Config.DefaultProvider
	}

	if name != "auto" {
		pc, ok := a.Config.Providers[name]

		if !ok {
			return "", "", fmt.Errorf(
				"unknown provider %q",
				name,
			)
		}

		if model == "" {
			model = pc.DefaultModel
		}

		if model == "" {
			return "", "", fmt.Errorf(
				"no model configured for %s",
				name,
			)
		}

		return name, model, nil
	}

	if len(a.Config.FallbackOrder) == 0 {
		return "", "", fmt.Errorf(
			"auto provider mode has an empty fallback order",
		)
	}

	return "auto", model, nil
}

func (a *App) Ask(
	ctx context.Context,
	prompt,
	providerName,
	model string,
	yes bool,
) (*generation.Plan, error) {
	if generation.ShouldUsePlanner(prompt) {
		return a.AskPlanned(
			ctx,
			prompt,
			providerName,
			model,
			yes,
		)
	}

	return a.askOnce(
		ctx,
		prompt,
		providerName,
		model,
		yes,
	)
}

func (a *App) askOnce(
	ctx context.Context,
	prompt,
	providerName,
	model string,
	yes bool,
) (*generation.Plan, error) {
	if a.Store == nil {
		return nil, errors.New(
			"workspace not initialized; run aicli init",
		)
	}

	name, mdl, err := a.ProviderAndModel(
		providerName,
		model,
	)
	if err != nil {
		return nil, err
	}

	history, err := a.Store.History(400)
	if err != nil {
		return nil, err
	}

	history, err = a.compactHistory(
		ctx,
		name,
		mdl,
		history,
	)
	if err != nil {
		return nil, err
	}

	ctxText, err := a.Store.WorkspaceContext()
	if err != nil {
		return nil, err
	}

	engine := generation.Engine{
		Registry:        a.Registry,
		Profile:         &a.Profile,
		MaxContextChars: 120000,
	}

	msgs := engine.Messages(
		a.Profile.Condensed(),
		ctxText,
		history,
		prompt,
	)

	_ = a.Store.AddMessage(
		provider.Message{
			Role:    "user",
			Content: prompt,
		},
	)

	resp, err := a.Registry.Send(
		ctx,
		name,
		msgs,
		provider.RequestOptions{
			Model:       mdl,
			Temperature: 0.2,
			MaxTokens:   16000,
			JSONMode:    true,
		},
	)
	if err != nil {
		return nil, err
	}

	_ = a.Store.AddMessage(
		provider.Message{
			Role:    "assistant",
			Content: resp.Content,
		},
	)

	plan, err := generation.ParsePlan(
		resp.Content,
	)

	if err != nil {
		fix := append(
			msgs,
			provider.Message{
				Role: "user",
				Content: "Your previous response was invalid JSON. Return ONLY valid JSON matching the required schema. Error: " +
					err.Error(),
			},
		)

		resp2, e2 := a.Registry.Send(
			ctx,
			name,
			fix,
			provider.RequestOptions{
				Model:       mdl,
				Temperature: 0,
				MaxTokens:   16000,
				JSONMode:    true,
			},
		)

		if e2 != nil {
			return nil, fmt.Errorf(
				"JSON correction request failed: %w",
				e2,
			)
		}

		plan, err = generation.ParsePlan(
			resp2.Content,
		)

		if err != nil {
			return nil, err
		}

		_ = a.Store.AddMessage(
			provider.Message{
				Role:    "assistant",
				Content: resp2.Content,
			},
		)
	}

	ui.Preview(
		plan,
		a.Store.Root,
	)

	if !yes {
		plan, err = ui.Confirm(
			plan,
			a.Store.Root,
		)
		if err != nil {
			return nil, err
		}
	}

	written, err := generation.Apply(
		a.Store.Root,
		plan,
	)
	if err != nil {
		return nil, err
	}

	if err := a.Store.MarkTouched(
		written,
	); err != nil {
		return nil, err
	}

	st, _ := a.Store.LoadState()

	st.ActiveProvider = resp.ProviderName
	st.ActiveModel = resp.Model

	if st.ActiveModel == "" {
		st.ActiveModel = mdl
	}

	_ = a.Store.SaveState(st)
	_ = a.Store.RefreshHashes(written)

	for attempt := 0; attempt <= a.Config.Verification.SelfCorrectionAttempts; attempt++ {
		vr, _ := verify.Detect(
			a.Store.Root,
			written,
		)

		fmt.Println(
			verify.Format(vr),
		)

		if vr.Passed {
			break
		}

		if attempt ==
			a.Config.Verification.SelfCorrectionAttempts {
			return nil, fmt.Errorf(
				"verification failed after %d self-correction attempts",
				attempt,
			)
		}

		correction := generation.BuildCorrectionPrompt(
			plan,
			vr.Output,
		)

		history, _ = a.Store.History(400)

		history, _ = a.compactHistory(
			ctx,
			name,
			mdl,
			history,
		)

		ctxText, _ = a.Store.WorkspaceContext()

		msgs = engine.Messages(
			a.Profile.Condensed(),
			ctxText,
			history,
			correction,
		)

		resp, err = a.Registry.Send(
			ctx,
			name,
			msgs,
			provider.RequestOptions{
				Model:       mdl,
				Temperature: 0,
				MaxTokens:   16000,
				JSONMode:    true,
			},
		)
		if err != nil {
			return nil, err
		}

		plan, err = generation.ParsePlan(
			resp.Content,
		)
		if err != nil {
			return nil, err
		}

		ui.Preview(
			plan,
			a.Store.Root,
		)

		written, err = generation.Apply(
			a.Store.Root,
			plan,
		)
		if err != nil {
			return nil, err
		}

		_ = a.Store.MarkTouched(written)
		_ = a.Store.RefreshHashes(written)
	}

	return &plan, nil
}

func (a *App) AskPlanned(
	ctx context.Context,
	prompt,
	providerName,
	model string,
	yes bool,
) (*generation.Plan, error) {
	if a.Store == nil {
		return nil, errors.New(
			"workspace not initialized; run aicli init",
		)
	}

	name, mdl, err := a.ProviderAndModel(
		providerName,
		model,
	)
	if err != nil {
		return nil, err
	}

	projectPlan, err := generation.LoadProjectPlan(
		a.Store.Root,
		prompt,
	)
	if err != nil {
		return nil, err
	}

	if projectPlan == nil {
		workspaceContext, err := a.Store.WorkspaceContext()
		if err != nil {
			return nil, err
		}

		planMessages := generation.PlannerPrompt(
			prompt,
			workspaceContext,
		)

		resp, err := a.Registry.Send(
			ctx,
			name,
			planMessages,
			provider.RequestOptions{
				Model:       mdl,
				Temperature: 0,
				MaxTokens:   12000,
				JSONMode:    true,
				JSONSchema:  generation.PlannerSchema(),
			},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"project planning failed: %w",
				err,
			)
		}

		parsed, err := generation.ParseProjectPlan(
			resp.Content,
		)

		if err != nil {
			fixMessages := append(
				planMessages,
				provider.Message{
					Role: "user",
					Content: "The planner response was invalid. Return ONLY valid JSON matching the exact project planner schema. Error: " +
						err.Error(),
				},
			)

			fixResp, fixErr := a.Registry.Send(
				ctx,
				name,
				fixMessages,
				provider.RequestOptions{
					Model:       mdl,
					Temperature: 0,
					MaxTokens:   12000,
					JSONMode:    true,
					JSONSchema:  generation.PlannerSchema(),
				},
			)

			if fixErr != nil {
				return nil, fmt.Errorf(
					"project planner correction failed: %w",
					fixErr,
				)
			}

			parsed, err = generation.ParseProjectPlan(
				fixResp.Content,
			)
			if err != nil {
				return nil, err
			}
		}

		normalized := generation.NewProjectPlan(
			prompt,
			parsed,
		)

		projectPlan = &normalized

		if err := generation.SaveProjectPlan(
			a.Store.Root,
			*projectPlan,
		); err != nil {
			return nil, err
		}

		fmt.Printf(
			"Project planned: %s\n",
			projectPlan.Project,
		)

		fmt.Printf(
			"Files planned: %d\n",
			len(projectPlan.Files),
		)
	} else {
		fmt.Printf(
			"Resuming project: %s\n",
			projectPlan.Project,
		)

		fmt.Printf(
			"Progress: %d/%d files complete\n",
			len(projectPlan.Files)-
				len(generation.PendingFiles(*projectPlan)),
			len(projectPlan.Files),
		)
	}

	var lastPlan generation.Plan

	for {
		batch := generation.NextBatch(
			*projectPlan,
		)

		if len(batch) == 0 {
			fmt.Println(
				"Project generation complete.",
			)

			return &lastPlan, nil
		}

		completed := len(projectPlan.Files) -
			len(generation.PendingFiles(*projectPlan))

		fmt.Printf(
			"\nGeneration progress: %d/%d files complete\n",
			completed,
			len(projectPlan.Files),
		)

		fmt.Println("Next batch:")

		for _, file := range batch {
			fmt.Printf(
				"  %s\n",
				file.Path,
			)
		}

		batchPrompt := generation.BuildBatchPrompt(
			prompt,
			*projectPlan,
			batch,
		)

		plan, err := a.askOnce(
			ctx,
			batchPrompt,
			providerName,
			model,
			yes,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"generation batch failed: %w",
				err,
			)
		}

		if err := generation.ValidateBatch(
			*plan,
			batch,
		); err != nil {
			return nil, err
		}

		paths := make(
			[]string,
			0,
			len(plan.Files),
		)

		for _, file := range plan.Files {
			paths = append(
				paths,
				file.Path,
			)
		}

		generation.MarkBatchCompleted(
			projectPlan,
			paths,
		)

		if err := generation.SaveProjectPlan(
			a.Store.Root,
			*projectPlan,
		); err != nil {
			return nil, err
		}

		lastPlan = *plan

		completed = len(projectPlan.Files) -
			len(generation.PendingFiles(*projectPlan))

		fmt.Printf(
			"Batch complete: %d/%d files\n",
			completed,
			len(projectPlan.Files),
		)
	}
}

func (a *App) RunProfileExtraction(
	ctx context.Context,
) {
	if a.Store == nil {
		return
	}

	history, err := a.Store.History(300)
	if err != nil || len(history) == 0 {
		return
	}

	name, model, _ := a.ProviderAndModel(
		"",
		"",
	)

	_ = profile.ExtractAndMerge(
		ctx,
		&a.Profile,
		a.Registry,
		name,
		model,
		history,
	)
}

func (a *App) compactHistory(
	ctx context.Context,
	name,
	model string,
	history []provider.Message,
) ([]provider.Message, error) {
	const maxChars = 120000

	total := 0

	for _, m := range history {
		total += len(m.Content)
	}

	if total <= maxChars {
		return history, nil
	}

	cut := len(history) / 2

	if cut < 1 {
		return history, nil
	}

	var source strings.Builder

	for _, m := range history[:cut] {
		fmt.Fprintf(
			&source,
			"%s: %s\n",
			m.Role,
			m.Content,
		)
	}

	msgs := []provider.Message{
		{
			Role: "system",
			Content: `Summarize this older FuzeCLI conversation for future coding context. Preserve decisions, requirements, file names, architecture, unresolved issues, and developer preferences. Return plain text only.`,
		},
		{
			Role:    "user",
			Content: source.String(),
		},
	}

	resp, err := a.Registry.Send(
		ctx,
		name,
		msgs,
		provider.RequestOptions{
			Model:       model,
			Temperature: 0,
			MaxTokens:   2000,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"conversation summarization failed: %w",
			err,
		)
	}

	result := []provider.Message{
		{
			Role: "system",
			Content: "Conversation summary of older turns:\n" +
				resp.Content,
		},
	}

	result = append(
		result,
		history[cut:]...,
	)

	return result, nil
}

func ReadPrompt(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}

	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(
		string(b),
	), nil
}

func (a *App) Chat(
	ctx context.Context,
	yes bool,
) error {
	if a.Store == nil {
		return errors.New(
			"workspace not initialized; run aicli init",
		)
	}

	fmt.Println(
		"FuzeCLI chat. Type /exit to quit. Use /code <request> for structured file generation.",
	)

	s := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("fuze> ")

		if !s.Scan() {
			break
		}

		line := strings.TrimSpace(
			s.Text(),
		)

		if line == "/exit" ||
			line == "/quit" {
			break
		}

		if line == "" {
			continue
		}

		if strings.HasPrefix(
			line,
			"/code ",
		) {
			if _, err := a.Ask(
				ctx,
				strings.TrimSpace(
					strings.TrimPrefix(
						line,
						"/code",
					),
				),
				"",
				"",
				yes,
			); err != nil {
				fmt.Fprintln(
					os.Stderr,
					"error:",
					err,
				)
			}

			continue
		}

		if err := a.chatTurn(
			ctx,
			line,
		); err != nil {
			fmt.Fprintln(
				os.Stderr,
				"error:",
				err,
			)
		}
	}

	go a.RunProfileExtraction(
		context.Background(),
	)

	return s.Err()
}

func (a *App) chatTurn(
	ctx context.Context,
	prompt string,
) error {
	name, model, err := a.ProviderAndModel(
		"",
		"",
	)
	if err != nil {
		return err
	}

	history, err := a.Store.History(400)
	if err != nil {
		return err
	}

	history, err = a.compactHistory(
		ctx,
		name,
		model,
		history,
	)
	if err != nil {
		return err
	}

	workspaceContext, err := a.Store.WorkspaceContext()
	if err != nil {
		return err
	}

	system := "You are FuzeCLI, a practical coding assistant. Answer clearly and concisely. Do not modify files in chat mode."

	if a.Profile.Condensed() != "" {
		system += "\nDeveloper profile:\n" +
			a.Profile.Condensed()
	}

	if workspaceContext != "" {
		system += "\nRelevant workspace files:\n" +
			workspaceContext
	}

	msgs := []provider.Message{
		{
			Role:    "system",
			Content: system,
		},
	}

	msgs = append(
		msgs,
		history...,
	)

	msgs = append(
		msgs,
		provider.Message{
			Role:    "user",
			Content: prompt,
		},
	)

	msgs = generationTrim(
		msgs,
		120000,
	)

	_ = a.Store.AddMessage(
		provider.Message{
			Role:    "user",
			Content: prompt,
		},
	)

	stream, err := a.Registry.Stream(
		ctx,
		name,
		msgs,
		provider.RequestOptions{
			Model:       model,
			Temperature: 0.3,
			MaxTokens:   4000,
		},
	)
	if err != nil {
		return err
	}

	var b strings.Builder

	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}

		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
			b.WriteString(chunk.Delta)
		}
	}

	fmt.Println()

	return a.Store.AddMessage(
		provider.Message{
			Role:    "assistant",
			Content: b.String(),
		},
	)
}

func generationTrim(
	messages []provider.Message,
	maxChars int,
) []provider.Message {
	total := 0

	for _, m := range messages {
		total += len(m.Content)
	}

	if total <= maxChars {
		return messages
	}

	out := []provider.Message{
		messages[0],
	}

	used := len(messages[0].Content)

	for i := len(messages) - 1; i >= 1; i-- {
		if used+len(messages[i].Content) > maxChars {
			continue
		}

		out = append(
			[]provider.Message{
				messages[i],
			},
			out...,
		)

		used += len(messages[i].Content)
	}

	return out
}

func InitWorkspace(root string) error {
	s, err := workspace.Init(root)
	if err != nil {
		return err
	}

	_ = s.Close()

	gitignore := filepath.Join(
		root,
		".gitignore",
	)

	b, _ := os.ReadFile(gitignore)

	content := string(b)

	if !strings.Contains(
		content,
		".aicli/",
	) {
		if content != "" &&
			!strings.HasSuffix(
				content,
				"\n",
			) {
			content += "\n"
		}

		content += ".aicli/\n"

		_ = os.WriteFile(
			gitignore,
			[]byte(content),
			0644,
		)
	}

	fmt.Printf(
		"Initialized FuzeCLI workspace in %s\n",
		root,
	)

	return nil
}

func ShowHistory(root string) error {
	s, err := workspace.Open(root)
	if err != nil {
		return err
	}

	defer s.Close()

	hist, err := s.History(200)
	if err != nil {
		return err
	}

	for _, m := range hist {
		fmt.Printf(
			"[%s] %s\n%s\n",
			m.Role,
			m.Content,
			"---",
		)
	}

	return nil
}