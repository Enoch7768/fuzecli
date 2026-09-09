package gemini

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type Provider struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func New(
	apiKey string,
	baseURL string,
) *Provider {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	return &Provider{
		APIKey:     apiKey,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: http.DefaultClient,
	}
}

func (p *Provider) Name() string {
	return "gemini"
}

type part struct {
	Text string `json:"text,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type requestBody struct {
	Contents          []content      `json:"contents"`
	SystemInstruction *content       `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]any `json:"generationConfig,omitempty"`
}

type responseBody struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

type modelInfo struct {
	Name             string   `json:"name"`
	DisplayName      string   `json:"displayName"`
	SupportedMethods []string `json:"supportedGenerationMethods"`
}

type modelsResponse struct {
	Models        []modelInfo `json:"models"`
	NextPageToken string      `json:"nextPageToken"`
}

func convert(
	messages []provider.Message,
) ([]content, *content) {
	var system *content
	var contents []content

	for _, message := range messages {
		switch message.Role {
		case "system":
			value := content{
				Role: "user",
				Parts: []part{
					{
						Text: message.Content,
					},
				},
			}

			system = &value

		case "assistant":
			contents = append(
				contents,
				content{
					Role: "model",
					Parts: []part{
						{
							Text: message.Content,
						},
					},
				},
			)

		default:
			contents = append(
				contents,
				content{
					Role: "user",
					Parts: []part{
						{
							Text: message.Content,
						},
					},
				},
			)
		}
	}

	return contents, system
}

func (p *Provider) Send(
	ctx context.Context,
	messages []provider.Message,
	opts provider.RequestOptions,
) (*provider.Response, error) {
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf(
			"gemini: model is required",
		)
	}

	contents, system := convert(messages)

	payload := requestBody{
		Contents:          contents,
		SystemInstruction: system,
		GenerationConfig: map[string]any{
			"temperature":     opts.Temperature,
			"maxOutputTokens": opts.MaxTokens,
		},
	}

	var result responseBody

	err := provider.DoJSON(
		ctx,
		p.HTTPClient,
		http.MethodPost,
		p.BaseURL+"/models/"+url.PathEscape(
			opts.Model,
		)+":generateContent",
		map[string]string{
			"x-goog-api-key": p.APIKey,
		},
		payload,
		&result,
		p.Name(),
	)

	if err != nil {
		return nil, err
	}

	if len(result.Candidates) == 0 {
		return nil, fmt.Errorf(
			"gemini: response contained no candidates",
		)
	}

	if len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf(
			"gemini: response contained no text parts",
		)
	}

	return &provider.Response{
		Content:      result.Candidates[0].Content.Parts[0].Text,
		Model:        opts.Model,
		ProviderName: p.Name(),
	}, nil
}

func (p *Provider) Stream(
	ctx context.Context,
	messages []provider.Message,
	opts provider.RequestOptions,
) (<-chan provider.StreamChunk, error) {
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf(
			"gemini: model is required",
		)
	}

	contents, system := convert(messages)

	payload := requestBody{
		Contents:          contents,
		SystemInstruction: system,
		GenerationConfig: map[string]any{
			"temperature":     opts.Temperature,
			"maxOutputTokens": opts.MaxTokens,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf(
			"encode gemini stream request: %w",
			err,
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.BaseURL+"/models/"+url.PathEscape(
			opts.Model,
		)+":streamGenerateContent?alt=sse",
		strings.NewReader(string(data)),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create gemini stream request: %w",
			err,
		)
	}

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set(
		"x-goog-api-key",
		p.APIKey,
	)

	response, err := p.HTTPClient.Do(request)
	if err != nil {
		return nil, &provider.ProviderError{
			Kind:     provider.ErrorProviderUnavailable,
			Provider: p.Name(),
			Message:  "request failed",
			Err:      err,
		}
	}

	if response.StatusCode < 200 ||
		response.StatusCode >= 300 {
		err := provider.ParseHTTPResponseError(
			response,
			p.Name(),
		)

		_ = response.Body.Close()

		return nil, err
	}

	stream := make(chan provider.StreamChunk)

	go func() {
		defer close(stream)
		defer response.Body.Close()

		scanner := bufio.NewScanner(
			response.Body,
		)

		scanner.Buffer(
			make([]byte, 0, 64<<10),
			4<<20,
		)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				stream <- provider.StreamChunk{
					Error: ctx.Err(),
				}
				return

			default:
			}

			line := strings.TrimSpace(
				scanner.Text(),
			)

			if !strings.HasPrefix(
				line,
				"data:",
			) {
				continue
			}

			data := strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"data:",
				),
			)

			if data == "" ||
				data == "[DONE]" {
				continue
			}

			var result responseBody

			if err := json.Unmarshal(
				[]byte(data),
				&result,
			); err != nil {
				continue
			}

			if len(result.Candidates) == 0 {
				continue
			}

			if len(result.Candidates[0].Content.Parts) == 0 {
				continue
			}

			delta := result.Candidates[0].
				Content.
				Parts[0].
				Text

			if delta == "" {
				continue
			}

			stream <- provider.StreamChunk{
				Delta: delta,
			}
		}

		if err := scanner.Err(); err != nil {
			stream <- provider.StreamChunk{
				Error: fmt.Errorf(
					"gemini stream read failed: %w",
					err,
				),
			}
			return
		}

		stream <- provider.StreamChunk{
			Done: true,
		}
	}()

	return stream, nil
}

func (p *Provider) ListModels(
	ctx context.Context,
) ([]string, error) {
	var result []string
	nextPageToken := ""

	for {
		endpoint := p.BaseURL + "/models"

		if nextPageToken != "" {
			endpoint += "?pageToken=" +
				url.QueryEscape(nextPageToken)
		}

		var response modelsResponse

		err := provider.DoJSON(
			ctx,
			p.HTTPClient,
			http.MethodGet,
			endpoint,
			map[string]string{
				"x-goog-api-key": p.APIKey,
			},
			nil,
			&response,
			p.Name(),
		)

		if err != nil {
			return nil, err
		}

		for _, model := range response.Models {
			for _, method := range model.SupportedMethods {
				if method == "generateContent" {
					result = append(
						result,
						strings.TrimPrefix(
							model.Name,
							"models/",
						),
					)
					break
				}
			}
		}

		if response.NextPageToken == "" {
			break
		}

		nextPageToken = response.NextPageToken
	}

	return result, nil
}
